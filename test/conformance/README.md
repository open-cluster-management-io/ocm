# Cluster Inventory API conformance

OCM's cluster manager publishes ClusterProfile objects when a bound
ManagedClusterSetBinding selects registered ManagedClusters. This test imports
the shared Cluster Inventory API observer and verifies those implementation-managed
objects in the selected namespace.

| Input | OCM value |
| --- | --- |
| Inventory namespace | The namespace of a bound ManagedClusterSetBinding, for example `conformance-inventory` |
| Cluster manager | `open-cluster-management` |
| Expected count | Number of registered ManagedClusters selected by the namespace's bindings |
| Supported features | `ClusterManagerLabel,InventoryMemberID,InventoryMemberUniqueness,ControlPlaneHealthy,Joined,KubernetesVersion` |
| Implementation mode | ClusterProfile feature gate enabled; normal CSR registration |

ClusterProfile clients and informers use `multicluster.x-k8s.io/v1alpha2`.
The dependency's CRD continues to serve v1alpha1, which remains its storage version.
Inventory member IDs use the coordinated ManagedCluster label when non-empty and
otherwise the ManagedCluster name. Selection through ClusterSetBinding is unchanged.

The test is a separate Go module so the observer's test dependencies do not enter
OCM's runtime dependency graph. `CONFORMANCE_ARGS` configures the common observer
through the `test-cluster-inventory-conformance` target. The common suite owns the
flags, result classification, report schema, and accepted capabilities.

Local workspace replacements must identify the actual suite revision separately
from the OCM revision. Implementation-managed resource creation and readiness are
preconditions; the observer does not create or repair ClusterProfiles.
