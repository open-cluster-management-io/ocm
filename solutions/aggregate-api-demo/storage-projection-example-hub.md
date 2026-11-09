# Projected aggregate-API storage comparison on `example-hub`

- **Observed:** 2026-11-05
- **Source:** read-only, metadata-only list queries (server-side
  `PartialObjectMetadataList`) against context `example-hub` (Kubernetes v1.33.5)
- **Scope:** the OCM resource kinds served by the
  [aggregate API server POC](../../poc/aggregate-api-server), plus the
  Kubernetes-native kinds the demo deliberately keeps in hub etcd
- **Projection only:** the POC was never deployed to `example-hub` and must not be
  (its installer refuses conflicting CRDs; see the POC deployment spec). The
  "projected" columns below apply the POC's API scope to the observed counts.

> **Privacy:** Only object counts were collected, via metadata-only list
> requests. Secret `data` and `stringData` were never read.

## Summary

If the aggregate API server POC served the OCM APIs on a hub with the object
counts observed on `example-hub`, the OCM custom resources would be stored in
PostgreSQL while Kubernetes-native objects — including the large Secret
population — would remain in hub etcd.

| Resource | Before: etcd | Projected: PostgreSQL | Projected: etcd |
|---|---:|---:|---:|
| Managed clusters and sets | 14 | 14 | 0 |
| Placements, decisions and scores | 51 | 51 | 0 |
| ManifestWorks | 85 | 85 | 0 |
| Add-on resources | 85 | 85 | 0 |
| ManagedClusterInfos | 5 | 5 | 0 |
| ClusterClaims | 23 | 23 | 0 |
| **Selected custom resources** | **263** | **263** | **0** |
| Secrets | 1,247 | 0 | 1,247 |
| ConfigMaps | 1,030 | 0 | 1,030 |
| Leases | 155 | 0 | 155 |
| **Total listed objects** | **2,695** | **263** | **2,432** |

Row groupings: managed clusters and sets = ManagedClusters + ManagedClusterSets
+ ManagedClusterSetBindings; placements, decisions and scores = Placements +
PlacementDecisions + AddOnPlacementScores; add-on resources =
ManagedClusterAddOns + ClusterManagementAddOns + AddOnDeploymentConfigs.

## Per-resource measurements

Every resource counted on `example-hub` for this projection:

| Resource | Count |
|---|---:|
| ClusterClaims | 23 |
| ManagedClusters | 5 |
| ManagedClusterSets | 3 |
| ManagedClusterSetBindings | 6 |
| Placements | 22 |
| PlacementDecisions | 22 |
| AddOnPlacementScores | 7 |
| ManifestWorks | 85 |
| ManifestWorkReplicaSets | 0 |
| ManagedClusterAddOns | 59 |
| ClusterManagementAddOns | 20 |
| AddOnDeploymentConfigs | 6 |
| AddOnTemplates | not installed on this hub |
| ManagedClusterInfos | 5 |
| Secrets | 1,247 |
| ConfigMaps | 1,030 |
| Namespaces | 114 |
| ServiceAccounts | 664 |
| Events | 4,686 |
| Leases | 155 |
| Roles | 394 |
| RoleBindings | 852 |
| ClusterRoles | 904 |
| ClusterRoleBindings | 470 |
| CSRs | 124 |

## Notes and limitations

- The POC serves 18 group/version/resource routes covering 14 resource kinds
  (some kinds serve two API versions); see
  `poc/aggregate-api-server/internal/server/resources.go`. The "18 CRs moved
  to the database" figure refers to these routes, not to 18 distinct kinds.
- `ManifestWorkReplicaSets` returned no objects on this hub (the v1alpha1 API
  is not in active use there), and the `AddOnTemplates` CRD is not installed,
  so neither contributes to the totals.
- These are object counts, not byte sizes, etcd write rates, or watch traffic.
  They do not measure what actually stresses hub etcd on this cluster.
- The POC takeover shadows whole group/versions; objects that existed before a
  takeover would strand in shadowed CRD storage rather than move (see the demo
  README). This table is therefore a scope illustration, not a forecast of
  physical etcd savings from a migration.
- Only a subset of Kubernetes-native kinds is listed in the summary. Namespaces,
  ServiceAccounts, RBAC, Events, CSRs, pods, and deployments also remain in
  hub etcd under the aggregate design; their observed counts are recorded
  above for completeness.
- For the read-only Secret metadata breakdown behind the 1,247-secret count's
  managed-cluster portion, see
  [secret-inventory-example-hub.md](secret-inventory-example-hub.md).
