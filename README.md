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

### Deploy the placement controller
Set environment variables.
```sh
export KUBECONFIG=</path/to/kubeconfig>
```

Build the docker image to run the placement controller.
```sh
make images
export IMAGE_NAME=<placement_image_name> # export IMAGE_NAME=quay.io/open-cluster-management/placement:latest
```

If your are using kind, load image into kind cluster.
```sh
kind load docker-image <placement_image_name> # kind load docker-image quay.io/open-cluster-management/placement:latest
```

And then deploy placement manager on cluster
```
make deploy-hub
```

### What is next
After a successful deployment, check on the cluster and see the placement controller has been deployed.
```
kubectl -n open-cluster-management-hub get pods
NAME                                                  READY   STATUS    RESTARTS   AGE
cluster-manager-placement-controller-cf9bbd6c-x9dnd   1/1     Running   0          2m16s
```

Create a clusterset.yaml as shown in this example:

```yaml
apiVersion: cluster.open-cluster-management.io/v1alpha1
kind: ManagedClusterSet
metadata:
  name: clusterset1
```

Apply the yaml file to the cluster to create a `ManagedClusterSet`.
```
kubectl apply -f clusterset.yaml
```

Create a cluster.yaml:

```yaml
apiVersion: cluster.open-cluster-management.io/v1
kind: ManagedCluster
metadata:
  name: cluster1
  labels:
    cluster.open-cluster-management.io/clusterset: clusterset1
    vendor: OpenShift
spec:
  hubAcceptsClient: true
```

Apply the yaml file to create a cluster and assign it to clusterset `clusterset1`.
```
kubectl apply -f cluster.yaml
```

And then create a binding.yaml:

```yaml
apiVersion: cluster.open-cluster-management.io/v1alpha1
kind: ManagedClusterSetBinding
metadata:
  name: clusterset1
  namespace: default
spec:
  clusterSet: clusterset1
```

Apply the yaml file to bind the `ManagedClusterSet` to the default namespace.
```
kubectl apply -f binding.yaml
```

Now create a placement.yaml:
```yaml
apiVersion: cluster.open-cluster-management.io/v1alpha1
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
```
Apply the yaml file to create the placement.

```
kubectl apply -f placement.yaml
```

Check the 'PlacementDecision'created for this placement. It contains all selected clusters in status.
```
kubectl get placementdecisions
NAME                    AGE
placement1-decision-1   14s
```

### Clean up
Undeploy placement controller from the cluster.
```
make undeploy-hub
```

<!--
## XXX References

If you have any further question about xxx, please refer to
[XXX help documentation](docs/xxx_help.md) for further information.
-->