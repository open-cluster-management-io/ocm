# OCM demo on the SQL-backed aggregate API server

This demo runs the standard three-cluster OCM developer flow from
[solutions/setup-dev-environment](../setup-dev-environment) with one change:
after `clusteradm init` installs the OCM hub CRDs and controllers, the
[aggregate API server POC](../../poc/aggregate-api-server) takes over the
CRD-backed APIServices so the OCM group/versions are served from PostgreSQL
instead of etcd.

The goal is a side-by-side picture of what moves to the database and what
stays in Kubernetes:

| Moved to PostgreSQL (via aggregated APIServices) | Stays in hub etcd (Kubernetes-native) |
|---|---|
| ManagedCluster, ManagedClusterSet/Binding, Placement/PlacementDecision, AddOnPlacementScore, ManifestWork, ManifestWorkReplicaSet, ManagedClusterAddOn, ClusterManagementAddOn, AddOnDeploymentConfig, AddOnTemplate, ManagedClusterInfo | Namespaces, ServiceAccounts, RBAC, Secrets, ConfigMaps, Leases (heartbeats), CSRs, Events, deployments/pods, the (shadowed) CRDs themselves |

## How the takeover works

1. `clusteradm init` installs the hub CRDs. Each CRD group/version gets a
   *local* APIService object (named `{version}.{group}`, no `spec.service`)
   managed by the kube-apiserver's autoregistration.
2. The POC installer (`OCM_API_POC_TAKEOVER=yes`) patches each of those local
   APIServices: it points `spec.service` at the aggregate server, sets the
   serving cert CA bundle, and removes the `kube-aggregator.kubernetes.io/automanaged`
   label. Without that label the autoregistration controller ignores the
   object, so the takeover persists.
3. From then on the aggregator proxies every request for those group/versions
   to the POC server, which reads and writes PostgreSQL. The CRDs stay
   installed but are shadowed: the CRD handler never serves those paths, even
   while the aggregate server is unavailable.
4. Hub controllers are rollout-restarted so their informers LIST/WATCH
   against PostgreSQL instead of keeping pre-takeover etcd watches.
5. `deploy/uninstall.sh` reverses the takeover: it restores the automanaged
   label and clears `spec.service`, returning each group/version to local CRD
   serving.

Objects created between `clusteradm init` and the takeover (for example the
default `global` ManagedClusterSet) are stranded in the shadowed CRD storage
and are re-created by the controllers in PostgreSQL. The count script reports
both so the residue is visible.

## Prerequisites

