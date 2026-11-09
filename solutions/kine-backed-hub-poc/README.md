# Hosted OCM hub with Kine/PostgreSQL

This disposable demo keeps the hosting kind cluster's native API and etcd
separate from the OCM hub API. OCM hub controllers run as Hosted-mode
workloads in the hosting cluster, while a distinct kube-apiserver stores OCM
hub state through Kine in PostgreSQL. Two ordinary kind spokes connect to the
standalone OCM API.

```text
                  hosting kind (native API → etcd)
          ┌────────────────────────────────────────────┐
          │ PostgreSQL ← Kine ← standalone kube-apiserver│
          │                         ↑                  │
          │ ClusterManager Hosted controllers/webhooks │
          └─────────────────────────┬──────────────────┘
                                    │ OCM API :7443
                          ┌─────────┴─────────┐
                       kind spoke 1        kind spoke 2
                       native API          native API
                         → etcd              → etcd
```

Only the standalone OCM API uses Kine/PostgreSQL. The hosting cluster's
Deployments, Pods, Services, Secrets, the ClusterManager operator and its
ClusterManager resource remain in the native etcd-backed API. Hosted mode
routes OCM CRDs, ManagedClusters, registration CSRs, hub RBAC, Leases,
ManifestWorks, placements and add-on resources to the external hub kubeconfig.
Spoke-local resources remain in each spoke's native API.

PostgreSQL, Kine and the standalone kube-apiserver run as workloads on the
hosting cluster. Kine is a storage adapter, not the OCM API endpoint. The
standalone API server listens on port `7443` inside the hosting kind
control-plane node. Spokes and in-cluster
controllers use the node IP at that port. Kind forwards a separate localhost
port (`17443` by default) so host-side tools can reach the same API without a
container-network route. The API server uses RBAC, a dedicated client CA,
ServiceAccount signing keys and admission plugins. A separate
kube-controller-manager handles the minimal
namespace, ServiceAccount, garbage-collection and bootstrap-token controllers
needed by the logical OCM API. It does not manage the hosting cluster.

## Run

Prerequisites: `kind`, `kubectl`, `helm`, `clusteradm`, `openssl`, `python3`,
and Docker or Podman. The script chooses Docker when available, otherwise
Podman; set `CONTAINER_ENGINE=podman` to choose explicitly. On Linux, the host
may need `fs.inotify.max_user_instances=8192`; the script checks for a low
inotify limit and low per-user kernel keyring quotas before creating resources.
For rootless Podman environments with the default 200-key quota, raise the
limits before running:

```bash
sudo sysctl -w kernel.keys.maxkeys=10000 kernel.keys.maxbytes=1000000
```

The three kind clusters also commonly require:

```bash
sudo sysctl -w fs.inotify.max_user_instances=8192
```

```bash
cd solutions/kine-backed-hub-poc
./local-up.sh
```

Defaults are `sql-hub`, `sql-spoke1`, and `sql-spoke2`. The setup refuses to
reuse those kind names or a non-empty runtime directory. Override names with
`HUB`, `CLUSTER1`, `CLUSTER2`, and `RUNTIME_DIR`. The runtime directory defaults
to `/tmp/opencode/ocm-sql-hub-poc-sql-hub`, has mode `0700`, and contains
temporary CA keys and separate host/internal OCM API kubeconfigs. Treat it as
sensitive. Set `OCM_API_HOST_PORT` if localhost port `17443` is already used.

The script:

1. Creates an ordinary etcd-backed hosting cluster.
2. Deploys PostgreSQL, Kine, a standalone kube-apiserver and its supporting
   kube-controller-manager in that cluster.
3. Creates a separate CA and an external-hub kubeconfig, then installs the
   ClusterManager chart in Hosted mode from this checkout.
4. Wires registration, work and add-on webhooks into hosting-cluster DNS so
   the standalone API server can reach the Hosted webhook pods.
5. Creates two etcd-backed kind spokes and joins both directly to the OCM API.
6. Verifies registration, availability, ManifestWork delivery and a minimal
   AddOnTemplate-driven workload on both spokes.

If setup was interrupted after creating the clusters and Hosted controllers,
fix the reported host limit and run `./local-resume.sh` to continue without
recreating the hosting cluster or database. The resume step repairs the spoke
bootstrap kubeconfigs to use the internal OCM API endpoint. For other partial
failures, inspect the resources and use `./local-down.sh` before starting over.

## Inspect

```bash
kubectl --context kind-sql-hub get pods -A
kubectl --kubeconfig /tmp/opencode/ocm-sql-hub-poc-sql-hub/ocm-api-host.kubeconfig get managedclusters
kubectl --kubeconfig /tmp/opencode/ocm-sql-hub-poc-sql-hub/ocm-api-host.kubeconfig get manifestworks -A
kubectl --context kind-sql-spoke1 -n default get configmap sql-hosted-smoke,sql-poc-addon
kubectl --context kind-sql-spoke2 -n default get configmap sql-hosted-smoke,sql-poc-addon
kubectl --context kind-sql-hub -n ocm-sql-api logs deployment/ocm-sql-apiserver -c kine
kubectl --context kind-sql-hub -n ocm-sql-api exec statefulset/ocm-sql-postgres -- psql -U ocm_kine -d ocm_kine -c 'select count(*) from kine;'
```

