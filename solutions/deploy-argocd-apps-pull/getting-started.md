# Getting Started

### Prerequisites
- [kind](https://kind.sigs.k8s.io) must be installed on your local machine. The Kubernetes version must be >= 1.19, see [kind user guide](https://kind.sigs.k8s.io/docs/user/quick-start/#creating-a-cluster) for more details.
- 

### Steps
1. Setup an OCM Hub cluster and registered an OCM Managed cluster. 
   ```
   curl -L https://raw.githubusercontent.com/open-cluster-management-io/OCM/main/solutions/setup-dev-environment/local-up.sh | bash
   ```
   
   See [Open Cluster Management Quick Start](https://open-cluster-management.io/getting-started/quick-start/) for more details.

2. Install ArgoCD on the hub cluster and both managed clusters. 
   ```
   kubectl config use-context kind-hub #repeat for kind-cluster1 and kind-cluster2
   kubectl create namespace argocd
   kubectl apply -n argocd -f https://raw.githubusercontent.com/argoproj/argo-cd/stable/manifests/install.yaml
   ```
   See [ArgoCD website](https://argo-cd.readthedocs.io/en/stable/getting_started/) for more details.

3. On the Hub cluster, scale down the Application controller:
    ```
    kubectl -n argocd scale statefulset/argocd-application-controller --replicas 0
    ```
    **Note:** This step is not necssary if the ArgoCD instance you are using contains the feature: 
    https://argo-cd.readthedocs.io/en/latest/user-guide/skip_reconcile/

1. Install the Pull controller:
    ```
    kubectl apply -f https://raw.githubusercontent.com/open-cluster-management-io/argocd-pull-integration/main/deploy/install.yaml
    ```

1. If your controller starts successfully, you should see:
    ```
    $ kubectl -n open-cluster-management get deploy | grep pull
    argocd-pull-integration-controller-manager   1/1     1            1           106s
    ```

1. On the Hub cluster, create an ArgoCD cluster secret that represents the managed cluster. This step can be automated with [OCM auto import controller](https://github.com/open-cluster-management-io/multicloud-integrations/).

    **Note**: replace the `cluster-name` with the registered managed cluster name.
    ```
    cat <<EOF | kubectl apply -f -
    apiVersion: v1
    kind: Secret
    metadata:
      name: <cluster-name>-secret # cluster1-secret
      namespace: argocd
      labels:
        argocd.argoproj.io/secret-type: cluster
    type: Opaque
    stringData:
      name: <cluster-name> # cluster1
      server: https://<cluster-name>-control-plane:6443 # https://cluster1-control-plane:6443
    EOF
    ```

1. On the Hub cluster, apply the manifests in `example/hub`:
    ```
    kubectl apply -f example/hub
    ```

1. On the managed clusters, apply the manifests in `example/managed`:
    ```
    kubectl config use-context kind-cluster1 #repeat for kind-cluster2
    kubectl apply -f example/managed
    ```

1. On the Hub cluster, apply the `guestbook-app-set` manifest:
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

10. When this guestbook ApplicationSet reconciles, it will generate an Application for the registered ManagedCluster. For example:
    ```
    $ kubectl -n argocd get appset
    NAME            AGE
    guestbook-app   84s
    $ kubectl -n argocd get app
    NAME                     SYNC STATUS   HEALTH STATUS
    cluster1-guestbook-app   Synced        Healthy
    cluster2-guestbook-app   Synced        Healthy     
    ```

11. On the Hub cluster, the pull controller will wrap the Application with a ManifestWork. For example:
    ```
    $ kubectl -n cluster1 get manifestwork
    NAME                          AGE
    cluster1-guestbook-app-d0e5   2m41s
    ```

12. On a Managed cluster, you should see that the Application is pulled down successfully. For example:
    ```
    $ kubectl -n argocd get app
    NAME                     SYNC STATUS   HEALTH STATUS
    cluster1-guestbook-app   Synced        Healthy
    $ kubectl -n guestbook get deploy
    NAME           READY   UP-TO-DATE   AVAILABLE   AGE
    guestbook-ui   1/1     1            1           7m36s
    ```

13. On the Hub cluster, the status controller will sync the dormant Application with the ManifestWork status feedback. For example:
    ```
    $ kubectl -n argocd get app
    NAME                     SYNC STATUS   HEALTH STATUS
    cluster1-guestbook-app   Synced        Healthy
    cluster2-guestbook-app   Synced        Healthy
    ```