- kind, kubectl, openssl
- [clusteradm](https://github.com/open-cluster-management-io/clusteradm/releases)
- docker or podman
- The host needs enough inotify instances for three kind clusters plus the
  OCM controllers (they watch certificate files). If pods crash-loop with
  "too many open files" or kind cannot create the third cluster, raise the
  limit (this is the same known issue documented by
  [setup-dev-environment](../setup-dev-environment/README.md)):

  ```bash
  sudo sysctl fs.inotify.max_user_instances=8192 fs.inotify.max_user_watches=524288
  ```

## Run the demo

```bash
cd solutions/aggregate-api-demo
./local-up-aggregate.sh
```

Cluster names default to `agg-hub`, `agg-cluster1`, `agg-cluster2`
(override with `HUB`, `CLUSTER1`, `CLUSTER2`). The script:

1. creates the three kind clusters,
2. runs `clusteradm init` on the hub,
3. builds and deploys the POC in takeover mode,
4. restarts the hub controllers,
5. joins and accepts `agg-cluster1`, `agg-cluster2`, and `local-cluster`,
6. waits for all clusters to be Joined and Available,
7. creates a ManifestWork on the hub and verifies the spoke applies it,
8. prints object counts.

Expected final state:

```text
NAME            HUB ACCEPTED   MANAGED CLUSTER URLS   JOINED   AVAILABLE
agg-cluster1    true           ...                    True     True
agg-cluster2    true           ...                    True     True
local-cluster   true           ...                    True     True
```

## Comparison counts

```bash
./count-resources.sh kind-agg-hub          # aggregate hub
```

Run the same script against a baseline hub created with
[solutions/setup-dev-environment/local-up.sh](../setup-dev-environment/local-up.sh)
(`./count-resources.sh kind-hub`) to compare. The script reports:

1. PostgreSQL object counts by resource (aggregate hub only),
2. Kubernetes API object counts (OCM resources plus Kubernetes-native
   resources, including a per-namespace breakdown for the managed-cluster
   namespaces),
3. raw etcd key counts by resource prefix and etcd database size — the
   definitive measure of what occupies etcd in each mode.

Counts are object metadata only; Secret values are never read.

For the read-only Secret metadata breakdown on the inspected hub, see
[secret-inventory-example-hub.md](secret-inventory-example-hub.md). It counts
names, types, and owner references only; no Secret payloads were accessed.

For the projected before/after storage comparison built from the inspected
hub's object counts, see
[storage-projection-example-hub.md](storage-projection-example-hub.md).
It applies the POC's aggregated API scope to the observed counts; the POC
itself was never deployed to that hub.

## Teardown

```bash
./teardown.sh
```

This restores the CRD-backed APIServices, removes the POC, and deletes the
three kind clusters. All demo data, including PostgreSQL, is destroyed.

## Proof-of-technology output (recorded live run, Nov 2026)

The demo was run end to end on this machine (podman + kind v1.36, clusteradm
v1.4.0) with two clusters (`agg-hub` + `agg-cluster1` + `local-cluster`;
`agg-cluster2` was dropped because the host's inotify instance limit could
not sustain a third cluster, see Prerequisites). Final state:

```text
agg-cluster1   HubAccepted=True  Joined=True  Available=True  ClockSynced=True
local-cluster  HubAccepted=True  Joined=True  Available=True  ClockSynced=True
```

and a ManifestWork created on the hub was stored in PostgreSQL, watched by
the spoke work agent through the aggregate server, and applied on the spoke
(`served-by: postgresql-backed-aggregate-api` ConfigMap).

Resulting counts on the aggregate hub:

| Where | What | Count |
|---|---|---|
| PostgreSQL | ManagedClusters / ManagedClusterSets / ManifestWorks | 2 / 2 / 1 (+ 28 watch-journal rows) |
| etcd | OCM custom-resource keys | 4 (2 pre-takeover ManagedClusterSets, 1 ClusterManager, 1 Klusterlet operator CR) |
| etcd | Kubernetes-native keys | ~760 (14 secrets, 22 configmaps, 13 leases incl. 2 heartbeats, 3 CSRs, 234 events, RBAC, ...) |
| etcd | POC infrastructure | 9 taken-over APIServices, 16 shadowed CRDs, 2 pods |

On a baseline hub the same flow keeps all OCM objects in etcd; run
`./count-resources.sh kind-hub` against a
[local-up.sh](../setup-dev-environment/local-up.sh) hub to compare.

Fixes the live run forced in the aggregate server (all in
`poc/aggregate-api-server`):

- cluster-wide (all-namespace) ManifestWork LIST/WATCH,
- the watch-list protocol (`sendInitialEvents` plus the
  `k8s.io/initial-events-end` bookmark) that client-go 1.36 reflectors and
  `kubectl wait` require,
- authorizing `fieldSelector=metadata.name=X` lists as gets of `X`, matching
  kube-apiserver semantics — without it the resourceNames-scoped RBAC that
  OCM grants spoke agents rejects their informers,
- serving `clusterclaims` (the hub-as-spoke klusterlet installs that CRD in
  `cluster.open-cluster-management.io/v1alpha1`, which the takeover masks).

The demo clusters from the recorded run (`agg-hub`, `agg-cluster1`) were left
on the test machine for inspection; `./teardown.sh` removes them and tolerates
the absent third cluster.

## Known limitations

- The POC does not implement CRD schemas, defaulting, CEL, admission
  webhooks, OpenAPI, or API version conversion; OCM admission webhooks are
  bypassed for requests proxied to the aggregate server. Controllers with
  fallbacks for missing defaults (for example lease duration) keep working.
- Serving both AddOn API versions does not convert between them.
- `kubectl get <ocm-resource>` prints names only (no server-side table
  formatting), and create/patch commands need `--validate=false` because the
  server publishes no OpenAPI schema.
- `clusteradm accept --wait` can hang when re-accepting a cluster that is
  already accepted but whose CSR is still pending (approve the CSR manually
  with `kubectl certificate approve`); the fresh-join path works normally.
- The Kubernetes-native flows that the demo deliberately keeps in etcd — CSR
  registration/approval, Lease heartbeats, bootstrap and import Secrets,
  ConfigMaps — still write to hub etcd. Moving them requires controller and
  agent changes tracked outside this demo.
- This is a disposable-cluster demo, not a migration path: the takeover
  shadows whole group/versions and strands pre-takeover CR objects in etcd.
