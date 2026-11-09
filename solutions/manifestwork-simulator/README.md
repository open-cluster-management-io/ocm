# OCM ManifestWork load simulator

A local Go dashboard for measuring OCM API load from synthetic ManifestWork clients and comparing the split Kine/PostgreSQL topology with an estimated direct hub API/etcd topology.

## Safety and scope

- The dashboard binds to loopback only (`127.0.0.1:8080`).
- It performs no writes unless started with `--enable-load` and the operator types `RUN` in the dashboard.
- Each simulated cluster creates a real `ManagedCluster` registration object with the bootstrap identity (`open-cluster-management/agent-registration-bootstrap` ServiceAccount), matching the real klusterlet flow; the hub must auto-accept it. A CSR is then requested as that same ServiceAccount, and the hub must auto-approve it while its signer issues a client certificate. The simulator waits for that flow; it never approves its own CSR. Without approval/signing, the registration remains pending and no simulated agent operations start.
- After hub acceptance provisions the cluster namespace and per-cluster RBAC, the issued client certificate (identity `system:open-cluster-management:<cluster>:simulator-agent`) is used for the simulated agent's ManifestWork watch/status calls and `managed-cluster-lease` writes. Hub-side ManifestWork creation/spec updates use the configured OCM kubeconfig.
- This is not a full Klusterlet: it does not deploy registration/work agent pods or apply application manifests on real spokes. It deliberately avoids triggering add-on or placement controllers on a real fleet.
- `Stop & clean up` cancels agents and deletes only simulator-owned CSRs, ManagedClusters, ManifestWorks, leases (through namespace cleanup), and namespaces bearing the current run's ownership labels. If an unowned ManifestWork is found in a simulator namespace, cleanup preserves that namespace and reports the reason. Use a disposable test OCM API. Never point it at production.
- The Deployment, Service, Ingress, ConfigMap and Secret manifests are stored in ManifestWorks only; Secrets contain generated fake strings.
- Limits are 1–1,000 simulated clusters, 1–100 ManifestWorks per cluster, at most 10,000 total ManifestWorks, and 0–100 logical updates/sec. Simulated-agent status writes are concurrency-bounded, and cleanup runs in bounded parallel batches.

The workload drives CSR issuance, ManagedCluster create/acceptance, lease create/renew, and ManifestWork create/update/watch/status operations. Results should be calibrated against real Klusterlet runs before extrapolating to production scale.

## Implementation and API behavior

The standalone Go server is implemented in this directory. `main.go` loads the two kubeconfigs and optional Prometheus/baseline sources. `server.go` embeds `web/index.html` and serves the HTTP API. `simulator.go` owns reconciliation, synthetic ManifestWorks, simulated watch/status agents, and resource cleanup. `registration.go` implements the CSR bootstrap, certificate-authenticated ManagedCluster registration, and lease heartbeat. `metrics.go` samples API-server and Prometheus metrics; `model.go` validates bounds and fits the direct-hub estimate.

| Route | Behavior |
|---|---|
| `GET /` | Embedded dashboard page |
| `GET /api/state` | Current slider values, run state, owned-resource counts, errors, and the recent metrics history |
| `PUT /api/config` | Validates and applies cluster/work/update-rate settings; while started, reconciliation scales owned resources toward the requested values |
| `PUT /api/assumptions` | Validates the editable per-cluster, per-ManifestWork and per-update-rate API/etcd CPU and memory increments used by the provisional direct-hub estimate |
| `POST /api/start` | Requires `--enable-load` at process startup, request confirmation `RUN`, a working bootstrap ServiceAccount token request, and successful API preflight; starts CSR-backed registration |
| `POST /api/pause` | Stops logical ManifestWork updates while retaining resources and watches |
| `POST /api/stop` | Requests asynchronous, ownership-scoped cleanup; progress remains visible in active/terminating counts and the finalized/total namespace cleanup card |
| `POST /api/reset-history` | Drops collected metric samples; the captured idle baseline is kept |

Writes require same-origin requests. The web server binds to loopback and rejects non-loopback listen addresses. Start is additionally gated by the command-line opt-in and typed confirmation. The OCM kubeconfig is used only for simulator namespaces and ManifestWorks; it is not returned to the browser.

Each simulated agent lists and watches its namespace's run-labeled ManifestWorks using its issued client certificate, then writes work-level and per-manifest `Applied=True` and `Available=True` status conditions for the observed generation. A global concurrency bound limits simultaneous status writes. Each registered cluster also creates and renews a `managed-cluster-lease` every 60 seconds. The fleet update slider schedules bounded logical spec updates; the dashboard separately reports CSR/ManagedCluster registration, lease writes, work updates/status writes, API request rate, and PostgreSQL transaction rate.

