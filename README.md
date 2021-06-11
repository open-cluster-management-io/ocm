# Cluster Registration

Contains controllers that support:

- the registration of managed clusters to a hub to place them under management
  (see [cluster join process](https://github.com/open-cluster-management-io/api/blob/main/docs/clusterjoinprocess.md) for design deatails)
- the concept of clusterset (see [KEP-1645](https://github.com/kubernetes/enhancements/tree/master/keps/sig-multicluster/1645-multi-cluster-services-api) for details)
  by `ManagedClusterSet` API to group managed clusters
  (see [managed cluster set](https://github.com/open-cluster-management-io/api/blob/main/docs/clusterset.md) for design deatails)
- the concept of clusterclaim (see [KEP-2149](https://github.com/kubernetes/enhancements/tree/master/keps/sig-multicluster/2149-clusterid) for details)
  by `ManagedClusterClaim` API to collect the cluster information from a managed cluster
  (see [cluster claim](https://github.com/open-cluster-management-io/api/blob/main/docs/clusterset.md) for design deatails)
- the management of [managed cluster add-ons](https://github.com/open-cluster-management-io/api/blob/main/addon/v1alpha1/types_managedclusteraddon.go)
  (see [managed cluster addons management](https://github.com/open-cluster-management-io/enhancements/tree/main/enhancements/sig-architecture/12-addon-manager) for design deatails)

## Community, discussion, contribution, development and support

Check the [CONTRIBUTING Doc](CONTRIBUTING.md) for how to contribute to the repo.

<!--

You can reach the maintainers of this project at:

- [#xxx on Slack](https://slack.com/signin?redir=%2Fmessages%2Fxxx)

-->

------

## Quickstart

1. Clone this repo:
  ```
  git clone https://github.com/open-cluster-management-io/registration.git
  ```

2. Prepare a [kind](https://kind.sigs.k8s.io/) cluster, like:
  ```
  kind create cluster
  ```

  > Note: The Kubernetes cluster needs v1.19 or greater

3. Export your kind cluster config, like:
  ```
  export KUBECONFIG=$HOME/.kube/config
  ```

4. Deploy the hub control plane:
  ```
  make deploy-hub
  make deploy-webhook
  ```

5. Deploy the registraion agent:
  ```
  make bootstrap-secret
  make deploy-spoke
  ```

You now have a cluster with registraion up and running. The cluster has been registered to itself.

Next you need to approve your cluster like this:

1. Approve the managed cluster
  ```
  kubectl patch managedcluster local-development -p='{"spec":{"hubAcceptsClient":true}}' --type=merge
  ```

2. Apporve the CSR of the managed clsuter
  ```
  kubectl get csr -l open-cluster-management.io/cluster-name=local-development | grep Pending | awk '{print $1}' | xargs kubectl certificate approve
  ```

3. Finally, you can find the managed cluster is joined and available
  ```
  kubectl get managedcluster

  NAME                HUB ACCEPTED   MANAGED CLUSTER URLS   JOINED   AVAILABLE   AGE
  local-development   true                                  True     True        2m21s
  ```

You can find more details for cluster join process from this [design doc](https://github.com/open-cluster-management-io/api/blob/main/docs/clusterjoinprocess.md), and after the registration is deployed, you can try the following features

### Cluster Set

1. Create a cluster set by `ManagedClusterSet` API
  ```
  cat << EOF | kubectl apply -f -
  apiVersion: cluster.open-cluster-management.io/v1alpha1
  kind: ManagedClusterSet
  metadata:
    name: clusterset1
  EOF
  ```
2. Add your cluster to the created cluster
  ```
  kubectl label managedclusters local-development "cluster.open-cluster-management.io/clusterset=clusterset1" --overwrite
  ```

3. Then, you can find there is one managed cluster is selected from the managed cluster set status, like:
  ```
  kubectl get managedclustersets clusterset1 -o jsonpath='{.status.conditions[?(@.type=="ClusterSetEmpty")]}'

  {"message":"1 ManagedClusters selected","reason":"ClustersSelected"}
  ```

You can find more details from the [managed cluster set design doc](https://github.com/open-cluster-management-io/api/blob/main/docs/clusterset.md)

### Cluster Claim

1. Create a `ClusterClaim` to claim the ID of this cluster
  ```
  cat << EOF | kubectl apply -f -
  apiVersion: cluster.open-cluster-management.io/v1alpha1
  kind: ClusterClaim
  metadata:
    name: id.k8s.io
  spec:
    value: local-development
  EOF
  ```

2. Then, you can find the claim from the managed cluster status, like:
  ```
  kubectl get managedcluster local-development -o jsonpath='{.status.clusterClaims}'

  [{"name":"id.k8s.io","value":"local-development"}]
  ```

You can find more details from the [cluster claim design doc](https://github.com/open-cluster-management-io/enhancements/tree/main/enhancements/sig-architecture/4-cluster-claims)

### Managed Cluster Add-Ons

A managed cluster add-ons is deployed on the managed cluster to extend the capability of managed
cluster. Developers can leverage [add-on framework](https://github.com/open-cluster-management-io/addon-framework)
to implement their add-ons. The registration provides the management of the lease update and
registration for all managed cluster addons, you can find more details from the
[Managed cluster addons management design doc](https://github.com/open-cluster-management-io/enhancements/tree/main/enhancements/sig-architecture/12-addon-manager)

> Note: The addon-management is in alpha stage, it is not enabled by default, it is controlled by
> feature gate `AddonManagement`

<!--
## XXX References

If you have any further question about xxx, please refer to
[XXX help documentation](docs/xxx_help.md) for further information.
-->
