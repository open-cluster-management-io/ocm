# klusterlet

The klusterlet provides the registration to the Hub clusters as a managed cluster.
This operator supports the installation and upgrade of klusterlet.

## Prerequisites

You need a Hub cluster which has installed clusterManager operator.

## Get Repo Info

```bash
helm repo add ocm https://open-cluster-management.io/helm-charts
helm repo update
helm search repo ocm
```

## Install the Chart

### Run klusterlet on Default mode

Install the Chart on the managed cluster:

```bash
helm install klusterlet --version <version> ocm/klusterlet \
    --set klusterlet.clusterName=<cluster name> \
    --set-file bootstrapHubKubeConfig=<the bootstrap kubeconfig file of hub cluster> \
    --namespace=open-cluster-management \
    --create-namespace
```

When the klusterlet is installed on the hub cluster.

```bash
helm install klusterlet --version <version> ocm/klusterlet \
    --set klusterlet.clusterName=<cluster name> \
    --set-file bootstrapHubKubeConfig=<the bootstrap kubeconfig file of hub cluster> \
    --namespace=open-cluster-management
```

> Note:
> If the cluster is KinD cluster, the kubeconfig should be internal:: `kind get kubeconfig --name managed --internal`.
> You can create the `bootstrap-hub-kubeconfig` secret in the `open-cluster-management-agent` namespace
> after install the chart without setting `bootstrapHubKubeConfig`.

### Run klusterlet on Hosted mode

Install the Chart on the hosting cluster:

```bash
helm install <klusterlet release name> --version <version> ocm/klusterlet \
    --set klusterlet.name=<klusterlet name>,klusterlet.clusterName=<cluster name>,klusterlet.mode=Hosted \
    --set-file bootstrapHubKubeConfig=<the bootstrap kubeconfig file of hub cluster>,\
      externalManagedKubeConfig=<the kubeconfig file of the managed cluster>
```

### Run klusterlet without operator
In this case, the klusterlet operator has been installed on the cluster.

```bash
    helm install <klusterlet release name> --version <version> ocm/klusterlet \
    --set klusterlet.name=<klusterlet name>,klusterlet.clusterName=<cluster name>,klusterlet.namespace=<klusterlet namespace>,noOperator=true \
    --set-file bootstrapHubKubeConfig=<the bootstrap kubeconfig file of hub cluster>
```


> Note:
> If the cluster is KinD cluster, the kubeconfig should be internal:: `kind get kubeconfig --name managed --internal`.
> You can create the `bootstrap-hub-kubeconfig` and `externalManagedKubeConfig` secrets in the `klusterlet-<cluster name>` namespace
> after install the chart without setting `bootstrapHubKubeConfig` and `externalManagedKubeConfig`.

## Uninstall

### Delete the klusterlet CR

``` bash
kubectl delete klusterlet <klusterlet name>
```

### Uninstall the Chart

```bash
helm uninstall klusterlet --namespace=open-cluster-management
```