Scale-down cancels simulated agents, removes only run-owned ManifestWorks and CSRs, deletes the simulator-owned ManagedCluster and waits for its finalizers, then deletes only the simulator-owned namespace. UID preconditions guard deletes. Before deleting registration state or a namespace, cleanup checks for remaining ManifestWorks; if it finds one not owned by the run, it preserves the namespace and reports the blocker. Stop marks active clusters terminating immediately and performs cleanup in bounded parallel batches. Kubernetes may finalize namespace deletion asynchronously; the dashboard reports the pending count and allows a fresh run using new cluster indices after delete requests are accepted. Reusing an explicit `--run-id` after a process restart discovers leftover run-labeled resources and cleans them before allowing another start. Do not use simulator namespaces for unrelated workloads.

API-server CPU is computed from `/metrics` process counters, API memory from resident-memory metrics, request rate from request counters, and mean latency from histogram deltas. etcd and OCM component-pod CPU/memory are collected separately from cAdvisor or Prometheus. In a kind direct-hub run, API server and etcd values include the kind control-plane baseline; OCM component pods are reported as a separate aggregate (namespaces `cluster-manager` and `open-cluster-management*`) and are not added to API/etcd estimates. Collect an idle sample before load and compare the loaded values to distinguish total kind usage from workload-driven delta. Container usage and PostgreSQL transaction rate are Prometheus queries; missing source metrics remain blank and display their source limitation. The rolling history holds up to 180 samples.

Without a direct-hub baseline, the direct-hub graph is an immediate provisional projection: the **idle hosting API/etcd baseline** (captured from the first idle samples before any load starts, and frozen; CPU baselines use the quietest idle sample because background churn is one-sided, memory baselines use the idle average) plus editable per-cluster, per-ManifestWork and per-update-rate CPU/memory increments. Using the idle baseline keeps a loaded hub from double-counting its active workload; if no idle baseline has been captured yet, the live hosting measurement is used as the base and the estimate message says so. While running or paused—and while idle before a first run—the projection reflects the selected slider load immediately; after `Stop`, it tracks only the resources that remain, falling to the captured baseline as cleanup completes, and returns to the selected-load preview once the dashboard is idle again. The default coefficients are calibrated from two no-Kine kind benchmarks (250 simulated clusters × 10 ManifestWorks at 4 and 10 fleet updates/sec, CSR registration and leases included): measured load-driven deltas of +0.175/+0.313 API CPU cores, +497/+679 MiB API memory, +0.042/+0.057 etcd CPU cores, and +54/+80 MiB etcd memory. Two points separate static per-object cost from per-update-rate cost but cannot separate per-cluster from per-work cost, so the earlier split ratio is preserved; edit the values in the dashboard to test other assumptions. A supplied direct-hub CSV supersedes the provisional formula with four least-squares models using features `[1, cluster count, total ManifestWorks, update rate]`. A baseline must have at least five observations and a full-rank feature matrix. Its displayed ranges are `±1.96 × residual standard deviation`, clamped at zero; they are a rough fit uncertainty indicator, not formal prediction guarantees.

## Build and run

From the OCM repository root, configure kubeconfigs for a **disposable test OCM API**. The hub must have ManagedCluster and CSR auto-approval enabled for `system:serviceaccount:open-cluster-management:agent-registration-bootstrap`, plus a working Kubernetes client signer. Start with a kubeconfig permitted to request a token for that ServiceAccount. The OCM kubeconfig also needs permissions for the simulator's hub-side namespace/ManifestWork operations, CSR and ManagedCluster cleanup/inspection, and metric reads. The issued certificate's standard OCM agent RBAC must allow ManagedCluster creation, ManifestWork watch/status, and Lease create/get/update in the accepted cluster namespace.

The OCM kubeconfig needs `get/list/create/delete` on core Namespaces and `get/list/watch/create/update/delete` on `work.open-cluster-management.io/manifestworks`, including `update` on `manifestworks/status`; `create` on `serviceaccounts/token`; `get/list/delete` on CSRs; and `get/list/delete` on ManagedClusters. Bootstrap CSR creation is performed as the configured bootstrap ServiceAccount. The hosting kubeconfig needs permission to read `/metrics`, list Nodes, and access `nodes/proxy` for kubelet cAdvisor metrics; Prometheus credentials are separately optional.

```bash
go run ./solutions/manifestwork-simulator \
  --hosting-kubeconfig /path/to/hosting-api.kubeconfig \
  --ocm-kubeconfig /path/to/ocm-api.kubeconfig
```

Open <http://127.0.0.1:8080>. The server reports its random run ID on startup. Without `--enable-load`, the dashboard is read-only. To permit explicit test writes:

```bash
go run ./solutions/manifestwork-simulator \
  --hosting-kubeconfig /path/to/hosting-api.kubeconfig \
  --ocm-kubeconfig /path/to/ocm-api.kubeconfig \
  --enable-load
```