The generated kubeconfig paths change when `HUB` or `RUNTIME_DIR` is
overridden. Use `ocm-api-host.kubeconfig` for host-shell commands and
`ocm-api-internal.kubeconfig` only for clients inside the hosting kind network.
Both have administrator credentials for this disposable POC only.

## Teardown

```bash
./local-down.sh
```

Teardown deletes only the three named POC kind clusters and generated files in
the marked runtime directory. It leaves the shared container-engine `kind`
network and unrelated clusters untouched. Pass the same name and runtime
overrides used during setup.

## Load simulator

The [ManifestWork load simulator](../manifestwork-simulator/README.md) lives
in this worktree at `solutions/manifestwork-simulator`. It generates synthetic
fleet load (registration, leases, ManifestWork create/update/watch/status)
against the Kine-backed OCM API and collects API-server, etcd, Kine and
PostgreSQL metrics in a loopback-only dashboard. See
[SIMULATOR-SETUP.md](SIMULATOR-SETUP.md) for running it against this POC. The
simulator writes nothing until it is started with `--enable-load` and the
operator types `RUN` in the dashboard; point it only at a disposable test API.
Its direct-hub comparison is a modeled estimate unless calibrated against a
measured etcd-backed baseline; do not present it as measured savings.

## Proof-of-technology results

Recorded outcome of the clean end-to-end run (November 5, 2026; full
configuration and test table in
[`IMPLEMENTATION-AND-TEST-RESULTS.md`](IMPLEMENTATION-AND-TEST-RESULTS.md)):

- `local-up.sh` completed from scratch and printed `POC ready`;
  partial-setup recovery via `local-resume.sh` also passed.
- Both spokes joined, became available (`Joined=True`, `Available=True`), and
  received the `sql-hosted-smoke` ManifestWork and the `sql-poc-addon`
  add-on workload on both spokes.
- Separation held: the ManagedCluster CRD was present in the standalone OCM
  API and `NotFound` in the hosting API, and PostgreSQL contained Kine
  records (483 rows at inspection time).
- Kine `v0.14.14` reported slow SQL queries and duplicate-key conflicts on
  `kine_name_prev_revision_uindex` during concurrent add-on status updates.
  The tested flows completed, but write reliability remains unresolved.

These results demonstrate functional feasibility of the tested flows only —
not throughput, durability, recovery, or production readiness.

## Boundaries and open validation

The clean from-scratch setup and functional checks passed in the tested local
environment. Detailed configuration and results are in
[`IMPLEMENTATION-AND-TEST-RESULTS.md`](IMPLEMENTATION-AND-TEST-RESULTS.md).

- This is a local feasibility demo, not a production deployment or migration
  path. Never use its generated credentials or database settings in a real
  cluster.
- Kine's etcd-compatible listener is loopback-only in the standalone
  kube-apiserver Pod. The PostgreSQL connection uses in-cluster networking;
  production TLS, backup/restore, HA and credential rotation are out of scope.
- The OCM API is RBAC-protected and trusts the dedicated admin CA plus the
  registration signer CA. The bootstrap admin credential is deliberately
  powerful and kept only in the temporary runtime directory.
- Hosted mode does not create the add-on conversion Service in the hosting
  cluster. The POC creates hosting-cluster Services for the Hosted webhook
  pods; Kubernetes maintains their endpoints as pods move. These transport
  Services do not store OCM state.
- PostgreSQL is the runnable backend. SQLite is not wired into this topology;
  Kine documents SQLite as single-instance only, so any comparison would be a
  separate single-node experiment.
- Kine implements the etcd API subset Kubernetes needs, not a general etcd
  replacement. Test the pinned Kubernetes/Kine compatibility and restart
  recovery before drawing broader conclusions.
- During this run, Kine `v0.14.14` reported slow SQL queries and duplicate-key
  conflicts on `kine_name_prev_revision_uindex` during concurrent add-on status
  updates. The tested registration, ManifestWork, and add-on delivery flows
  eventually completed, but the conflicts need investigation; this run is not
  evidence of acceptable performance or write reliability.
- The clean-run clusters (`sql-fresh-hub`, `sql-fresh-spoke1`,
  `sql-fresh-spoke2`) were left on the test machine, but the runtime directory
  holding the generated credentials and ownership marker has since been
  removed. The documented kubeconfig inspection commands therefore no longer
  resolve, and `local-down.sh` refuses to act without the marker; remove the
  leftover clusters with `kind delete cluster --name sql-fresh-hub` (and each
  spoke) directly. Teardown was validated on an earlier isolated partial run,
  but has not been repeated after the successful clean run.
- The demo does not claim a measured etcd-load reduction or production
  suitability.
