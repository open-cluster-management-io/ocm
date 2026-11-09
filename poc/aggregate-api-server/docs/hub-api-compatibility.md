# OCM hub API inventory and compatibility notes

**Status:** Initial source review  
**Repository revision reviewed:** `76282e9e86e65ffd4c631af0c5eaeb11c12477fd`  
**Scope:** Hub CRDs installed by this repository's cluster-manager, including its feature-gated hub CRDs. Spoke-only and operator configuration CRDs are out of scope.

## Summary

The cluster-manager installs **12 default hub CRDs** and can install **2 additional feature-gated CRDs**. All listed versions are served; each CRD has one storage version. Two AddOn CRDs use conversion webhooks. Three resources are targeted by admission webhook configurations: `ManagedCluster` (mutating and validating), `ManagedClusterSetBinding` (validating), and `ManifestWork` (validating).

OCM controllers use generated typed clients and shared informers. Several maintain long-running list/watch caches, use label selectors and indexes, and update status or finalizers. These behaviors, along with Kubernetes metadata, schema handling, admission, and authorization, are part of compatibility—not just CRUD persistence.

The source does not establish which resource or workload contributes the most etcd load. `ManifestWork` is a useful provisional POC candidate because hub and spoke components exchange it, the spoke agent watches it in its cluster namespace, and it carries embedded Kubernetes manifests and status. Validate that choice against measured hub traffic before treating it as representative of the largest load.

## Inventory

The installation baseline is `hubCRDResourceFiles` in [`clustermanager_crd_reconcile.go`](../../../pkg/operator/operators/clustermanager/controllers/clustermanagercontroller/clustermanager_crd_reconcile.go). `ClusterProfile` and the SIG Multicluster `PlacementDecision` are appended only when their respective feature gates are enabled. The manifests live under [`manifests/cluster-manager/hub/crds`](../../../manifests/cluster-manager/hub/crds).

`S` marks a storage version; all versions shown are served. “Schema features” highlights notable schema behavior found in the CRD source, not an exhaustive schema listing. Unless noted, conversion strategy is `None` and no resource-specific admission webhook was found in the hub webhook configurations reviewed.

| Installation | Resource | Scope | Served versions (storage marked `S`) | Notable API behavior |
|---|---|---|---|---|
| Default | `ClusterManagementAddOn` (`addon.open-cluster-management.io`) | Cluster | `v1alpha1 (S)`, `v1beta1` | `/status`; conversion webhook; defaults |
| Default | `ManagedCluster` (`cluster.open-cluster-management.io`) | Cluster | `v1 (S)` | `/status`; mutating and validating admission on CREATE/UPDATE; defaults; authorization-sensitive registration operations |
| Default | `ManagedClusterSet` (`cluster.open-cluster-management.io`) | Cluster | `v1beta2 (S)` | `/status`; defaults |
| Default | `ManagedClusterSetBinding` (`cluster.open-cluster-management.io`) | Namespaced | `v1beta2 (S)` | `/status`; validating admission on CREATE/UPDATE |
| Default | `Placement` (`cluster.open-cluster-management.io`) | Namespaced | `v1beta1 (S)` | `/status`; extensive defaults and list semantics |
| Default | `PlacementDecision` (`cluster.open-cluster-management.io`) | Namespaced | `v1beta1 (S)` | `/status` |
| Default | `AddOnPlacementScore` (`cluster.open-cluster-management.io`) | Namespaced | `v1alpha1 (S)` | `/status`; map-list semantics |
| Default | `ManagedClusterAddOn` (`addon.open-cluster-management.io`) | Namespaced | `v1alpha1 (S)`, `v1beta1` | `/status`; conversion webhook; defaults |
| Default | `AddOnDeploymentConfig` (`addon.open-cluster-management.io`) | Namespaced | `v1alpha1 (S)`, `v1beta1` | Defaults; no conversion webhook configured in the CRD |
| Default | `AddOnTemplate` (`addon.open-cluster-management.io`) | Cluster | `v1alpha1 (S)`, `v1beta1` | CEL validation, defaults, embedded/preserved-unknown schema regions |
| Default | `ManifestWork` (`work.open-cluster-management.io`) | Namespaced | `v1 (S)` | `/status`; validating admission on CREATE/UPDATE; CEL validation, defaults, embedded resources and preserved-unknown manifest payloads |
| Default | `ManifestWorkReplicaSet` (`work.open-cluster-management.io`) | Namespaced | `v1alpha1 (S)` | `/status`; CEL validation, defaults, embedded resources and preserved-unknown manifest payloads |
| Feature-gated: `ClusterProfile` | `ClusterProfile` (`multicluster.x-k8s.io`) | Namespaced | `v1alpha1 (S)`, `v1alpha2` | `/status`; CEL validation and preserved-unknown schema regions |
| Feature-gated: `SIGPlacementDecision` | `PlacementDecision` (`multicluster.x-k8s.io`) | Namespaced | `v1alpha1 (S)`, `v1alpha2` | CEL validation in `v1alpha2`; no status subresource declared |

