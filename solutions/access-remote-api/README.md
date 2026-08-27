# Access APIServer of managed cluster

## Prerequisite

- Set up the dev environment in your local machine following [setup dev environment](../setup-dev-environment).
- helm is installed
- Add ocm helm repo with `helm repo add ocm https://open-cluster-management.io/helm-charts`. If you
  already have an `ocm` repo entry pointing at the old URL, Helm 3.3.2+ will refuse to overwrite it;
  either run `helm repo remove ocm` first, or add `--force-update` to the command above.

## Install cluster-proxy and managed-serviceaccount addon on the clusters

Install cluster-proxy addon:

```
helm install \
    -n open-cluster-management-addon --create-namespace \
    cluster-proxy ocm/cluster-proxy 
```

Check the status of the cluster-proxy addon

```
clusteradm get addon cluster-proxy
```

Install managed-serviceaccount addon:

```
helm install \
    -n open-cluster-management-addon --create-namespace \
    managed-serviceaccount ocm/managed-serviceaccount --take-ownership
```

Note: `--take-ownership` is required here because this chart's `ManagedClusterSetBinding` named
`global` conflicts with a resource of the same name already installed by the `cluster-proxy`
chart above; without it, `helm install` fails with an ownership conflict error.

Check the status of the managed-serviceaccount addon

```
clusteradm get addon managed-serviceaccount
```

## Create a managed service account and set rbac

Create managed-service account on hub

```
kubectl apply -f manifests/managed-sa.yaml
```

create a clusterrolebinding on managed cluster to set permission for this service account

```
clusteradm create work rbac -f manifests/clusterrolebinding.yaml --cluster cluster1
```

## Use the clusteradm proxy command

```
clusteradm proxy kubectl --cluster=cluster1 --sa=test --args="get nodes"
```