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

## Get started with [Kind](https://kind.sigs.k8s.io/)

1. Create a cluster with kind
   ```shell
   kind create cluster 
   ```

2. Deploy
   ```shell
   export KUBECONFIG=$HOME/.kube/config
   make deploy
   ```

## More details about deployment

We mainly provide deployment in two scenarios:
1. All-in-one: using one cluster as hub and spoke at the same time.
2. Hub-spoke: using one cluster as hub and another cluster as spoke.

### Deploy all-in-on deployment

1. Set an env variable `KUBECONFIG` to kubeconfig file path. 
   ```shell
   export KUBECONFIG=$HOME/.kube/config
   ```
2. Deploy all components on the cluster.
   ```shell
   make deploy
   ```
3. To clean the environment, run `make clean-deploy`

### Deploy hub-spoke deployment

1. Set env variables.
   ```shell
   export KUBECONFIG=$HOME/.kube/config
   ```
2. Switch to hub context and deploy hub components.
   ```
   kubectl config use-context {hub-context}
   make deploy-hub
   ```
   **PLEASE NOTE**: If you're running kubernetes in docker, the `server` address in kubeconfig may not be accessible for other clusters. In this case, you need to set `HUB_KUBECONFIG` explicitly.

   For example, if your clusters are created by kind, you need to use kind's command to export a kubeconfig of hub with an accessible `server` address. ([The related issue](https://github.com/kubernetes-sigs/kind/issues/1305))

   ```shell
   kind get kubeconfig --name {your kind cluster name} --internal > ./.hub-kubeconfig # ./.hub-kubeconfig is default value of HUB_KUBECONFIG 
   ```
3. Switch to spoke context and deploy agent components.
    ```
    kubectl config use-context {spoke context}
    make deploy-spoke
    ```
4. To clean the hub environment.
   ```shell
   kubectl config use-context {hub-context} 
   make clean-hub
   ```
5. To clean the spoke environment.
   ```shell
   kubectl config use-context {spoke context} 
   make clean-spoke
   ``` 

### Deploy spoke(Klusterlet) with Detached mode

We support deploy the Klusterlet(registration-agent, work-agent) outside of managed cluster, called `Detached` mode, and we define the cluster where the Klusterlet runs as management-cluster.

1. Set env variables.
   ```shell
   export KUBECONFIG=$HOME/.kube/config
   ```
2. Switch to hub context and deploy hub components.
   ```
   kubectl config use-context {hub-context}
   make deploy-hub
   ```
   **PLEASE NOTE**: If you're running kubernetes in docker, the `server` address in kubeconfig may not be accessible for other clusters. In this case, you need to set `HUB_KUBECONFIG` explicitly.

   For example, if your clusters are created by kind, you need to use kind's command to export a kubeconfig of hub with an accessible `server` address. ([The related issue](https://github.com/kubernetes-sigs/kind/issues/1305))

   ```shell
   kind get kubeconfig --name {kind-hub-cluster-name} --internal > ./.hub-kubeconfig # ./.hub-kubeconfig is default value of HUB_KUBECONFIG
   ```
3. Switch to management context and deploy agent components on management cluster.
    ```
    kubectl config use-context {management-context}
    make deploy-spoke-detached
    ```

   **PLEASE NOTE**: If you're running kubernetes in docker, the `server` address in kubeconfig may not be accessible for other clusters. In this case, you need to set `EXTERNAL_MANAGED_KUBECONFIG` explicitly.

   For example, if your clusters are created by kind, you need to use kind's command to export a kubeconfig of managed/spoke cluster with an accessible `server` address. ([The related issue](https://github.com/kubernetes-sigs/kind/issues/1305))

   ```shell
   kind get kubeconfig --name {kind-managed-cluster-name} --internal > ./.external-managed-kubeconfig # ./.external-managed-kubeconfig is default value of EXTERNAL_MANAGED_KUBECONFIG, it is only useful in Detached mode.
   ```
4. To clean the hub environment.
   ```shell
   kubectl config use-context {hub-context}
   make clean-hub
   ```
5. To clean the spoke environment.
   ```shell
   kubectl config use-context {management-context}
   make clean-spoke-detached

## What is next

After a successful deployment, a `certificatesigningrequest` and a `managedcluster` will
be created on the hub.

Switch to hub context and deploy hub components.
```
kubectl config use-context {hub-context}
kubectl get csr
```
Next approve the csr and set managedCluster to be accepted by hub with the following command
```
kubectl certificate approve {csr name}
kubectl patch managedcluster {cluster name} -p='{"spec":{"hubAcceptsClient":true}}' --type=merge
kubectl get managedcluster
```

## Community, discussion, contribution, and support

Check the [CONTRIBUTING Doc](CONTRIBUTING.md) for how to contribute to the repo.

### Communication channels

Slack channel: [#open-cluster-mgmt](http://slack.k8s.io/#open-cluster-mgmt)

## License

This code is released under the Apache 2.0 license. See the file LICENSE for more information.
