# Cluster Inventory API conformance

The OCM cluster manager publishes a ClusterProfile for each registered ManagedCluster
that a ManagedClusterSetBinding selects. This module runs the shared
[Cluster Inventory API](https://github.com/kubernetes-sigs/cluster-inventory-api)
observer against those ClusterProfiles in the inventory namespace.

The observer only reads. It does not create or repair ClusterProfiles, so their
creation and readiness are preconditions of the test.

## Observer configuration

| Input | OCM value |
| --- | --- |
| Inventory namespace | Namespace of a bound ManagedClusterSetBinding, for example `conformance-inventory` |
| Cluster manager | `open-cluster-management` |
| Expected count | Number of registered ManagedClusters selected by the namespace's bindings |
| Supported features | `ClusterManagerLabel,InventoryMemberID,InventoryMemberUniqueness,ControlPlaneHealthy,Joined,KubernetesVersion` |
| Implementation mode | `ClusterProfile` feature gate enabled; default CSR registration |

## Implementation behavior

- **API version.** ClusterProfile clients and informers use `multicluster.x-k8s.io/v1alpha2`.
  The CRD from the dependency still serves v1alpha1, which remains the storage version.
- **Inventory member ID.** The coordinated ManagedCluster label, if it is non-empty;
  otherwise the ManagedCluster name.
- **Selection.** Selection through ManagedClusterSetBinding is unchanged.

## Test module

`test/conformance` is a separate Go module, so the observer's test dependencies stay out of
OCM's runtime dependency graph. The shared suite defines the flags, result classification,
report schema, and accepted capabilities.

With no arguments, `make test-cluster-inventory-conformance` observes the existing
dedicated kind hub. It obtains its kubeconfig from kind, selects
`conformance-inventory`, and checks three members with all six supported features.
It does not depend on the current Kubernetes context or change cluster resources.
If the environment is missing, it points to the kind setup target.

The setup and observer entry scripts share observer configuration and execution
through `common.sh`; the setup target prepares the environment first.

## Run on local kind clusters

| Make target | Behavior without arguments |
| --- | --- |
| `test-cluster-inventory-conformance-kind` | Builds local images, creates the environment, and runs the observer |
| `test-cluster-inventory-conformance` | Repeats the observer against the existing environment |
| `clean-cluster-inventory-conformance-kind` | Deletes only the three named kind clusters; keeps reports |

The kind setup target does the following:

1. Builds all five OCM images from this checkout.
2. Runs `local-up.sh` to create three kind clusters, with `ClusterProfile=true` on the hub.
3. Creates the `conformance-inventory` namespace, a ManagedClusterSet `conformance` that
   selects all clusters, and a ManagedClusterSetBinding in that namespace.
4. Waits for managed-cluster registration, the binding, and ClusterProfile status.
5. Runs the observer in `conformance-inventory`. It expects three members:
   the two managed clusters and the hub, registered as `local-cluster`.

The cluster names default to the values below and can be overridden with environment variables.
The setup, observer, and cleanup targets use the same names and honor the same
Docker/Podman selection. The setup target rejects names that already exist.
For image and feature-gate settings, see the
[development environment configuration](../../solutions/setup-dev-environment/README.md#configuration).

| Variable | Default |
| --- | --- |
| `HUB` | `ocm-conformance-hub` |
| `CLUSTER1` | `ocm-conformance-cluster1` |
| `CLUSTER2` | `ocm-conformance-cluster2` |

Cleanup succeeds when the named clusters are already absent. A deletion failure
returns an error.

### Outputs

The clusters are kept after the run, whether it succeeds or fails.

Both the setup and default observer targets write the following to a temporary
directory outside the checkout:

| Output | Description |
| --- | --- |
| `kubeconfig` | Dedicated kubeconfig for the run |
| `reports/` | HTML and YAML reports, in development run mode |

Both targets print these paths on exit. The setup target also prints a short Make
command that repeats the observer checks against the retained inventory.

The setup report records the source revision and dirty state used for the build.
Observer-only reports leave the implementation version and revision unspecified:
the current checkout does not identify the source of an already running image.

## Suite dependency

This module's `go.mod` selects the upstream suite version. Reports record the suite
version and source revision separately from the OCM revision.