The two AddOn conversion webhooks are configured in the `ClusterManagementAddOn` and `ManagedClusterAddOn` CRDs and implemented under [`pkg/addon/webhook`](../../../pkg/addon/webhook). The CRD manifests also declare `conversionReviewVersions`; conversion must be available whenever those served versions are used.

### Admission and authorization hooks

- [`webhook-mutatingconfiguration.yaml`](../../../manifests/cluster-manager/hub/registration/webhook-mutatingconfiguration.yaml) and [`webhook-validatingconfiguration.yaml`](../../../manifests/cluster-manager/hub/registration/webhook-validatingconfiguration.yaml) target `ManagedCluster` CREATE/UPDATE for all versions and use `failurePolicy: Fail`.
- [`webhook-clustersetbinding-validatingconfiguration.yaml`](../../../manifests/cluster-manager/hub/registration/webhook-clustersetbinding-validatingconfiguration.yaml) validates `ManagedClusterSetBinding` `v1beta2` CREATE/UPDATE and uses `failurePolicy: Fail`.
- [`work/webhook-validatingconfiguration.yaml`](../../../manifests/cluster-manager/hub/work/webhook-validatingconfiguration.yaml) validates `ManifestWork` CREATE/UPDATE for all versions and uses `failurePolicy: Fail`.
- Registration validation performs authorization checks for operations such as `managedclusters/accept` and `managedclustersets/join`; ManagedClusterSetBinding validation checks `managedclustersets/bind`. These are RBAC authorization actions and should not be mistaken for CRD-declared REST subresources. See [`managedcluster_validating.go`](../../../pkg/registration/webhook/v1/managedcluster_validating.go) and [`managedclustersetbinding_validating.go`](../../../pkg/registration/webhook/v1beta2/managedclustersetbinding_validating.go).

## Consumer flows observed in source

These are representative call paths, not a complete inventory of every client operation.

| Component | Observed use | Compatibility implications |
|---|---|---|
| Registration hub | [`pkg/registration/hub/manager.go`](../../../pkg/registration/hub/manager.go) builds shared informer factories for ManagedClusters, ClusterProfiles, ManifestWorks, AddOns, and core Kubernetes objects. It wires ManagedCluster, ManagedClusterSet, namespace, lease, and related controllers. | Initial LIST plus long-running WATCH, cache freshness, selector behavior, and resource-version/conflict handling matter. ClusterProfile access is feature-gated. |
| Placement | [`pkg/placement/controllers/manager.go`](../../../pkg/placement/controllers/manager.go) watches ManagedClusters, ManagedClusterSets, bindings, Placements, cluster PlacementDecisions, and AddOnPlacementScores; it optionally watches SIG Multicluster PlacementDecisions. | Scheduling depends on coherent cross-resource reads and timely event delivery. Feature-gated API traffic must be included only when that gate is enabled. |
| Add-on manager | [`pkg/addon/manager.go`](../../../pkg/addon/manager.go) creates typed and dynamic informer factories; it label-filters ManifestWorks and builds indexes for AddOn and ManifestWork lookups. | Label selectors, watch filtering, index inputs, arbitrary/dynamic Kubernetes resources, and list/watch scale need coverage. |
| Work hub | [`pkg/work/hub/manager.go`](../../../pkg/work/hub/manager.go) watches ManifestWorks, ManifestWorkReplicaSets, Placements, and PlacementDecisions. ManifestWork can use either the Kubernetes driver or a CloudEvents-backed client. | The storage/API path differs by work driver; establish which driver and API path are in the POC environment. Replica-set reconciliation consumes both work and placement events. |
| Work spoke agent | [`pkg/work/spoke/spokeagent.go`](../../../pkg/work/spoke/spokeagent.go) creates a hub ManifestWork client and informer scoped by the managed-cluster namespace, and runs work, finalizer, and status controllers. | Namespaced LIST/WATCH, status updates, finalizers/deletion, watch reconnects, and reliable change notifications are key behaviors. |

