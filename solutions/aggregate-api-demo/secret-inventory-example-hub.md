# Secret inventory on `example-hub`

- **Observed:** 2026-11-04
- **Source:** read-only Kubernetes metadata queries against context `example-hub`
- **Scope:** managed-cluster namespaces `example-mc01`, `example-virt01`, `example-virt02`,
  `example-virt03`; `local-cluster` is tallied separately.

> **Privacy:** Only Secret names, types, owner-reference kinds/names, and a
> management-label field were queried. Secret `data` and `stringData` were not
> read or decoded. Cluster, namespace, and object names are anonymized.

## Summary

The four provisioned managed-cluster namespaces contain **64 Secrets** total.
Of those, **28 have owner references to OCM or cluster-provisioning
resources**, **20 are owned by ServiceAccounts** (service-account pull
credentials), and **16 have no owner reference** and appear to be
cluster-provisioning inputs based on their names.

| Namespace | OCM/provisioning owner reference | ServiceAccount owner | No owner reference; likely provisioning input | Total |
|---|---:|---:|---:|---:|
| `example-mc01` | 6 | 5 | 4 | 15 |
| `example-virt01` | 7 | 5 | 4 | 16 |
| `example-virt02` | 8 | 5 | 4 | 17 |
| `example-virt03` | 7 | 5 | 4 | 16 |
| **Total** | **28** | **20** | **16** | **64** |

So **28/64 (43.75%) are directly attributable by owner reference** to OCM or
cluster-provisioning resources. A further 16 are plausibly
provisioning-related from their names,
but metadata alone does not establish which component created or currently
manages them. ServiceAccount ownership means Kubernetes generated the
service-account token/pull Secret; it does not by itself establish which
component owns the ServiceAccount.

## Per-namespace detail

### `example-mc01` — 15 Secrets

**Owner references (6):** `application-manager` and
`klusterlet-addon-workmgr-log` (`ManagedServiceAccount`); `example-mc01-import`
(`ManagedCluster`); `example-mc01-merged-pull-secret` (`ClusterDeployment`); and
`example-mc01-0-xxxxx-admin-kubeconfig` plus
`example-mc01-0-xxxxx-admin-password` (`ClusterProvision` and `ClusterDeployment`).

**ServiceAccount-owned (5):** `builder-dockercfg-xxxxx`,
`cluster-installer-dockercfg-xxxxx`, `default-dockercfg-xxxxx`,
`deployer-dockercfg-xxxxx`, and `example-mc01-bootstrap-sa-dockercfg-xxxxx`.

**No owner reference (4):** `example-mc01-aws-creds`, `example-mc01-install-config`,
`example-mc01-pull-secret`, and `example-mc01-ssh-private-key`.

### `example-virt01` — 16 Secrets

**Owner references (7):** `application-manager` and
`klusterlet-addon-workmgr-log` (`ManagedServiceAccount`);
`example-virt01-import` (`ManagedCluster`);
`example-virt01-merged-pull-secret` (`ClusterDeployment`);
`example-virt01-workload` (`ManagedServiceAccount`); and the
`example-virt01-0-xxxxx-admin-kubeconfig` and
`example-virt01-0-xxxxx-admin-password` Secrets (`ClusterProvision` and
`ClusterDeployment`).

**ServiceAccount-owned (5):** `builder-dockercfg-xxxxx`,
`cluster-installer-dockercfg-xxxxx`, `default-dockercfg-xxxxx`,
`deployer-dockercfg-xxxxx`, and `example-virt01-bootstrap-sa-dockercfg-xxxxx`.

**No owner reference (4):** `example-virt01-azure-creds`,
`example-virt01-install-config`, `example-virt01-pull-secret`, and
`example-virt01-ssh-private-key`.

### `example-virt02` — 17 Secrets

**Owner references (8):** the same common `application-manager`, import,
merged-pull-secret, admin kubeconfig/password, bootstrap dockercfg, and
work-manager log categories as `example-virt01`, plus `cluster-recommendation`
(`ManagedServiceAccount`) and `example-virt02-workload` (`ManagedServiceAccount`).

**ServiceAccount-owned (5):** builder, cluster-installer, default, deployer,
and `example-virt02-bootstrap-sa-dockercfg-xxxxx` dockercfg Secrets.

**No owner reference (4):** `example-virt02-azure-creds`,
`example-virt02-install-config`, `example-virt02-pull-secret`, and
`example-virt02-ssh-private-key`.

### `example-virt03` — 16 Secrets

**Owner references (7):** the same categories as `example-virt01`, including its
`ManagedServiceAccount`-owned `example-virt03-workload` Secret and its
`ClusterDeployment`/`ClusterProvision` admin credentials.

**ServiceAccount-owned (5):** builder, cluster-installer, default, deployer,
and `example-virt03-bootstrap-sa-dockercfg-xxxxx` dockercfg Secrets.

**No owner reference (4):** `example-virt03-azure-creds`,
`example-virt03-install-config`, `example-virt03-pull-secret`, and
`example-virt03-ssh-private-key`.

## `local-cluster` — separate count

The hub's self-managed namespace contains **8 Secrets**, not included in the
64-secret total above:

- **3 with OCM-related owner references:** `application-manager` and
  `klusterlet-addon-workmgr-log` (`ManagedServiceAccount`), plus
  `local-cluster-import` (`ManagedCluster`).
- **4 ServiceAccount-owned:** builder, default, deployer, and
  `local-cluster-bootstrap-sa` dockercfg Secrets.
- **1 without an owner reference:** `platform-operator-azure-credentials`.
  Its name suggests platform-operator use, but that is not proven by the
  inspected metadata.

## Interpretation for the aggregate API comparison

- The 28 owner-referenced Secrets are associated with OCM registration,
  add-on service accounts, or cluster provisioning. Their presence alone
  does not show they can be removed from etcd: current components may require
  them as Kubernetes Secret objects.
- The 16 unowned provisioning-looking Secrets may be cluster-provisioning
  inputs or operator-created credentials. An absent owner reference is **not** proof
  that a Secret is unmanaged; creator audit events or controller source would
  be needed for attribution.
- The 20 dockercfg Secrets are ServiceAccount-owned platform objects. Their
  ServiceAccounts may belong to OCM-related workloads, but they remain ordinary
  Kubernetes resources used by the cluster.
- These are object counts, not byte sizes, change rates, etcd write counts, or
  a forecast of what an aggregate API migration would remove. Do not infer
  savings by deleting or relocating credentials based on this inventory.

This report contains metadata only and intentionally makes no claim about the
contents, sensitivity, validity, or current use of any Secret.
