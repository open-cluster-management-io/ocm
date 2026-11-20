#!/bin/bash
# Builds the demo: one OCM hub and two Kubernetes v1.37 spokes with Workload Aware
# Scheduling (gang + topology) enabled for JobSets and plain Jobs (WorkloadWithJob),
# JobSet v0.13 and LeaderWorkerSet v1.0 (both via Helm) with their WAS integrations on, and
# ManagedCluster labels that describe each spoke's node topology.

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

hub=${HUB:-was-hub}
spoke1=${SPOKE1:-was-spoke1}
spoke2=${SPOKE2:-was-spoke2}
node_image=${KIND_NODE_IMAGE:-kindest/node:v1.37.0}
jobset_version=${JOBSET_VERSION:-v0.13.1}
lws_version=${LWS_VERSION:-v1.0.0}
# Node labels a spoke can be scheduled against; each one found on a spoke becomes a
# ManagedCluster label so Placements can select on it.
topology_keys=${TOPOLOGY_KEYS:-topology.example.io/block topology.example.io/rack topology.kubernetes.io/zone}

hubctx="kind-${hub}"

for tool in kind kubectl clusteradm helm; do
  command -v "${tool}" >/dev/null || { echo "${tool} is required but not on PATH" >&2; exit 1; }
done

create_cluster() {
  local name=$1 config=$2
  if kind get clusters | grep -qx "${name}"; then
    echo "kind cluster ${name} already exists, reusing it"
    return
  fi
  kind create cluster --name "${name}" --image "${node_image}" ${config:+--config "${config}"}
}

install_jobset() {
  local ctx=$1
  # jobset-values.yaml turns on the WAS integration, which is gated in the controller
  # config rather than by a flag. JobSet chart versions have no leading "v".
  helm upgrade --install jobset oci://registry.k8s.io/jobset/charts/jobset --version "${jobset_version#v}" \
    --kube-context "${ctx}" --namespace jobset-system --create-namespace \
    -f manifests/jobset-values.yaml --wait --timeout 5m
  kubectl --context "${ctx}" apply -f manifests/jobset-work-agent-rbac.yaml
}

install_lws() {
  local ctx=$1
  # lws-values.yaml selects the kubernetes gang scheduling provider and turns on the
  # WorkloadAwareScheduling gate; the chart renders both into the controller config.
  helm upgrade --install lws oci://registry.k8s.io/lws/charts/lws --version "${lws_version}" \
    --kube-context "${ctx}" --namespace lws-system --create-namespace \
    -f manifests/lws-values.yaml --wait --timeout 5m
  kubectl --context "${ctx}" apply -f manifests/lws-work-agent-rbac.yaml
}

# The LWS webhook rejects spec.scheduling unless the kubernetes provider and the
# WorkloadAwareScheduling gate are configured, so a server-side dry run answers whether
# LWS gangs work here. Retries because the webhook can lag the rollout.
lws_gang_enabled() {
  local ctx=$1 attempt
  for attempt in $(seq 1 20); do
    if kubectl --context "${ctx}" create --dry-run=server -o name -f - >/dev/null 2>&1 <<'EOF'
apiVersion: leaderworkerset.x-k8s.io/v1
kind: LeaderWorkerSet
metadata:
  name: lws-gang-probe
  namespace: default
spec:
  scheduling:
    replica:
      schedulingPolicy:
        gang: {}
  leaderWorkerTemplate:
    size: 2
    workerTemplate:
      spec:
        containers:
        - name: probe
          image: registry.k8s.io/pause:3.10
EOF
    then
      return 0
    fi
    # A missing CRD will not fix itself; only retry while the webhook may be starting.
    kubectl --context "${ctx}" get crd leaderworkersets.leaderworkerset.x-k8s.io >/dev/null 2>&1 || return 1
    sleep 3
  done
  return 1
}

