# Kine-backed OCM API: implementation and test results

**Validation date:** November 5, 2026

## Executive summary

This proof of concept demonstrated that OCM hub state can be served by a separate Kubernetes API backed by Kine and PostgreSQL while the hosting cluster keeps its native etcd-backed API. A clean automated setup deployed Hosted-mode OCM controllers and connected two kind spokes; both joined, became available, and received ManifestWork and add-on workloads. API checks confirmed that the ManagedCluster CRD is present in the standalone OCM API but absent from the hosting API, and PostgreSQL contained Kine records. Two startup timing gaps were corrected during testing. This demonstrates functional feasibility for the tested flows, not production readiness, performance improvement, or write reliability: slow Kine queries and duplicate-key conflicts observed during concurrent add-on status updates remain unresolved.

## Objective and scope

Validate a split control-plane topology that:

- Keeps hosting-cluster infrastructure in the native etcd-backed API.
- Routes OCM hub controllers and spoke agents through a separate OCM API backed by Kine/PostgreSQL.
- Supports registration and workload delivery to two spokes.
- Allows isolated testing without disturbing existing kind clusters.

This is a disposable local feasibility experiment, not a production deployment or migration solution.

## Architecture and state ownership

```text
Hosting kind cluster — native API → etcd
│
├── ClusterManager operator and Hosted OCM controllers/webhooks
├── Standalone OCM kube-apiserver → Kine → PostgreSQL
└── Supporting kube-controller-manager
                  ▲
                  │ Separate OCM API
          ┌───────┴───────┐
       kind spoke 1    kind spoke 2
       native etcd     native etcd
```

| API/storage | Resources |
|---|---|
| Hosting API / etcd | Infrastructure Deployments, Pods, Services, Secrets, the ClusterManager operator and ClusterManager resource |
| Standalone OCM API / Kine / PostgreSQL | OCM CRDs and hub resources, including ManagedClusters, registration CSRs, hub RBAC, Leases, ManifestWorks, placements and add-on resources |
| Spoke APIs / etcd | Spoke-local resources and delivered workloads |

The standalone API uses Kine's etcd-compatible endpoint at `127.0.0.1:12379`; Kine connects to PostgreSQL through an in-cluster Service. The standalone kube-apiserver and supporting kube-controller-manager run as workloads on the hosting cluster. The latter provides the minimal namespace, ServiceAccount, garbage-collection, bootstrap-token and CSR-signing controllers needed by the logical OCM API; it does not manage the hosting cluster.

## Implementation

### API, storage and credentials

`local-up.sh` creates an etcd-backed hosting kind cluster, deploys single-instance PostgreSQL and Kine, and runs a separate kube-apiserver and kube-controller-manager. The OCM API enables RBAC and disables anonymous authentication. The script generates dedicated serving and client certificates and ServiceAccount signing keys.

Two kubeconfigs distinguish network paths: a host kubeconfig uses localhost, and an internal kubeconfig uses the hosting node address for in-cluster controllers and spokes. The runtime directory is restricted and contains temporary CA keys and administrator credentials; these are for the disposable POC only.

### Hosted controllers and webhook connectivity

The ClusterManager chart is installed in Hosted mode. Chart values and templates support Hosted registration and work webhook addresses, ports and bind configuration. The setup creates hosting-cluster Services for the registration, work and add-on webhooks, and an ExternalName Service in the standalone API for add-on webhook resolution. Add-on management is enabled after webhook connectivity is configured.

### Automation and recovery

| File | Purpose |
|---|---|
| `local-up.sh` | Creates the topology and verifies registration and workload delivery |
| `local-resume.sh` | Continues a supported partial setup and repeats delivery checks |
| `local-down.sh` | Deletes only the named POC clusters and generated runtime files |
| `README.md` | Documents topology and operator instructions |

Setup checks prerequisites, kernel quotas, cluster-name collisions, runtime-directory reuse and host-port availability. Teardown requires the runtime ownership marker, removes only the named clusters and generated files, and retains the shared kind network.

## Test configuration

| Item | Tested value |
|---|---|
| Container engine | Podman |
| Hosting cluster | `sql-fresh-hub` |
| Spokes | `sql-fresh-spoke1`, `sql-fresh-spoke2` |
| Kubernetes / kind node version | `v1.36.1` |
| Kine version | `v0.14.14` |
| PostgreSQL image | `postgres:16-alpine` |
| Internal OCM API | `https://10.89.0.20:7443` |
| Host OCM API | `https://127.0.0.1:17444` |
| Runtime directory | `/tmp/opencode/ocm-sql-hub-poc-sql-fresh-hub` |

