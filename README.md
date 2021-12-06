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


## Quickstart

### Prepare

1. Clone this repo:
  ```
  git clone https://github.com/open-cluster-management-io/registration.git && cd registration
  ```

2. Prepare a [kind](https://kind.sigs.k8s.io/) cluster, like:
  ```
  kind create cluster # if you want to deploy on two clusters, you can exec `kind create cluster name=<cluster-name> to prepare another cluster`
  ```

  > Note: The Kubernetes cluster needs v1.19 or greater

### Deploy

#### Deploy on one single cluster

1. Export your kind cluster config, like:
  ```
  export KUBECONFIG=$HOME/.kube/config
  ```

2. Override the docker image (optional)
```sh
export IMAGE_NAME=<your_own_image_name> # export IMAGE_NAME=quay.io/open-cluster-management/registration:latest
```

3. Deploy the hub control plane and the registration agent:
  ```
  make deploy
  ```

#### Deploy on two clusters

1. Set environment variables.

- Hub and managed cluster share a kubeconfig file
    ```sh
    export KUBECONFIG=</path/to/kubeconfig>
    export HUB_KUBECONFIG_CONTEXT=<hub-context-name>
    export SPOKE_KUBECONFIG_CONTEXT=<spoke-context-name>
    ```
- Hub and managed cluster use different kubeconfig files.
    ```sh
    export HUB_KUBECONFIG=</path/to/hub_cluster/kubeconfig>
    export SPOKE_KUBECONFIG=</path/to/managed_cluster/kubeconfig>
    ```

2. Set cluster ip if you are deploying on KIND clusters.
```sh
export CLUSTER_IP=<host_name/ip_address>:<port> # export CLUSTER_IP=hub-control-plane:6443
```
If you are not using KIND, you can get the above information with command below.
```sh
kubectl --kubeconfig </path/to/hub_cluster/kubeconfig> -n kube-public get configmap cluster-info -o yaml
```

3. Override the docker image (optional)
```sh
export IMAGE_NAME=<your_own_image_name> # export IMAGE_NAME=quay.io/open-cluster-management/registration:latest
```

4. Deploy the hub control plane and the registration agent
```
make deploy
```

### Approve your cluster

You now have a cluster with registration up and running. The cluster has been registered to the hub.

Next you need to approve your cluster like this:

1. Approve the managed cluster
  ```
  kubectl config use-context <hub-context-name>
  kubectl patch managedcluster cluster1 -p='{"spec":{"hubAcceptsClient":true}}' --type=merge
  ```

2. Apporve the CSR of the managed clsuter
  ```
  kubectl get csr -l open-cluster-management.io/cluster-name=cluster1 | grep Pending | awk '{print $1}' | xargs kubectl certificate approve
  ```

3. Finally, you can find the managed cluster is joined and available
  ```
  kubectl get managedcluster

  NAME                HUB ACCEPTED   MANAGED CLUSTER URLS   JOINED   AVAILABLE   AGE
  cluster1            true                                  True     True        2m21s
  ```

You can find more details for cluster join process from this [design doc](https://github.com/open-cluster-management-io/api/blob/main/docs/clusterjoinprocess.md), and after the registration is deployed, you can try the following features

### Cluster Set

1. Create a cluster set by `ManagedClusterSet` API
  ```
  cat << EOF | kubectl apply -f -
  apiVersion: cluster.open-cluster-management.io/v1beta1
  kind: ManagedClusterSet
  metadata:
    name: clusterset1
  EOF
  ```
2. Add your cluster to the created cluster
  ```
  kubectl label managedclusters cluster1 "cluster.open-cluster-management.io/clusterset=clusterset1" --overwrite
  ```

3. Then, you can find there is one managed cluster is selected from the managed cluster set status, e.g:
  ```
  kubectl get managedclustersets clusterset1 -o jsonpath='{.status.conditions[?(@.type=="ClusterSetEmpty")]}'

  {"lastTransitionTime":"2021-08-17T06:18:26Z","message":"1 ManagedClusters selected","reason":"ClustersSelected","status":"False","type":"ClusterSetEmpty"}
  ```

You can find more details from the [managed cluster set design doc](https://github.com/open-cluster-management-io/api/blob/main/docs/clusterset.md)

### Cluster Claim

1. Create a `ClusterClaim` to claim the ID of this cluster
  ```
  kubectl config use-context <spoke-context-name>

  cat << EOF | kubectl apply -f -
  apiVersion: cluster.open-cluster-management.io/v1alpha1
  kind: ClusterClaim
  metadata:
    name: id.k8s.io
  spec:
    value: cluster1
  EOF
  ```

2. Then, you can find the claim from the managed cluster status, like:
  ```
  kubectl config use-context <hub-context-name>

  kubectl get managedcluster cluster1 -o jsonpath='{.status.clusterClaims}'

  [{"name":"id.k8s.io","value":"cluster1"}]
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

## Community, discussion, contribution, and support

Check the [CONTRIBUTING Doc](CONTRIBUTING.md) for how to contribute to the repo.

### Communication channels

Slack channel: [#open-cluster-mgmt](http://slack.k8s.io/#open-cluster-mgmt)

## License

This code is released under the Apache 2.0 license. See the file LICENSE for more information.


<!--
## XXX References

If you have any further question about xxx, please refer to
[XXX help documentation](docs/xxx_help.md) for further information.
-->
