# Placement

With `Placement`, you can select a set of `ManagedClusters` from the `ManagedClusterSets` bound to the placement namespace.

## Community, discussion, contribution, and support

Check the [CONTRIBUTING Doc](CONTRIBUTING.md) for how to contribute to the repo.

<!--

You can reach the maintainers of this project at:

- [#xxx on Slack](https://slack.com/signin?redir=%2Fmessages%2Fxxx)

-->

------

## Getting Started

### Prerequisites

You have at least one running kubernetes cluster;

### Clone this repo

```sh
git clone https://github.com/open-cluster-management-io/placement.git
cd placement
```

### Deploy the placement controller

Set environment variables.

```sh
export KUBECONFIG=</path/to/kubeconfig>
```

Build the docker image to run the placement controller.

```sh
go install github.com/openshift/imagebuilder/cmd/imagebuilder@v1.2.1
make images
export IMAGE_NAME=<placement_image_name> # export IMAGE_NAME=quay.io/open-cluster-management/placement:latest
```

If your are using kind, load image into the kind cluster.

```sh
kind load docker-image <placement_image_name> # kind load docker-image quay.io/open-cluster-management/placement:latest
```

And then deploy placement manager on the cluster.

```sh
make deploy-hub
```

### What is next

After a successful deployment, check on the cluster and see the placement controller has been deployed.

```sh
kubectl -n open-cluster-management-hub get pods
NAME                                                  READY   STATUS    RESTARTS   AGE
cluster-manager-placement-controller-cf9bbd6c-x9dnd   1/1     Running   0          2m16s
```

Here is an example.

Create a `ManagedClusterSet`.

```sh
cat <<EOF | kubectl apply -f -
apiVersion: cluster.open-cluster-management.io/v1beta2
kind: ManagedClusterSet
metadata:
  name: clusterset1
EOF
```

Create a `ManagedCluster` and assign it to clusterset `clusterset1`.

```sh
cat <<EOF | kubectl apply -f -
apiVersion: cluster.open-cluster-management.io/v1
kind: ManagedCluster
metadata:
  name: cluster1
  labels:
    cluster.open-cluster-management.io/clusterset: clusterset1
    vendor: OpenShift
spec:
  hubAcceptsClient: true
EOF
```

Create a `ManagedClusterSetBinding` to bind the `ManagedClusterSet` to the default namespace.

```sh
cat <<EOF | kubectl apply -f -
apiVersion: cluster.open-cluster-management.io/v1beta2
kind: ManagedClusterSetBinding
metadata:
  name: clusterset1
  namespace: default
spec:
  clusterSet: clusterset1
EOF
```

Now create a `Placement`:

```sh
cat <<EOF | kubectl apply -f -
apiVersion: cluster.open-cluster-management.io/v1beta1
kind: Placement
metadata:
  name: placement1
  namespace: default
spec:
  predicates:
    - requiredClusterSelector:
        labelSelector:
          matchLabels:
            vendor: OpenShift
EOF
```

Check the `PlacementDecision` created for this placement. It contains all selected clusters in status.

```txt
kubectl get placementdecisions
NAME                    AGE
placement1-decision-1   2m27s

kubectl describe placementdecisions placement1-decision-1
......
Status:
  Decisions:
    Cluster Name:  cluster1
    Reason:
Events:            <none>
```

### Clean up

Undeploy placement controller from the cluster.

```sh
make undeploy-hub
```

<!--
## XXX References

If you have any further question about xxx, please refer to
[XXX help documentation](docs/xxx_help.md) for further information.
-->
