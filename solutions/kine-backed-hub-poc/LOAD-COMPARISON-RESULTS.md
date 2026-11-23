# Load comparison: Kine/PostgreSQL split control plane vs direct etcd-backed hub

**Validation date:** November 23, 2026

## Executive summary

The same synthetic ManifestWork fleet load was run against two disposable local
topologies: an ordinary etcd-backed kind hub with the ClusterManager chart in
Default mode, and the Kine/PostgreSQL split topology of this POC. The
[ManifestWork load simulator](../manifestwork-simulator/README.md) generated
the load in both runs, collected the metrics, and calibrated its direct-hub
comparison model from the etcd-backed run. The heaviest level was 250
simulated clusters with 2,500 ManifestWorks at 10 fleet updates per second.

Two results stand out at that level, measured as load-driven change above each
topology's idle baseline:

1. **The load moved; it did not shrink.** The direct hub's API server + etcd
   consumed +0.414 CPU cores; the split topology's standalone API server +
   Kine + PostgreSQL consumed +0.457 CPU cores (~10% more, within single-run
   noise). Both topologies added ~484 MiB to the OCM API server process and
   ~66–70 MiB to the storage layer. In the split topology the hosting
   cluster's own API server and etcd stayed flat (±0.004 CPU cores) — the
   entire OCM control-plane cost was transferred to the separate stack, not
   eliminated.
2. **The Kubernetes API server dominates both topologies.** The API server
   accounted for 84% of the control-plane CPU delta on the direct hub and 73%
   in the split topology. etcd cost +0.066 CPU cores for this workload; the
   Kine + PostgreSQL stack cost +0.125 CPU cores — about 1.9× etcd for the
   same workload. The API server's load-driven memory growth (+484 MiB) was
   identical in both topologies, indicating that cost is intrinsic to the API
   machinery (request handling, watch fan-out, serialization), not to the
   storage backend.

Kine `v0.14.14` slow SQL queries (up to 4.05 seconds during the measured run,
worsening to 12.0 seconds on a later rerun) and duplicate-key conflicts on
`kine_name_prev_revision_uindex` were reproduced under concurrent
registration writes; all tested flows still completed.

These measurements support the POC's functional feasibility conclusion and add
measured evidence for two framing points: separating the OCM API **moves** the
control-plane load off the hosting cluster, and exchanging etcd for
Kine/PostgreSQL does not address the dominant API-server cost. This is
evidence from one local machine at one scale, not a certified benchmark.

## Objective and scope

Test two claims against measured data:

- Splitting the OCM hub API onto Kine/PostgreSQL transfers resource usage from
  the hosting control plane to the new control plane rather than reducing
  total usage.
- When comparing etcd and PostgreSQL storage, the Kubernetes API server is
  the dominant resource consumer in either topology.

## Method

| Item | Value |
|---|---|
| Host | 16 CPU cores, 23 GiB RAM, Linux |
| Container engine / kind | Podman 5.7.0 / kind v0.32.0 |
| Kubernetes (all clusters) | v1.36.1 (`kindest/node:v1.36.1`) |
| OCM images | `quay.io/open-cluster-management/*:latest` via the ClusterManager chart |
| Split topology storage | Kine `v0.14.14`, `postgres:16-alpine` |
| Load generator | `solutions/manifestwork-simulator` (CSR-backed registration, leases, ManifestWork create/watch/status) |

The two runs were sequential on the same machine. Each run captured an idle
baseline before load, then executed the same level plan, holding each level
until steady and collecting metric samples every 5 seconds. Steady-state means
below average the samples captured during each level's hold window (48–60
samples per level, except the 1×1 warm-up with 18).

| Level | Clusters | ManifestWorks | Fleet updates/sec |
|---|---:|---:|---:|
| p0 | 1 | 1 | 0 |
| p1 | 100 | 1,000 | 0 |
| p2 | 250 | 1,250 | 0 |
| p3 | 250 | 2,500 | 0 |
| p4 | 250 | 2,500 | 4 |
| p5 | 250 | 2,500 | 10 |

Metric sources: API-server process CPU/memory, request rate and mean latency
from each API server's `/metrics`; etcd, Kine, PostgreSQL and OCM
component-pod CPU/memory from cAdvisor through the hosting API's kubelet
proxy. cAdvisor replay gaps leave some container CPU samples blank; means use
the available samples. The direct-hub run recorded its API/etcd usage into
[`direct-hub-baseline.csv`](direct-hub-baseline.csv) (110 samples across the
six levels), which the split-topology run loaded to replace the dashboard's
provisional estimate with a calibrated model.

Topology differences worth noting: the direct hub ran the ClusterManager chart
in Default mode with the bootstrap ServiceAccount auto-approval configuration
mirroring this POC's registration settings. The split topology is this POC's
`local-up.sh` output, including its two real joined spokes (a small constant
background the direct hub did not have).

