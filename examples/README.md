
# Getting Started

We have 2 AddOn examples for user to understand how Addon works and how to develop an AddOn.

The [helloworld example](helloworld) is implemented using Go templates, and the [helloworld_helm example](helloworld_helm) is implemented using Helm Chart.

You can get more details in the [docs](../docs).
## Prerequisites

These instructions assume:

- You have at least one running kubernetes cluster;
- You have already followed instructions from [registration-operator](https://github.com/open-cluster-management-io/registration-operator) and installed OCM successfully;
- At least one managed cluster has been imported and accepted;

## Deploy the example AddOns
Set environment variables.
```sh
export KUBECONFIG=</path/to/hub_cluster/kubeconfig>
```

Build the docker image to run the sample AddOn.
```sh
# get imagebuilder first
go get github.com/openshift/imagebuilder/cmd/imagebuilder@v1.2.1
export PATH=$PATH:$(go env GOPATH)/bin
# build image
make images
export EXAMPLE_IMAGE_NAME=<helloworld_addon_image_name> # export EXAMPLE_IMAGE_NAME=quay.io/open-cluster-management/helloworld-addon:latest
```

If your are using kind, load image into kind hub cluster.
```sh
kind load docker-image $EXAMPLE_IMAGE_NAME --name <your-hub-cluster-name> # kind load docker-image  $EXAMPLE_IMAGE_NAME --name cluster1
```

And then deploy the example AddOns controller on hub cluster.
```sh
make deploy-example
```

The helloworld AddOn controller will create one `ManagedClusterAddOn` for each managed cluster automatically to install the helloworld agent on the managed cluster.

After a successful deployment, check on the managed cluster and see the helloworld AddOn agent has been deployed from the hub cluster.
```sh
kubectl --kubeconfig </path/to/managed_cluster/kubeconfig> -n default get pods
NAME                               READY   STATUS    RESTARTS   AGE
helloworld-agent-b99d47f76-v2j6h   1/1     Running   0          53s
```

The helloworld_helm AddOn controller cannot create `ManagedClusterAddOn` automatically.

We can create a `ManagedClusterAddOn` in the managedCluster namespace on the Hub cluster to enable the installation of the AddOn on the managed cluster.
```sh
kubectl apply -f examples/deploy/addon-cr/helloworld_helm_addon_cr.yaml
```

We can check the helloworld_helm AddOn agent is deployed in the `installNamespace` on the managed cluster. 

## Clean up
Undeploy managedClusterAddons firstly.
```sh
make undeploy-addon
```

Undeploy example AddOn controllers from hub cluster after all managedClusterAddons are deleted.
```sh
make undeploy-example
```

Remove the AddOn CR from hub cluster. It will undeploy the AddOn agent from the managed cluster as well.
```sh
kubectl --kubeconfig </path/to/hub_cluster/kubeconfig> delete managedclusteraddons -n <managed_cluster_name> helloworld
```

Follow instructions from [registration-operator](https://github.com/open-cluster-management-io/registration-operator) to uninstall OCM if necessary;

<!--
## XXX References

If you have any further question about xxx, please refer to
[XXX help documentation](docs/xxx_help.md) for further information.
-->
