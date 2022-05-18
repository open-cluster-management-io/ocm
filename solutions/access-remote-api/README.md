# Access APIServer of managed cluster

## Prerequisite

- Set up the dev environment in your local machine following [setup dev environment](../setup-dev-environment).
- helm is installed
- Add ocm helm repo with `helm repo add ocm https://openclustermanagement.blob.core.windows.net/releases/`

## Install cluster-proxy and managed-serviceaccount addon on the clusters

Install cluster-proxy addon:

```
helm install \
    -n open-cluster-management-addon --create-namespace \
    cluster-proxy ocm/cluster-proxy 
```

Install managed-serviceaccount addon:

```
helm install \
    -n open-cluster-management-addon --create-namespace \
    managed-serviceaccount ocm/managed-serviceaccount
```

## Create a managed service account and set rbac

Create managed-service account on hub

```
kubectl apply -f manifests/managed-sa.yaml
```

create a clusterrolebinding on managed cluster to set permission for this service account

```
clusteradm create work -f manifests/clusterrolebinding.yaml
```