The dashboard offers cluster-count, ManifestWorks-per-cluster, and fleet-wide update-rate sliders. It shows active/desired/terminating resources, pending registrations with per-stage breakdown (ManagedCluster create, CSR, certificate issuance, hub acceptance, namespace setup), oldest and average pending-registration age, a stalled-registration count that flags registrations without stage progress for two minutes, CSR/ManagedCluster/lease counters, achieved versus requested updates/sec, API request rate/latency, logical work updates, simulated agent status writes and their request latency, and errors. During cleanup the namespace card shows finalized/total progress instead of a bouncing finalizing count. Clicking any chart marks that sample on every chart with a dashed crosshair, dots and values where the crosshair intersects each line, and a readout panel of all values at that timestamp; the selection is anchored to the sample's timestamp, so it slides left as new samples arrive and clears when the sample leaves the 180-sample window. A **Reset graphs** button drops collected samples without discarding the captured idle baseline. Chart colors are consistent across panels: API server blue, etcd green, OCM pods orange, Kine purple, PostgreSQL red. Clicking any chart marks that sample on every chart and shows a readout of all panel values at that timestamp. Slider changes while running reconcile resources; reducing clusters or works deletes excess simulator-owned resources. Pausing retains resources and watches but stops logical mutations. Stopping deletes the current run's owned resources.

The API-server `/metrics` endpoint is scraped through each kubeconfig for API process CPU/memory, API request rate/latency, and storage-backend operation rate/latency when `etcd_request_duration_seconds` is exposed. Container CPU/memory for etcd, Kine and PostgreSQL are collected through the hosting API's kubelet cAdvisor proxy when permitted. cAdvisor housekeeping runs per container, so a scrape can replay an older sample; CPU rates are computed from each container's own counter timestamps, and a container whose sample did not advance reports no CPU for that interval (a line gap) rather than a false zero. Prometheus can supply or override those values:

```bash
go run ./solutions/manifestwork-simulator \
  --hosting-kubeconfig /path/to/hosting-api.kubeconfig \
  --ocm-kubeconfig /path/to/ocm-api.kubeconfig \
  --hosting-prometheus-url https://prometheus-hosting.example \
  --ocm-prometheus-url https://prometheus-ocm.example \
  --hosting-prometheus-token-file /path/to/hosting-prometheus.token \
  --ocm-prometheus-token-file /path/to/ocm-prometheus.token
```

Prometheus URLs are optional if cAdvisor proxy access is available. If neither source provides a measurement, the dashboard leaves that graph blank rather than substituting estimates. PostgreSQL transactions/sec requires the `pg_stat_database_xact_commit` and `pg_stat_database_xact_rollback` exporter metrics for database `ocm_kine`.

## Direct-hub estimate and calibration

The direct-hub panel is **not a live measurement**. It fits separate least-squares models for direct hub API-server CPU/memory and etcd CPU/memory using this CSV schema:

```csv
clusters,works_per_cluster,updates_per_second,api_cpu_cores,api_memory_bytes,etcd_cpu_cores,etcd_memory_bytes
```

Collect calibration samples by attaching the OCM kubeconfig to a disposable etcd-backed test hub, using Prometheus URLs that scrape that hub and its etcd, then running with `--enable-load --record-direct-baseline --baseline-csv /secure/path/direct-hub.csv`. Capture idle kind API/etcd and OCM-component readings separately before load; API/etcd in the CSV are total measured process/container usage, including the kind baseline, while OCM component pods are a separate reported aggregate. Change slider settings between sustained runs so cluster count, work count and update rate vary independently. Recording begins only while all requested clusters are registered and the requested works have been provisioned. Samples are skipped if required CPU/memory metrics are missing or achieved update rate is below 90% of the requested rate; the dashboard reports recording status. Keep calibration data private if it contains operational metrics.

Load the resulting CSV in the split-mode comparison run with `--baseline-csv /secure/path/direct-hub.csv`. At least five varied observations and a full-rank workload matrix are required. Otherwise the dashboard explicitly reports “uncalibrated.” It flags slider settings outside the baseline's measured range as **extrapolated** and weak fits (`R² < 0.7`) as **weak fit**. The model reports its R² and residual-based 95% ranges; these intervals describe fit residuals and are not guarantees or a substitute for a controlled benchmark. Do not present a direct-hub estimate as measured savings.

The hosting API panel always measures the hosting kubeconfig. In split mode, the OCM panel measures the separate Kine-backed API. For a direct-hub calibration run, configure the OCM kubeconfig and Prometheus endpoints to the same etcd-backed hub being calibrated.

## Run identity and cleanup

Every run gets a random label ID printed at startup. If the process exits before the stop action completes, restart with the same explicit `--run-id <id>`; the dashboard will discover and clean namespaces owned by that run. It refuses to adopt a same-named namespace without the matching run label. Never manually remove resources based only on the `sim-` name prefix.

## Tests

```bash
go test ./solutions/manifestwork-simulator
```

Tests cover slider bounds and total-work safety limits, deterministic synthetic manifests, baseline calibration/uncertainty, opt-in and confirmation gates, watch cancellation, update/status behavior, and ownership-scoped cleanup.