Controllers also issue direct writes. For example, registration patches ManagedCluster state, and work garbage collection deletes ManifestWorks. The compatibility test plan should trace the exact read/write verbs and patch types for the selected resource rather than infer them from informer wiring alone.

## Compatibility matrix for the POC

| Requirement | POC disposition | Verification scenario |
|---|---|---|
| Discovery and API shape for the selected group/version/resource | **POC-required** | Existing generated client and `kubectl` discover and address the selected resource through the intended route. |
| CRUD, LIST, namespace/cluster scope, selectors, pagination as used by consumers | **POC-required** | Exercise the operations and selectors found in the consumer trace; compare returned metadata and objects. |
| WATCH, reconnect, event ordering, and `resourceVersion` behavior | **POC-required** | Start informer/list-watch clients, create/update/delete objects, reconnect, and verify no lost or reordered observable state beyond documented Kubernetes semantics. |
| Optimistic concurrency and conflict behavior | **POC-required** | Race updates using the same observed resource version; stale writes must fail predictably and clients must be able to retry. |
| Status subresource, finalizers, deletion timestamp, and owner-reference behavior used by the selected resource | **POC-required** | Verify status/spec isolation, finalizer-driven deletion, and observable lifecycle transitions. |
| Structural schema pruning, defaults, CEL, embedded resources, and preserved-unknown fields used by the selected resource | **POC-required** | Submit valid/invalid objects and unknown/embedded fields; verify behavior against the CRD contract. |
| Conversion and admission webhooks | **POC-required when exercised by the chosen resource/version; otherwise deferred** | Exercise conversion or admission for applicable versions and verify fail-closed behavior and error reporting. |
| Authentication, delegated authorization, RBAC actions, and audit identity | **POC-required** | Test an allowed and denied principal, including any relevant status or virtual authorization action; ensure database access cannot bypass API authorization. |
| Persistence across server restart and database failure behavior | **POC-required** | Restart the POC and verify durable reads; induce unavailable storage and record API/watch failure behavior. |
| Support for all 14 CRDs, production migration, multi-replica HA, and full release operations | **Deferred** | Document as explicit non-goals; address only in follow-up work after the POC recommendation. |
| Relative etcd-load reduction under representative traffic | **Unresolved until measured** | Establish a repeatable baseline and workload; collect comparable hub etcd metrics and report measurement limits. |

The matrix is scoped to one representative resource. It does not imply that implementation of an aggregated server can satisfy these requirements for all OCM resources without additional work.

## Architectural constraints and risks