The isolated cluster names and alternate localhost port avoided interference with the existing `sql-hub`, `sql-spoke1` and `sql-spoke2` topology. The clean run used:

```bash
CONTAINER_ENGINE=podman \
HUB=sql-fresh-hub \
CLUSTER1=sql-fresh-spoke1 \
CLUSTER2=sql-fresh-spoke2 \
RUNTIME_DIR=/tmp/opencode/ocm-sql-hub-poc-sql-fresh-hub \
OCM_API_HOST_PORT=17444 \
./solutions/kine-backed-hub-poc/local-up.sh
```

## Issues found and fixes

### Add-on manager creation race

The initial clean setup checked rollout status before the add-on manager Deployment existed and failed with `NotFound`. `local-up.sh` now waits for Deployment creation, then checks rollout readiness and reports a specific error if the Deployment does not appear.

### Spoke registration race

Recovery initially attempted to inspect or accept ManagedClusters before the registration agents had created them. Both setup and resume now wait for each ManagedCluster resource before proceeding with acceptance and availability checks.

The isolated partial topology was cleaned up and recreated after these changes. The final clean `local-up.sh` run completed without using the resume path.

## Test results

| Test | Result | Evidence |
|---|---|---|
| Clean end-to-end setup | **Passed** | `local-up.sh` completed and printed `POC ready` |
| Partial setup recovery | **Passed** | `local-resume.sh` completed registration and delivery checks |
| Spoke registration and availability | **Passed** | Both spokes showed `hubAcceptsClient=true`, `Joined=True` and `Available=True` |
| ManifestWork delivery | **Passed** | `sql-hosted-smoke` ConfigMap delivered to both spokes with expected data |
| Add-on workload delivery | **Passed** | `sql-poc-addon` ConfigMap delivered to both spokes with expected data |
| Add-on status | **Passed** | Both ManagedClusterAddons showed `Available=True` and `Progressing=False` |
| Hosting/OCM API separation | **Passed** | ManagedCluster CRD was `NotFound` in hosting API and present in standalone OCM API |
| PostgreSQL storage activity | **Passed** | `select count(*) from kine;` returned `483` at inspection time |
| Isolated teardown before clean rerun | **Passed** | Named fresh clusters and generated credentials were removed successfully |
| Preservation of existing topology | **Passed for recorded operations** | Original `sql-*` clusters were left untouched |
| Bash syntax checks | **Passed** | `bash -n` completed without errors |
| Helm lint/template and whitespace checks | **Passed** | Recorded lint, rendering and `git diff --check` completed successfully |

The final successful fresh topology was left running for inspection. Teardown was verified on the earlier isolated partial run; a teardown after the final successful clean run was not performed. A Kine row count demonstrates storage activity only; it is not an object count or durability measurement.

## Limitations and unresolved risks

### Kine concurrency and performance

During testing with Kine `v0.14.14`, slow SQL queries and duplicate-key conflicts were observed on `kine_name_prev_revision_uindex` during concurrent add-on status updates. Registration, ManifestWork and add-on delivery eventually completed, but the cause and correctness implications remain unresolved. These results are not evidence of acceptable performance or write reliability.

### Validation boundaries

The testing did not establish:

- Acceptable throughput, latency or resource consumption.
- A measured reduction in hosting-cluster etcd load.
- Durability or recovery after API, Kine, PostgreSQL or node failure.
- High availability, backup/restore or disaster recovery.
- Comprehensive placement, admission, webhook or credential-rotation behavior.
- Production security or long-running stability.
- Compatibility beyond the tested Kubernetes/Kine configuration.

The database connection disables TLS, the components are single-instance, and generated administrator credentials are intentionally powerful. These choices are limited to this disposable POC. SQLite is not configured in this topology, and Kine is not a general-purpose etcd replacement.

## Recommended next steps

1. Reproduce the Kine conflicts with controlled concurrent writes; inspect retry behavior and check for lost or inconsistent updates.
2. Test API, Kine and PostgreSQL restarts, watch recovery and state persistence.
3. Measure latency, throughput and hosting-etcd activity against an equivalent etcd-backed baseline.
4. Add placement, workload update/deletion, admission rejection and credential-renewal tests.
5. Complete teardown of the final running topology when inspection is finished.

## Conclusion

The tested split topology is functionally feasible for registration and the exercised delivery flows. Reliability, performance and operational readiness remain open validation gates before broader adoption.
