# ManifestWork

Support a primitive that enables resources to be applied to a managed cluster.

<!--

You can reach the maintainers of this project at:

- [#xxx on Slack](https://slack.com/signin?redir=%2Fmessages%2Fxxx)

-->

------
## Description

### Work agent
Work agent is a controller running on the managed cluster. It watches the `ManifestWorks` CRs in a certain namespace on hub cluster and applies the manifests included in those CRs on the managed clusters.

### Work webhook
Work webhook is an admission webhook running on hub cluster to ensure the content of `ManifestWorks` created/updated are valid.

## Getting Started

### Prerequisites

These instructions assume:

- You have at least one running kubernetes cluster;
- The name of the managed cluster is `cluster1`;

### Deploy

#### Deploy on one single cluster
Set environment variables.

```sh
export KUBECONFIG=</path/to/kubeconfig>
```

Override the docker image (optional)
```sh
export IMAGE_NAME=<your_own_image_name> # export IMAGE_NAME=quay.io/open-cluster-management/work:latest
```

And then deploy work webhook and work agent
```
make deploy
```

#### Deploy on two clusters

Set environment variables. 

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

Set cluster ip if you are deploying on KIND clusters.
```sh
export CLUSTER_IP=<host_name/ip_address>:<port> # export CLUSTER_IP=hub-control-plane:6443
```
You can get the above information with command below.
```sh
kubectl --kubeconfig </path/to/hub_cluster/kubeconfig> -n kube-public get configmap cluster-info -o yaml
```

Override the docker image (optional)
```sh
export IMAGE_NAME=<your_own_image_name> # export IMAGE_NAME=quay.io/open-cluster-management/work:latest
```

And then deploy work webhook and work agent
```
make deploy
```

### What is next
After a successful deployment, a namespace `cluster1` is created on the hub cluster. You are able to create `ManifestWorks` in this namespace, and the workload will be distributed to the managed cluster and then be applied by the work agent.

Create a manifest-work.yaml as shown in this example:
```yaml
apiVersion: work.open-cluster-management.io/v1
kind: ManifestWork
metadata:
  name: hello-work
  namespace: cluster1
  labels:
    app: hello
spec:
  workload:
    manifests:
      - apiVersion: apps/v1
        kind: Deployment
        metadata:
          name: hello
          namespace: default
        spec:
          selector:
            matchLabels:
              app: hello
          template:
            metadata:
              labels:
                app: hello
            spec:
              containers:
                - name: hello
                  image: quay.io/asmacdo/busybox
                  command: ['sh', '-c', 'echo "Hello, Kubernetes!" && sleep 3600']
```
Apply the yaml file to the hub cluster.

```
kubectl --kubeconfig </path/to/hub_cluster/kubeconfig> apply -f manifest-work.yaml
```

Verify that the `ManifestWork` resource was applied to the hub.

```
kubectl --kubeconfig </path/to/hub_cluster/kubeconfig> -n cluster1 get manifestwork/hello-work -o yaml
```

```yaml
apiVersion: work.open-cluster-management.io/v1
kind: ManifestWork
metadata:
  labels:
    app: hello
  name: hello-work
  namespace: cluster1
spec:
  ... ...
status:
  conditions:
    - lastTransitionTime: '2021-06-15T02:26:02Z'
      message: Apply manifest work complete
      reason: AppliedManifestWorkComplete
      status: 'True'
      type: Applied
    - lastTransitionTime: '2021-06-15T02:26:02Z'
      message: All resources are available
      reason: ResourcesAvailable
      status: 'True'
      type: Available
  resourceStatus:
    manifests:
      - conditions:
          - lastTransitionTime: '2021-06-15T02:26:02Z'
            message: Apply manifest complete
            reason: AppliedManifestComplete
            status: 'True'
            type: Applied
          - lastTransitionTime: '2021-06-15T02:26:02Z'
            message: Resource is available
            reason: ResourceAvailable
            status: 'True'
            type: Available
        resourceMeta:
          group: apps
          kind: Deployment
          name: hello
          namespace: default
          ordinal: 0
          resource: deployments
          version: v1
```

As shown above, the status of the `ManifestWork` includes the conditions for both the whole `ManifestWork` and each of the manifest it contains. And there are two condition types:
- **Applied**. If true, it indicates the whole `ManifestWork` (or a particular manifest) has been applied on the managed cluster; otherwise `reason`/`message` of the condition will show more information for troubleshooting.
- **Available**. If true, it indicates the corresponding Kubernetes resources of the  whole `ManifestWork` (or a particular manifest) are available on the managed cluster; otherwise `reason`/`message` of the condition will show more information for troubleshooting

Check on the managed cluster and see the `Pod` has been deployed from the hub cluster.
```
kubectl --kubeconfig </path/to/managed_cluster/kubeconfig> -n default get pod
NAME                     READY   STATUS    RESTARTS   AGE
hello-64dd6fd586-rfrkf   1/1     Running   0          108s
```

### Clean up
To clean the environment
```
make undeploy
```

### Run e2e test cases as sanity check on an existing environment

In order to verify the `ManifestWork` API on an existing environment with work webhook/agent installed and well configured, you are able to run the e2e test cases as sanity check by following the steps below.

Build the binary of the e2e test cases
```
make build-e2e
```

And then run the e2e test cases on a certian managed cluster e.g. cluster1.
```
./e2e.test --ginkgo.v --ginkgo.label-filter=sanity-check -hub-kubeconfig=/path/to/file -cluster-name=cluster1 -managed-kubeconfig=/path/to/file
```

By setting the deployment name of the work valiating webhook on the hub cluster, the test cases will not start running until the webhook is ready.
```
./e2e.test --ginkgo.v --ginkgo.label-filter=sanity-check -hub-kubeconfig=/path/to/file -cluster-name=cluster1 -managed-kubeconfig=/path/to/file -webhook-deployment-name=work-webhook
```

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