# The Job spec.scheduling field shows up in OpenAPI whether or not WorkloadWithJob is
# on, so probe instead: with the gate off the apiserver silently drops the field.
job_scheduling_enabled() {
  local ctx=$1
  kubectl --context "${ctx}" create --dry-run=server -o jsonpath='{.spec.scheduling.schedulingPolicy}' -f - <<'EOF' 2>/dev/null | grep -q gang
apiVersion: batch/v1
kind: Job
metadata:
  name: workload-with-job-probe
  namespace: default
spec:
  scheduling:
    schedulingPolicy:
      gang: {}
  template:
    spec:
      restartPolicy: Never
      containers:
      - name: probe
        image: registry.k8s.io/pause:3.10
EOF
}

# Turns the spoke's node topology into ManagedCluster labels on the hub:
#   <topology key>=<number of distinct domains>, e.g. topology.example.io/rack=2
# plus scheduling.example.io/workload-aware=true when the spoke serves the Workload API,
# scheduling.example.io/job-workload=true when plain Jobs get Workloads (WorkloadWithJob),
# and scheduling.example.io/lws-gang=true when LWS can gang-schedule on that API.
label_topology() {
  local cluster=$1 ctx="kind-$1" key count
  local labels=()
  for key in ${topology_keys}; do
    count=$(kubectl --context "${ctx}" get nodes -o jsonpath="{range .items[*]}{.metadata.labels.${key//./\\.}}{\"\\n\"}{end}" | sort -u | grep -c . || true)
    if [[ "${count}" -gt 0 ]]; then
      labels+=("${key}=${count}")
    else
      labels+=("${key}-")
    fi
  done
  if kubectl --context "${ctx}" api-resources --api-group=scheduling.k8s.io -o name | grep -qx 'workloads.scheduling.k8s.io'; then
    labels+=("scheduling.example.io/workload-aware=true")
  else
    labels+=("scheduling.example.io/workload-aware-")
  fi
  if job_scheduling_enabled "${ctx}"; then
    labels+=("scheduling.example.io/job-workload=true")
  else
    labels+=("scheduling.example.io/job-workload-")
  fi
  if lws_gang_enabled "${ctx}"; then
    labels+=("scheduling.example.io/lws-gang=true")
  else
    labels+=("scheduling.example.io/lws-gang-")
  fi
  kubectl --context "${hubctx}" label managedcluster "${cluster}" --overwrite "${labels[@]}"
}

create_cluster "${hub}" ""
create_cluster "${spoke1}" kind/spoke1.yaml
create_cluster "${spoke2}" kind/spoke2.yaml

echo "Initializing the OCM hub"
# The hub work controller that reconciles ManifestWorkReplicaSets only runs with this gate.
clusteradm init --wait --feature-gates=ManifestWorkReplicaSet=true --context "${hubctx}"
joincmd=$(clusteradm get token --context "${hubctx}" | grep "clusteradm join")

for spoke in "${spoke1}" "${spoke2}"; do
  echo "Joining ${spoke}"
  $(echo "${joincmd}" --force-internal-endpoint-lookup --wait --context "kind-${spoke}" | sed "s/<cluster_name>/${spoke}/g")
done
clusteradm accept --context "${hubctx}" --clusters "${spoke1},${spoke2}" --wait

for spoke in "${spoke1}" "${spoke2}"; do
  echo "Installing JobSet ${jobset_version} with WAS enabled on ${spoke}"
  install_jobset "kind-${spoke}"
  echo "Installing LeaderWorkerSet ${lws_version} with WAS enabled on ${spoke}"
  install_lws "kind-${spoke}"
  label_topology "${spoke}"
done

kubectl --context "${hubctx}" apply -f manifests/clusterset.yaml
kubectl --context "${hubctx}" label managedcluster "${spoke1}" "${spoke2}" --overwrite \
  cluster.open-cluster-management.io/clusterset=was-demo

kubectl --context "${hubctx}" get managedclusters --show-labels
