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
- At least one managed cluster has been imported and accepted;

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

And then deploy helloworld addon controller on hub cluster
```
make deploy-example
```
The helloworld addon controller will create one `ManagedClusterAddOn` for each managed cluster automatically to install the helloworld agent on the managed cluster.

### What is next
After a successful deployment, check on the managed cluster and see the helloworld addon agent has been deployed from the hub cluster.
```
kubectl --kubeconfig </path/to/managed_cluster/kubeconfig> -n default get pods
NAME                               READY   STATUS    RESTARTS   AGE
helloworld-agent-b99d47f76-v2j6h   1/1     Running   0          53s
```

### Clean up
Undeploy helloworld addon controller from hub cluster.
```
make undeploy-example
```

Remove the addon CR from hub cluster. It will undeploy the helloworld addon agent from the managed cluster as well.
```
kubectl --kubeconfig </path/to/hub_cluster/kubeconfig> delete managedclusteraddons -n <managed_cluster_name> helloworld
```

Follow instructions from [registration-operator](https://github.com/open-cluster-management-io/registration-operator) to uninstall OCM if necessary;

<!--
## XXX References

If you have any further question about xxx, please refer to
[XXX help documentation](docs/xxx_help.md) for further information.
-->