## Results — direct etcd-backed hub

Steady-state means per level:

| Level | API CPU | API mem | etcd CPU | etcd mem | OCM pods CPU | OCM pods mem |
|---|---:|---:|---:|---:|---:|---:|
| idle | 0.057 | 377 MiB | 0.023 | 46 MiB | 0.007 | 225 MiB |
| p0 (1×1) | 0.085 | 385 MiB | 0.034 | 48 MiB | 0.007 | 232 MiB |
| p1 (100×10) | 0.138 | 588 MiB | 0.039 | 84 MiB | 0.007 | 280 MiB |
| p2 (250×5) | 0.167 | 723 MiB | 0.049 | 83 MiB | 0.008 | 307 MiB |
| p3 (250×10) | 0.180 | 783 MiB | 0.048 | 91 MiB | 0.008 | 335 MiB |
| p4 (250×10@4) | 0.295 | 798 MiB | 0.068 | 95 MiB | 0.015 | 357 MiB |
| p5 (250×10@10) | 0.406 | 862 MiB | 0.089 | 116 MiB | 0.022 | 373 MiB |

## Results — Kine/PostgreSQL split topology

Steady-state means per level:

| Level | OCM API CPU | OCM API mem | Kine CPU | Kine mem | PostgreSQL CPU | PostgreSQL mem | Hosting API CPU | Hosting etcd CPU | OCM pods CPU |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| idle | 0.038 | 348 MiB | 0.027 | 44 MiB | 0.008 | 47 MiB | 0.044 | 0.021 | 0.007 |
| p0 (1×1) | 0.053 | 356 MiB | 0.034 | 40 MiB | 0.013 | 47 MiB | 0.040 | 0.019 | 0.005 |
| p1 (100×10) | 0.096 | 545 MiB | 0.042 | 42 MiB | 0.024 | 61 MiB | 0.039 | 0.018 | 0.006 |
| p2 (250×5) | 0.121 | 616 MiB | 0.058 | 40 MiB | 0.052 | 74 MiB | 0.039 | 0.018 | 0.008 |
| p3 (250×10) | 0.149 | 745 MiB | 0.054 | 57 MiB | 0.048 | 78 MiB | 0.039 | 0.018 | 0.007 |
| p4 (250×10@4) | 0.250 | 770 MiB | 0.071 | 44 MiB | 0.056 | 85 MiB | 0.041 | 0.019 | 0.012 |
| p5 (250×10@10) | 0.370 | 832 MiB | 0.085 | 45 MiB | 0.075 | 111 MiB | 0.040 | 0.019 | 0.019 |

The hosting cluster's API server and etcd never left their idle range at any
level; the OCM workload's storage and API traffic went entirely to the
standalone stack.

## Deltas at the heaviest level (250 clusters, 2,500 works, 10 updates/sec)

Load-driven change above each topology's idle baseline:

| Component | Direct hub | Split topology |
|---|---:|---:|
| OCM API server CPU | +0.349 cores | +0.332 cores |
| OCM API server memory | +484 MiB | +484 MiB |
| Storage CPU | +0.066 cores (etcd) | +0.125 cores (Kine +0.058, PostgreSQL +0.067) |
| Storage memory | +70 MiB (etcd) | +66 MiB (Kine +1, PostgreSQL +65) |
| Combined control-plane CPU | +0.414 cores | +0.457 cores |
| API-server share of CPU delta | 84% | 73% |
| Hosting API / etcd CPU | — | −0.004 / −0.002 cores (flat) |

## What the dashboard showed

During the split-topology run the simulator's "Direct hub API — estimated"
panel was driven by the calibrated model (109 in-range samples, one of the
four least-squares fits below the R² 0.7 threshold, so the dashboard flagged
it **weak fit**). At the heaviest level the model estimated the direct hub at
0.418 CPU cores / 850 MiB for the API server and 0.088 CPU cores / 112 MiB
for etcd, against the directly measured 0.406 / 862 MiB and 0.089 / 116 MiB —
the calibrated estimate tracked the measurement within a few percent. The
dashboard's "Separate OCM control plane — measured" panel showed the live
standalone API server, Kine and PostgreSQL usage next to that estimate.

## Throughput and latency

Both topologies sustained the requested load: 3.8 of 4 and ~8.8–8.9 of 10
updates per second at p4/p5 (the ≥90% recording gate), with equivalent API
request rates (65.4 vs 62.7 requests/sec at p5).

Mean non-watch request latency and storage-operation latency against the OCM
API server:

| Level | Direct hub | Split topology |
|---|---|---|
| p1 | 3.5 ms / 1.8 ms | 54.4 ms / 12.4 ms |
| p2 | 3.9 ms / 1.6 ms | 70.9 ms / 26.7 ms |
| p3 | 4.2 ms / 1.7 ms | 95.7 ms / 37.2 ms |
| p4 | 4.0 ms / 1.7 ms | 13.4 ms / 5.2 ms |
| p5 | 4.4 ms / 2.0 ms | 6.6 ms / 2.9 ms |

The direct hub's etcd-backed latency was stable at 1.6–2.0 ms storage latency
across all levels. The Kine/PostgreSQL path was variable: tens of
milliseconds with spikes above 300 ms during the static levels, then single
digits during the update levels — not load-proportional. During the measured
run the Kine container logged 705 slow-SQL entries, the worst (the
`DISTINCT ON (name)` list query) taking 4.05 seconds, plus 16 duplicate-key
conflicts on `kine_name_prev_revision_uindex` during the concurrent
registration phases. All registrations and deliveries completed. A later
rerun of the top load level against the same database (whose `kine` table had
accumulated revisions from the earlier run) logged 5,076 slow-SQL entries
with a worst case of 12.0 seconds, suggesting the slow queries worsen as the
table grows. The cause remains unresolved; these numbers are not evidence of
acceptable write latency.

## Interpretation

1. **Transfer, not elimination.** At matched load the two control planes
   consumed comparable total resources (+0.414 vs +0.457 CPU cores; +554 vs
   +550 MiB including storage). The split topology's benefit is isolation:
   the hosting cluster's API server and etcd stayed at idle while the entire
   OCM cost landed on the separate stack — plus the Kine and PostgreSQL
   overhead that did not exist before. Nothing in this data shows a total
   resource reduction.
2. **The API server is the dominant cost.** In both topologies the API server
   used 3–5× the CPU of the storage layer and grew by the same +484 MiB
   regardless of whether storage was etcd or Kine/PostgreSQL. Exchanging etcd
   for Kine/PostgreSQL raised storage-layer CPU about 1.9× at this scale.
   Reducing OCM control-plane cost would have to target API-server load
   (request volume, watch fan-out, payload sizes, or sharding the API itself);
   swapping the storage backend does not address the dominant term.

These are measurements at one scale on one machine with a synthetic
workload. They are consistent with the claims above; they do not establish
behavior at larger fleet sizes, with real Klusterlet agents, or under
production traffic mixes.

## Limitations and unresolved risks

- One run per topology, no repetitions; a ~10% CPU difference between
  topologies is within expected single-run noise.
- kind-based, single-node control planes; cAdvisor sampling gaps blank some
  container CPU samples (means use available samples).
- Mean request latency, not percentiles; the simulator measures its own
  traffic only.
- The simulator models agent API behavior (registration, leases, work
  watch/status), not full Klusterlet deployments or spoke application
  runtimes.
- The split topology carried its two real joined spokes as a small constant
  background; the direct hub carried none.
- Kine and PostgreSQL are single-instance with TLS disabled; no HA, backup,
  recovery or durability validation.
- The Kine slow-query and duplicate-key behavior documented in
  [IMPLEMENTATION-AND-TEST-RESULTS.md](IMPLEMENTATION-AND-TEST-RESULTS.md)
  reproduced here; write reliability remains unresolved.

## Reproduction

Artifacts: [`direct-hub-baseline.csv`](direct-hub-baseline.csv) (recorded
calibration data), and the captured per-sample summaries referenced in the
tables above.

1. Direct hub: create a kind cluster, install the ClusterManager chart in
   Default mode with `createBootstrapSA: true` and the
   `ManagedClusterAutoApproval` feature gate, then run the simulator with
   `--enable-load --record-direct-baseline --baseline-csv direct-hub.csv`
   against that cluster for both kubeconfigs.
2. Split topology: run `./local-up.sh`, then run the simulator with
   `--hosting-kubeconfig <hosting> --ocm-kubeconfig <runtime-dir>/ocm-api-host.kubeconfig
   --enable-load --baseline-csv direct-hub-baseline.csv`.
3. Drive the level plan through the dashboard (or its HTTP API) and collect
   `/api/state` history snapshots; steady-state means come from each level's
   hold window.

## Conclusion

The split Kine/PostgreSQL control plane moved the OCM hub's API-server and
storage load off the hosting cluster without reducing total consumption, and
in both topologies the Kubernetes API server — not the storage backend — was
the dominant resource consumer. The storage swap itself added CPU (Kine +
PostgreSQL used ~1.9× etcd's CPU at the heaviest level) and introduced
latency variance and unresolved Kine write-conflict behavior. Any follow-on
work aimed at reducing OCM control-plane cost should target API-server load;
any work on the split topology should treat Kine write reliability as an open
gate, as already recorded in the implementation results.
