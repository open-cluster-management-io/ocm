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
   
   See [Open Cluster Management (OCM) Quick Start](https://open-cluster-management.io/getting-started/quick-start/) for more details.

1. Install Argo CD on the Hub cluster.
    ```
    kubectl config use-context kind-hub
    kubectl create namespace argocd
    kubectl apply -n argocd -f https://raw.githubusercontent.com/argoproj/argo-cd/stable/manifests/install.yaml
    ```
   See [Argo CD website](https://argo-cd.readthedocs.io/en/stable/getting_started/) for more details.

1. Install the OCM Argo CD add-on on the Hub cluster:
    ```
    kubectl config use-context kind-hub
    clusteradm install hub-addon --names argocd
    ```
   If your hub controller starts successfully, you should see:
    ```
    $ kubectl -n argocd get deploy argocd-pull-integration
    NAME                      READY   UP-TO-DATE   AVAILABLE   AGE
    argocd-pull-integration   1/1     1            1           55s
    ```

1. Enable the add-on for your choice of managed clusters:
    ```
    kubectl config use-context kind-hub
    clusteradm addon enable --names argocd --clusters cluster1,cluster2
    ```
   Replace `cluster1` and `cluster2` with your managed cluster names.

   If your add-on starts successfully, you should see:
    ```
    $ kubectl -n cluster1 get managedclusteraddon argocd
    NAME     AVAILABLE   DEGRADED   PROGRESSING
    argocd   True                   False
    ```

1. On the Hub cluster, apply the `guestbook-app-set` manifest:
    ```
    kubectl config use-context kind-hub
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

1.  When this guestbook ApplicationSet reconciles, it will generate an Application for the registered managed clusters. For example:
    ```
    $ kubectl config use-context kind-hub
    $ kubectl -n argocd get appset
    NAME            AGE
    guestbook-app   84s
    $ kubectl -n argocd get app
    NAME                     SYNC STATUS   HEALTH STATUS
    cluster1-guestbook-app
    cluster2-guestbook-app
    ```

1.  On the Hub cluster, the pull controller will wrap the Application with a ManifestWork. For example:
    ```
    $ kubectl config use-context kind-hub
    $ kubectl -n cluster1 get manifestwork
    NAME                          AGE
    cluster1-guestbook-app-d0e5   2m41s
    ```

1.  On a managed cluster, you should see that the Application is pulled down successfully. For example:
    ```
    $ kubectl config use-context kind-cluster1
    $ kubectl -n argocd get app
    NAME                     SYNC STATUS   HEALTH STATUS
    cluster1-guestbook-app   Synced        Healthy
    $ kubectl -n guestbook get deploy
    NAME           READY   UP-TO-DATE   AVAILABLE   AGE
    guestbook-ui   1/1     1            1           7m36s
    ```

1. On the Hub cluster, the status controller will sync the dormant Application with the ManifestWork status feedback. For example:
    ```
    $ kubectl config use-context kind-hub
    $ kubectl -n argocd get app
    NAME                     SYNC STATUS   HEALTH STATUS
    cluster1-guestbook-app   Synced        Healthy
    cluster2-guestbook-app   Synced        Healthy
    ```
If you have issues or need help troubleshooting, check out the [troubleshooting guide](./troubleshooting.md)
