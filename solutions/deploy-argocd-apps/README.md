# Deploy applications with Argo CD

The script and instructions provided in this doc help you to setup an Open Cluster Management (OCM) environment with Kind clusters and integrate it with Argo CD. And then you can deploy Argo CD applications to OCM managed clusters.

## Prerequisite

- [kind](https://kind.sigs.k8s.io) must be installed on your local machine. The Kubernetes version must be >= 1.19, see [kind user guide](https://kind.sigs.k8s.io/docs/user/quick-start/#creating-a-cluster) for more details.

- Download and install [clusteradm](https://github.com/open-cluster-management-io/clusteradm/releases). For Linux OS, run the following commands:

    ```
    wget -qO- https://github.com/open-cluster-management-io/clusteradm/releases/latest/download/clusteradm_linux_amd64.tar.gz | sudo tar -xvz -C /usr/local/bin/

    sudo chmod +x /usr/local/bin/clusteradm
    ```

## Setup the clusters

1. Find your private IP address

```shell
ifconfig | grep inet
```
Your IP address is usually next to the last entry of 'inet'. An IP address is always in the format of x.x.x.x but it will never be 127.0.0.1 because that is your machines loopback address. 

2. Edit the kind configuration files, including `cluster1-config.yaml` and `cluster2-config.yaml`, for managed clusters to update the value in `apiServerAddress` to match your private IP address, which makes the kube apiserver of the managed clusters accessable for Argo CD running on the hub cluster. 

3. Run `./setup-ocm.sh`, you will see two clusters registered on the hub cluster.

    ```
    NAME       HUB ACCEPTED   MANAGED CLUSTER URLS                  JOINED   AVAILABLE   AGE
    cluster1   true           https://cluster1-control-plane:6443                        18s
    cluster2   true           https://cluster2-control-plane:6443                        1s
    ```
    <details>
    <summary>Known issue when running the setup script on a Linux environment</summary>

    You may run into this issue when trying to create multiple clusters
    ```
    Creating cluster "cluster2" ...
    ✓ Ensuring node image (kindest/node:v1.26.3) 🖼
    ✗ Preparing nodes 📦  
    ERROR: failed to create cluster: could not find a log line that matches "Reached target .*Multi-User System.*|detected cgroup v1
    ```
    This might be caused by kernel limits such as number of open files, inotifiy watches, etc.
    To solve this, try increasing your `max_user_instances` and `max_user_watches`:

    * To see the current limits
      ```
      $ cat /proc/sys/fs/inotify/max_user_watches
      $ cat /proc/sys/fs/inotify/max_user_instances
      ``` 
    * To temporarily increase the limits
      ```
      $ sudo sysctl fs.inotify.max_user_instances=8192
      $ sudo sysctl fs.inotify.max_user_watches=524288
      $ sudo sysctl -p
      ``` 
    * To permanently increase the limits
      ```
      $ sudo echo "fs.inotify.max_user_watches=1024" >> /etc/sysctl.conf
      $ sudo echo "fs.inotify.max_user_instances=1024" >> /etc/sysctl.conf
      $ sudo sysctl -p /etc/sysctl.conf #reloads system settings to apply changes
      ``` 
    Once you've increased the limits, delete the clusters already created and try again:
    ```
    $ kind delete clusters hub cluster1 cluster2
    $ ./setup-ocm.sh
    ```


    </details> 

## Install Argo CD

1. Install Argo CD on the OCM hub cluster

    ```bash
    kubectl create namespace argocd
    kubectl apply -n argocd -f https://raw.githubusercontent.com/argoproj/argo-cd/stable/manifests/install.yaml
    ```

    See more informatin from [Argo CD Getting Started](https://argo-cd.readthedocs.io/en/stable/getting_started/).

2. Confirm that all pods are running.

    ```bash
    $ kubectl -n argocd get pods
    argocd-application-controller-0                    1/1     Running        0          22s
    argocd-applicationset-controller-7f466f7cc-9hdlq   1/1     Running        0          22s
    argocd-dex-server-54cd4596c4-t5sb5                 1/1     Running        0          22s
    argocd-notifications-controller-8445d56d96-8745z   1/1     Running        0          22s
    argocd-redis-65596bf87-lkrg2                       1/1     Running        0          22s
    argocd-repo-server-5ccf4bd568-hxlzw                1/1     Running        0          22s
    argocd-server-7dff66c8f8-qxzp9                     1/1     Running        0          22s
    ```

3. Download Argo CD CLI

    Download the latest version of Argo CD CLI `argocd` from https://github.com/argoproj/argo-cd/releases/latest. More detailed installation instructions can be found via the [CLI installation documentation](https://argo-cd.readthedocs.io/en/stable/cli_installation/).

## Integrate OCM with Argo CD

1. Start port forwarding to expose Argo CD API server on the hub cluster.

    ```bash
    kubectl port-forward svc/argocd-server -n argocd 8080:443
    ```

2. Run `./setup-argocd.sh`, you will see two clusters registered on Argo CD.

    ```
    SERVER                          NAME        VERSION  STATUS      MESSAGE
    https://10.0.109.2:11443        cluster2    1.23     Successful  
    https://10.0.109.2:10443        cluster1    1.23     Successful 
    ```

## Deploy an application to managed clusters

1. Create a placement to select a set of managed clusters. 

    ```bash
    kubectl apply -f ./manifests/guestbook-app-placement.yaml
    ```

    Confirm the the placemet selects all managed clusters. 

    ```bash
    $ kubectl -n argocd get placementdecisions -l cluster.open-cluster-management.io/placement=guestbook-app-placement -o yaml
    apiVersion: v1
    items:
    - apiVersion: cluster.open-cluster-management.io/v1beta1
      kind: PlacementDecision
      metadata:
        labels:
          cluster.open-cluster-management.io/placement: guestbook-app-placement
        name: guestbook-app-placement-decision-1
        namespace: argocd
      status:
        decisions:
        - clusterName: cluster1
          reason: ""
        - clusterName: cluster2
          reason: ""
    kind: List
    metadata:
      resourceVersion: ""
      selfLink: ""
    ```

2. Create an Argo CD `ApplicationSet`.

    ```bash
    kubectl apply -f ./manifests/guestbook-app.yaml
    ```
    
    With references to the OCM Placement generator `argocd/ocm-placement-generator` and a placement, an `ApplicationSet` can target the application to the clusters selected by the placement.

3. Confirm the `ApplicationSet` is created and an `Application` is generated for each selected managed cluster.

    ```bash
    $ kubectl -n argocd get applicationsets
    NAME            AGE
    guestbook-app   4s

    $ kubectl -n argocd get applications
    NAME                     SYNC STATUS   HEALTH STATUS
    cluster1-guestbook-app   Synced        Progressing
    cluster2-guestbook-app   Synced        Progressing
    ```

4. Confirm the `Application` is running on the selected managed clusters

    ```bash
    $ kubectl --context kind-cluster1 -n guestbook get pods
    NAME                           READY   STATUS    RESTARTS   AGE
    guestbook-ui-6b689986f-cdrk8   1/1     Running   0          112s

    $ kubectl --context kind-cluster2 -n guestbook get pods
    NAME                           READY   STATUS    RESTARTS   AGE
    guestbook-ui-6b689986f-x9tsq   1/1     Running   0          2m33s
    ```