1. **APIService routing is group/version-scoped.** An aggregated API registers a group/version, not one resource within an existing CRD group/version. The POC must choose a conflict-free test group/version or explicitly define how the whole affected group/version is served. Do not assume a single CRD can be transparently moved behind an APIService while sibling CRDs remain on the CRD handler.
2. **CRD reconciliation is active.** Cluster-manager applies the hub CRDs from `hubCRDResourceFiles` and conditionally adds feature-gated definitions in [`clustermanager_crd_reconcile.go`](../../../pkg/operator/operators/clustermanager/controllers/clustermanagercontroller/clustermanager_crd_reconcile.go). A production ownership change would need coordinated changes to install, reconcile, upgrade, and cleanup behavior—not only an APIService object.
3. **Admission does not transfer automatically.** A separate aggregated server does not automatically execute the CRD schema/CEL or the current admission webhook chain. The POC must identify which validation/defaulting/conversion layer owns each behavior and test it.
4. **Aggregation security is part of the API contract.** The aggregated server needs correctly configured front-proxy authentication and delegated authorization, or an equivalently reviewed design. Preserve Kubernetes RBAC decisions, audit identity, TLS trust, and least-privilege database credentials.
5. **Watch and metadata semantics are substantial.** Controllers rely on informer caches and lifecycle behavior. A SQL table that stores JSON objects but lacks durable resource versions, watch history/reconnect semantics, optimistic concurrency, and Kubernetes deletion behavior is not a compatible API store.
6. **The current source has no measured etcd attribution.** It shows consumers, not request volume or storage pressure by resource. Resource selection and the claimed load reduction require measurement in the POC environment.

## Provisional POC resource recommendation

Use **`ManifestWork`** as the initial candidate, subject to the consumer and load checks above. It has an explicit hub-to-spoke lifecycle, namespaced watch consumption by the spoke agent, hub reconciliation, status/finalizer flows, and embedded arbitrary Kubernetes manifests with CEL/schema behavior. This exercises more compatibility dimensions than a simple scalar CRD. It is not evidence that ManifestWork is the largest source of etcd load.

Compare against:

- **`ManagedCluster`**: cluster-scoped registration lifecycle, status, mutating/validating admission, and authorization-sensitive registration actions.
- **Cluster `PlacementDecision`**: scheduler-produced decisions and placement-controller consumption; simpler object shape but a useful event-freshness case.

If baseline metrics show a different resource dominates storage or write volume, revise the candidate before building the POC.

## Open questions for the follow-on evaluation

- Is the objective to replace Kubernetes CRD storage for selected resources, place SQL beneath an etcd-compatible API server, or create a separate APIService with a distinct API group? These options have materially different compatibility and rollout paths.
- Which hub resource and workload account for enough etcd activity to demonstrate a meaningful reduction?
- Which semantics must be covered by the SQL layer versus delegated to Kubernetes API machinery, admission webhooks, or an etcd-compatible adapter?
- What durability, backup/restore, HA, consistency, and recovery requirements are expected for the target hub deployment?
- How should feature-gated and externally owned APIs be treated if the POC expands beyond this repository's default CRDs?

## Source index

- Hub CRD installation and feature gates: [`clustermanager_crd_reconcile.go`](../../../pkg/operator/operators/clustermanager/controllers/clustermanagercontroller/clustermanager_crd_reconcile.go)
- Hub CRD definitions: [`manifests/cluster-manager/hub/crds`](../../../manifests/cluster-manager/hub/crds)
- ManagedCluster admission configuration: [`registration/webhook-mutatingconfiguration.yaml`](../../../manifests/cluster-manager/hub/registration/webhook-mutatingconfiguration.yaml), [`registration/webhook-validatingconfiguration.yaml`](../../../manifests/cluster-manager/hub/registration/webhook-validatingconfiguration.yaml)
- ManagedClusterSetBinding admission: [`registration/webhook-clustersetbinding-validatingconfiguration.yaml`](../../../manifests/cluster-manager/hub/registration/webhook-clustersetbinding-validatingconfiguration.yaml)
- ManifestWork admission: [`work/webhook-validatingconfiguration.yaml`](../../../manifests/cluster-manager/hub/work/webhook-validatingconfiguration.yaml)
- AddOn conversion implementations: [`pkg/addon/webhook`](../../../pkg/addon/webhook)
- Registration consumers: [`pkg/registration/hub/manager.go`](../../../pkg/registration/hub/manager.go)
- Placement consumers: [`pkg/placement/controllers/manager.go`](../../../pkg/placement/controllers/manager.go)
- AddOn consumers: [`pkg/addon/manager.go`](../../../pkg/addon/manager.go)
- Work hub consumers: [`pkg/work/hub/manager.go`](../../../pkg/work/hub/manager.go)
- Work spoke consumers: [`pkg/work/spoke/spokeagent.go`](../../../pkg/work/spoke/spokeagent.go)
