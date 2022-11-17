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

**PLEASE NOTE**: if the server address in kubeconfig is a domain name, the hub api server may not be accessible for `klusterlet` operator、 `registration` and `work` agent. In this case, you need to set hostAlias for [`klusterlet` deployment](deploy/klusterlet/config/operator/operator.yaml#L65) and [`klusterlet` CR](deploy/klusterlet/config/samples/operator_open-cluster-management_klusterlets.cr.yaml#L18) explicitly.

## More details about deployment

We mainly provide deployment in two scenarios:

1. All-in-one: using one cluster as hub and spoke at the same time.
2. Hub-spoke: using one cluster as hub and another cluster as spoke.

### Deploy all-in-on deployment

1. Set the env variable `KUBECONFIG` to kubeconfig file path.

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

   ```shell
   kubectl config use-context {hub-context}
   make deploy-hub
   ```

   **PLEASE NOTE**: If you're running kubernetes in docker, the `server` address in kubeconfig may not be accessible for other clusters. In this case, you need to set `HUB_KUBECONFIG` explicitly.

   For example, if your clusters are created by kind, you need to use kind's command to export a kubeconfig of hub with an accessible `server` address. ([The related issue](https://github.com/kubernetes-sigs/kind/issues/1305))

   ```shell
   kind get kubeconfig --name {your kind cluster name} --internal > ./.hub-kubeconfig # ./.hub-kubeconfig is default value of HUB_KUBECONFIG 
   ```

3. Switch to spoke context and deploy agent components.

   ```shell
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
   kubectl config use-context {spoke-context} 
   make clean-spoke
   ```

### Deploy hub(Clustermanager) with Hosted mode

1. Create 3 Kind clusters: management cluster, hub cluster and a managed cluster.

   ```shell
   kind create cluster --name hub
   cat <<EOF | kind create cluster --name management --config=-
   kind: Cluster
   apiVersion: kind.x-k8s.io/v1alpha4
   nodes:
   - role: control-plane
     extraPortMappings:
     - containerPort: 30443
       hostPort: 30443
       protocol: TCP
     - containerPort: 31443
       hostPort: 31443
       protocol: TCP
   EOF
   kind create cluster --name managed
   ```

2. Set the env variable `KUBECONFIG` to kubeconfig file path.

   ```shell
   export KUBECONFIG=$HOME/.kube/config
   ```

3. Get the `EXTERNAL_HUB_KUBECONFIG` kubeconfig.

   ```shell
   kind get kubeconfig --name hub --internal > ./.external-hub-kubeconfig  
   ```

4. Switch to management cluster and deploy hub components.

   ```shell
   kubectl config use-context {management-context}
   make deploy-hub-hosted
   ```

    After deploy hub successfully, the user needs to expose webhook-servers in the management cluster manually.

    ```shell
    cat <<EOF | kubectl apply -f -
    apiVersion: v1
    kind: Service
    metadata:
      name: cluster-manager-registration-webhook-external
      namespace: cluster-manager
    spec:
      type: NodePort
      selector:
        app: cluster-manager-registration-webhook
      ports:
        - port: 9443
          nodePort: 30443
    ---
    apiVersion: v1
    kind: Service
    metadata:
      name: cluster-manager-work-webhook-external
      namespace: cluster-manager
    spec:
      type: NodePort
      selector:
        app: cluster-manager-work-webhook
      ports:
        - port: 9443
          nodePort: 31443
    EOF
    ```

### Deploy spoke(Klusterlet) with Hosted mode

We support deploy the Klusterlet(registration-agent, work-agent) outside of managed cluster, called `Hosted` mode, and we define the cluster where the Klusterlet runs as management-cluster.

1. Set env variables.

   ```shell
   export KUBECONFIG=$HOME/.kube/config
   ```

2. Switch to hub context and deploy hub components.

   ```shell
   kubectl config use-context {hub-context}
   make deploy-hub
   ```

   **PLEASE NOTE**: If you're running kubernetes in docker, the `server` address in kubeconfig may not be accessible for other clusters. In this case, you need to set `HUB_KUBECONFIG` explicitly.

   For example, if your clusters are created by kind, you need to use kind's command to export a kubeconfig of hub with an accessible `server` address. ([The related issue](https://github.com/kubernetes-sigs/kind/issues/1305))

   ```shell
   kind get kubeconfig --name {kind-hub-cluster-name} --internal > ./.hub-kubeconfig # ./.hub-kubeconfig is default value of HUB_KUBECONFIG
   ```

3. Switch to management context and deploy agent components on management cluster.

    ```shell
    kubectl config use-context {management-context}
    make deploy-spoke-hosted
    ```

   **PLEASE NOTE**: If you're running kubernetes in docker, the `server` address in kubeconfig may not be accessible for other clusters. In this case, you need to set `EXTERNAL_MANAGED_KUBECONFIG` explicitly.

   For example, if your clusters are created by kind, you need to use kind's command to export a kubeconfig of managed/spoke cluster with an accessible `server` address. ([The related issue](https://github.com/kubernetes-sigs/kind/issues/1305))

   ```shell
   kind get kubeconfig --name {kind-managed-cluster-name} --internal > ./.external-managed-kubeconfig # ./.external-managed-kubeconfig is default value of EXTERNAL_MANAGED_KUBECONFIG, it is only useful in Hosted mode.
   ```

4. To clean the hub environment.

   ```shell
   kubectl config use-context {hub-context}
   make clean-hub
   ```

5. To clean the spoke environment.

   ```shell
   kubectl config use-context {management-context}
   make clean-spoke-hosted

## What is next

After a successful deployment, a `certificatesigningrequest` and a `managedcluster` will
be created on the hub.

Switch to hub context and deploy hub components.

```shell
kubectl config use-context {hub-context}
kubectl get csr
```

Next approve the csr and set managedCluster to be accepted by hub with the following command

```shell
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
