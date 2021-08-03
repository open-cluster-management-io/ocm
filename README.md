# Addon Framework

This is to define an addon framework library.

Still in PoC phase.

## Community, discussion, contribution, and support

Check the [CONTRIBUTING Doc](CONTRIBUTING.md) for how to contribute to the repo.

<!--

You can reach the maintainers of this project at:

- [#xxx on Slack](https://slack.com/signin?redir=%2Fmessages%2Fxxx)

-->

------
## Getting Started

### Prerequisites

These instructions assume:

- You have at least one running kubernetes cluster;
- You have already followed instructions from [registration-operator](https://github.com/open-cluster-management-io/registration-operator) and installed OCM successfully;
- A managed cluster with name `cluster1` has been imported and accepted;

### Deploy the helloworld addon
Set environment variables.
```sh
export KUBECONFIG=</path/to/hub_cluster/kubeconfig>
```

Build the docker image to run the sample addon.
```sh
make images
export EXAMPLE_IMAGE_NAME=<helloworld_addon_image_name> # export EXAMPLE_IMAGE_NAME=quay.io/open-cluster-management/helloworld-addon:latest
```

If your are using kind, load image into kind cluster.
```sh
kind load docker-image <helloworld_addon_image_name> # kind load docker-image quay.io/open-cluster-management/helloworld-addon:latest
```

And then deploy helloworld addon contoller on hub cluster
```
make deploy-example
```

Create a addon-cr.yaml as shown in this example:
```yaml
apiVersion: addon.open-cluster-management.io/v1alpha1
kind: ManagedClusterAddOn
metadata:
  name: helloworld
  namespace: cluster1
spec:
  installNamespace: default
```
Apply the yaml file to the hub cluster.

```
kubectl apply -f addon-cr.yaml
```

### What is next
After a successful deployment, check on the managed cluster `cluster1` and see the helloworld addon agent has been deployed from the hub cluster.
```
kubectl --kubeconfig </path/to/managed_cluster/kubeconfig> -n default get pods
NAME                               READY   STATUS    RESTARTS   AGE
helloworld-agent-b99d47f76-v2j6h   1/1     Running   0          53s
```

### Clean up
Remove the addon CR from hub cluster. It will undeploy the helloworld addon agent from the managed cluster `cluster1` as well.
```
kubectl delete --ignore-not-found -f addon-cr.yaml
```

Undeploy helloworld addon contoller from hub cluster.
```
make undeploy-example
```

Follow instructions from [registration-operator](https://github.com/open-cluster-management-io/registration-operator) to uninstall OCM if necessary;

<!--
## XXX References

If you have any further question about xxx, please refer to
[XXX help documentation](docs/xxx_help.md) for further information.
-->