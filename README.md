# Registration Operator

The Registration Operator has 2 operators, **Cluster Manager** and **Klusterlet**.
**Cluster Manager** installs the foundational components of OCM for the Hub cluster.
And we can use the **Klusterlet** to install the agent components on the manged clusters when we import the manged clusters to the Hub.

The 2 operators are available on OperatorHub [Cluster Manager](https://operatorhub.io/operator/cluster-manager) and [Klusterlet](https://operatorhub.io/operator/klusterlet).

## Concepts

### Cluster Manager

The operator ClusterManager configures the controllers on the hub that govern [registration](https://github.com/open-cluster-management-io/registration), [placement](https://github.com/open-cluster-management-io/placement) and [work](https://github.com/open-cluster-management-io/work) distribution for attached Klusterlets.

The controllers are all deployed in _open-cluster-management-hub_ namespace on the Hub cluster.

### Klusterlet

The operator Klusterlet represents the agent controllers [registration](https://github.com/open-cluster-management-io/registration) and [work](https://github.com/open-cluster-management-io/work) on the managed cluster.
The Klusterlet requires a secret named of _bootstrap-hub-kubeconfig_ in the same namespace to allow API requests to the hub for the registration protocol.

The controllers are all deployed in _open-cluster-management-agent_ namespace by default. The namespace can be specified in Klusterlet CR.

## Guides

## Deploy all-in-one deployment on kind

1. Create a kind cluster
    ```
    kind create cluster --name cluster1
    kind get kubeconfig --name cluster1 > ./.kubeconfig
    ```
2. Deploy all components on the kind cluster
    ```
    export MANAGED_CLUSTER=cluster1
    make deploy
    ```
3. To clean the environment, run `make clean-deploy`

## Deploy on OCP

1. Deploy hub component
    ```
    export OLM_NAMESPACE=openshift-operator-lifecycle-manager
    make deploy-hub
    ```
2. Deploy agent component
    ```
    export KLUSTERLET_KUBECONFIG_CONTEXT={kube config context of managed cluster}
    export OLM_NAMESPACE=openshift-operator-lifecycle-manager
    make deploy-spoke
    ```
3. To clean the environment, run `make clean-hub` and `make clean-spoke`

## What is next

After a successful deployment, a `certificatesigningrequest` and a `managedcluster` will
be created on the hub.

```
kubectl get csr
kubectl get managedcluster
```

Next approve the csr and set managecluster to be accepcted by hub with the following command

```
kubectl certificate approve {csr name}
kubectl patch managedcluster {cluster name} -p='{"spec":{"hubAcceptsClient":true}}' --type=merge
```

## Community, discussion, contribution, and support

Check the [CONTRIBUTING Doc](CONTRIBUTING.md) for how to contribute to the repo.

### Communication channels

Slack channel: [#open-cluster-mgmt](http://slack.k8s.io/#open-cluster-mgmt)

## License

This code is released under the Apache 2.0 license. See the file LICENSE for more information.
