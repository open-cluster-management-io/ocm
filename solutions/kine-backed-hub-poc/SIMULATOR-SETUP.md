# Kine environment and simulator setup

Use this guide to run the Kine/PostgreSQL OCM API with the ManifestWork load simulator. This is a disposable local POC only; never use production clusters or credentials.

## 1. Start the Kine environment

Prerequisites: Docker or Podman, `kind`, `kubectl`, `helm`, `clusteradm`, `openssl`, `python3`, and Go. On Linux, setup may require higher inotify or keyring limits; see the [Kine POC README](README.md#run).

Choose unused kind cluster names and an unused localhost port. Example, from the Kine worktree root:

```bash
export HUB=kine-demo-hub
export CLUSTER1=kine-demo-spoke1
export CLUSTER2=kine-demo-spoke2
export RUNTIME_DIR=/tmp/opencode/ocm-sql-hub-poc-${HUB}
export OCM_API_HOST_PORT=17444

./solutions/kine-backed-hub-poc/local-up.sh
```

The script creates the hosting cluster, separate Kine-backed OCM API, and two spokes, then verifies registration and workload delivery. It refuses to reuse existing cluster names or a non-empty runtime directory. The runtime directory contains sensitive, temporary administrator credentials; do not share it.

If setup stops partway through, inspect the error and use `./solutions/kine-backed-hub-poc/local-resume.sh` only for a supported interrupted setup. Otherwise inspect resources before deciding whether to tear down and retry.

## 2. Start the simulator

Create a dedicated hosting kubeconfig for the hosting kind cluster:

```bash
kind export kubeconfig --name "$HUB" --kubeconfig "$RUNTIME_DIR/hosting.kubeconfig"
```

In another terminal, from the worktree root, run the simulator:

```bash
export HUB=kine-demo-hub
export RUNTIME_DIR=/tmp/opencode/ocm-sql-hub-poc-${HUB}
go run ./solutions/manifestwork-simulator \
  --hosting-kubeconfig "$RUNTIME_DIR/hosting.kubeconfig" \
  --ocm-kubeconfig "$RUNTIME_DIR/ocm-api-host.kubeconfig" \
  --enable-load
```

Open <http://127.0.0.1:8080>. The simulator binds to loopback. Load generation is explicitly enabled above, but it creates no simulator resources until `RUN` is typed in the dashboard. Start with small values and confirm the dashboard is connected to the disposable Kine OCM API before starting. The simulator relies on the POC's registration auto-approval and client-certificate signer.

The hosting kubeconfig is used for hosting API and container metrics; the OCM kubeconfig is used for simulator resources and the Kine-backed API metrics. Missing optional metrics may appear blank. The direct-hub comparison is an estimate unless calibrated with a separate etcd-backed baseline; do not present it as measured savings.

For permissions, dashboard behavior, recovery by run ID, and simulator safety boundaries, see the [simulator README](../manifestwork-simulator/README.md).

## 3. Stop and clean up

In the dashboard, choose **Stop & clean up** and wait for owned simulator resources to finish deleting. Then stop the simulator with `Ctrl-C`. If the process exits before cleanup completes, restart it with the same `--run-id` printed at startup to recover and clean up that run.

Remove the extra hosting kubeconfig, then tear down the POC from the Kine worktree root, using the same environment variables as setup:

```bash
rm -f "$RUNTIME_DIR/hosting.kubeconfig"
./solutions/kine-backed-hub-poc/local-down.sh
```

Teardown deletes the three named POC kind clusters and generated credentials. It leaves unrelated clusters and the shared kind network untouched. See the [Kine POC README](README.md) for inspection commands and details.
