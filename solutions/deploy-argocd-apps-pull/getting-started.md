# Getting Started

### Prerequisites
- [kind](https://kind.sigs.k8s.io) must be installed on your local machine. The Kubernetes version must be >= 1.19. See the [kind user guide](https://kind.sigs.k8s.io/docs/user/quick-start/#creating-a-cluster) for more details.
- Download and install [clusteradm](https://github.com/open-cluster-management-io/clusteradm/releases). For Linux OS, run the following commands:

    ```
    wget -qO- https://github.com/open-cluster-management-io/clusteradm/releases/latest/download/clusteradm_linux_amd64.tar.gz | sudo tar -xvz -C /usr/local/bin/

    sudo chmod +x /usr/local/bin/clusteradm
    ```
- The [kubectl](https://kubernetes.io/docs/reference/kubectl/) cli, which should be compatible with your Kubernetes version. See [install and setup](https://kubernetes.io/docs/tasks/tools/install-kubectl-linux/#before-you-begin) for more info.

### Steps
1. Setup an OCM Hub cluster and registered an OCM Managed cluster. 
   ```
   curl -L https://raw.githubusercontent.com/open-cluster-management-io/OCM/main/solutions/setup-dev-environment/local-up.sh | bash
   ```
   
   See [Open Cluster Management Quick Start](https://open-cluster-management.io/getting-started/quick-start/) for more details.

2. Install ArgoCD on the hub cluster and both managed clusters. 
    ```
    for i in "hub" "cluster1" "cluster2"
    do
      kubectl config use-context kind-$i
      kubectl create namespace argocd
      kubectl apply -n argocd -f https://raw.githubusercontent.com/argoproj/argo-cd/stable/manifests/install.yaml
    done
    ```
   See [ArgoCD website](https://argo-cd.readthedocs.io/en/stable/getting_started/) for more details.

1. Install the Pull controller on the hub cluster:
    ```
    kubectl config use-context kind-hub
    kubectl apply -f https://raw.githubusercontent.com/open-cluster-management-io/argocd-pull-integration/main/deploy/install.yaml
    ```

2. If your controller starts successfully, you should see:
    ```
    $ kubectl -n open-cluster-management get deploy | grep pull
    argocd-pull-integration-controller-manager   1/1     1            1           106s
    ```

3. On the Hub cluster, create ArgoCD cluster secrets that represent the managed clusters. This step can be automated with [OCM auto import controller](https://github.com/open-cluster-management-io/multicloud-integrations/).

    ```
    for i in "cluster1" "cluster2"
    do
      cat <<EOF | kubectl apply -f -
      apiVersion: v1
      kind: Secret
      metadata:
        name: $i-secret # cluster1-secret
        namespace: argocd
        labels:
          argocd.argoproj.io/secret-type: cluster
      type: Opaque
      stringData:
        name: $i # cluster1
        server: https://$i-control-plane:6443 # https://cluster1-control-plane:6443
    EOF
    done
    ```

4. On the Hub cluster, apply the manifests in `example/hub`:
    ```
    kubectl config use-context kind-hub
    kubectl apply -f example/hub
    ```

5. On the managed clusters, apply the manifests in `example/managed`:
    ```
    for i in "cluster1" "cluster2"
    do
      kubectl config use-context kind-$i
      kubectl apply -f example/managed
    done
    ```

6. On the Hub cluster, apply the `guestbook-app-set` manifest:
    ```
    kubectl apply -f example/guestbook-app-set.yaml
    ```
    **Note:** The Application template inside the ApplicationSet must contain the following content:
    ```
          labels:
            apps.open-cluster-management.io/pull-to-ocm-managed-cluster: 'true'
          annotations:
            argocd.argoproj.io/skip-reconcile: 'true'
            apps.open-cluster-management.io/ocm-managed-cluster: '{{name}}'
    ```
    The label allows the pull model controller to select the Application for processing.

    The `skip-reconcile` annotation is to prevent the Application from reconciling on the Hub cluster.

    The `ocm-managed-cluster` annotation is for the ApplicationSet to generate multiple Application based on each cluster generator targets.

7.  When this guestbook ApplicationSet reconciles, it will generate an Application for the registered managed clusters. For example:
    ```
    $ kubectl -n argocd get appset
    NAME            AGE
    guestbook-app   84s
    $ kubectl -n argocd get app
    NAME                     SYNC STATUS   HEALTH STATUS
    cluster1-guestbook-app   Synced        Healthy
    cluster2-guestbook-app   Synced        Healthy     
    ```

8.  On the Hub cluster, the pull controller will wrap the Application with a ManifestWork. For example:
    ```
    $ kubectl -n cluster1 get manifestwork
    NAME                          AGE
    cluster1-guestbook-app-d0e5   2m41s
    ```

9.  On a managed cluster, you should see that the Application is pulled down successfully. For example:
    ```
    $ kubectl -n argocd get app
    NAME                     SYNC STATUS   HEALTH STATUS
    cluster1-guestbook-app   Synced        Healthy
    $ kubectl -n guestbook get deploy
    NAME           READY   UP-TO-DATE   AVAILABLE   AGE
    guestbook-ui   1/1     1            1           7m36s
    ```

10. On the Hub cluster, the status controller will sync the dormant Application with the ManifestWork status feedback. For example:
    ```
    $ kubectl -n argocd get app
    NAME                     SYNC STATUS   HEALTH STATUS
    cluster1-guestbook-app   Synced        Healthy
    cluster2-guestbook-app   Synced        Healthy
    ```
If you have issues or need help troubleshooting, check out the [troubleshooting guide](./troubleshooting.md)