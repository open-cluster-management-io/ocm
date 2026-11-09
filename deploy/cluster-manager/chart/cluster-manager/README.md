# cluster-manager

The cluster-manager provides the multicluster hub, which can manage Kubernetes-based clusters across data centers,
public clouds, and private clouds. This operator supports the installation and upgrade of ClusterManager.

## Get Repo Info

```bash
helm repo add ocm https://open-cluster-management.io/helm-charts
helm repo update
helm search repo ocm
```

## Install the Chart

For example, install the chart into `open-cluster-management` namespace.

```bash
helm install cluster-manager  --version <version> ocm/cluster-manager --namespace=open-cluster-management --create-namespace
```

### Hosted mode

Hosted mode keeps the operator and OCM controllers in the release cluster while
the `ClusterManager` controller reads and writes hub resources through an
external kubeconfig. Before installing the chart, create a namespace named
`cluster-manager` in the release cluster and add an `external-hub-kubeconfig`
Secret with a `kubeconfig` data key. Set `clusterManager.mode` to `Hosted` and
provide reachable webhook addresses, for example:

```yaml
clusterManager:
  mode: Hosted
  hosted:
    registrationWebhookConfiguration:
      address: 192.0.2.10
      port: 30443
      bindConfiguration:
        port: 30443
        hostNetwork: true
    workWebhookConfiguration:
      address: 192.0.2.10
      port: 31443
      bindConfiguration:
        port: 31443
        hostNetwork: true
```

The API server serving the hub must be able to resolve and reach the webhook
addresses. Hosted add-on conversion routing is not configured by this chart.

## Uninstall

### Delete all managedClusters before uninstall the Chart

```bash
kubectl get managedcluster | awk '{print $1}' | xargs kubectl delete managedcluster
```

### And then delete the clusterManager CR

``` bash
kubectl delete clustermanagers cluster-manager
```

### Uninstall the Chart

```bash
helm uninstall cluster-manager --namespace=open-cluster-management
```
