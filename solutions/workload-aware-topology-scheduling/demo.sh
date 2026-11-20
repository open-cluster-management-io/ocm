#!/bin/bash
# Walks through the multicluster topology-aware gang scheduling demo step by step,
# for JobSets, plain batch/v1 Jobs and LeaderWorkerSets.
# Runs setup.sh first if the kind clusters do not exist yet.
#
#   ./demo.sh                 # pause before each step
#   DEMO_AUTO=1 ./demo.sh     # run straight through

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

hub=${HUB:-was-hub}
spoke1=${SPOKE1:-was-spoke1}
spoke2=${SPOKE2:-was-spoke2}
auto=${DEMO_AUTO:-0}
timeout=${DEMO_TIMEOUT:-300}

H="kubectl --context kind-${hub}"
S1="kubectl --context kind-${spoke1}"
S2="kubectl --context kind-${spoke2}"

bold=$(tput bold 2>/dev/null || true)
cyan=$(tput setaf 6 2>/dev/null || true)
green=$(tput setaf 2 2>/dev/null || true)
reset=$(tput sgr0 2>/dev/null || true)

step() {
  echo
  echo "${bold}${cyan}==> $*${reset}"
  if [[ "${auto}" != 1 ]]; then
    read -r -p "    press enter to continue " _
  fi
}

note() { echo "${green}    $*${reset}"; }

# Echoes the command before running it so the audience sees what is executed.
run() {
  echo "${bold}\$ $*${reset}"
  eval "$@"
}

wait_for() {
  local desc=$1 cmd=$2 deadline=$((SECONDS + timeout))
  echo "    waiting for ${desc}..."
  until eval "${cmd}" >/dev/null 2>&1; do
    if ((SECONDS > deadline)); then
      echo "timed out after ${timeout}s waiting for ${desc}" >&2
      exit 1
    fi
    sleep 3
  done
}

# $2 is a pod label selector, e.g. jobset.sigs.k8s.io/jobset-name=rack-gang.
running_pods() {
  $1 get pods -l "$2" --field-selector=status.phase=Running -o name | wc -l
}

pods_table() {
  run "$1 get pods -l $2" \
    "-o custom-columns=POD:.metadata.name,STATUS:.status.phase,NODE:.spec.nodeName,PODGROUP:.spec.schedulingGroup.podGroupName"
}

for tool in kind kubectl clusteradm helm; do
  command -v "${tool}" >/dev/null || { echo "${tool} is required but not on PATH" >&2; exit 1; }
done

missing=0
for c in "${hub}" "${spoke1}" "${spoke2}"; do
  kind get clusters 2>/dev/null | grep -qx "${c}" || missing=1
done
if ((missing)); then
  step "Demo clusters not found: running setup.sh (takes ~10 minutes)"
  ./setup.sh
fi

# Start from a clean slate so the demo can be re-run.
$H -n was-demo delete manifestworkreplicaset rack-gang zone-gang rack-job rack-lws --ignore-not-found --wait >/dev/null
$H -n was-demo delete placement rack-topology zone-topology rack-job-topology rack-lws-topology --ignore-not-found >/dev/null
for ctx in "$S1" "$S2"; do
  $ctx delete jobset rack-gang zone-gang --ignore-not-found --wait >/dev/null
  $ctx delete job rack-job --ignore-not-found --cascade=foreground --wait >/dev/null
  $ctx delete leaderworkerset rack-lws --ignore-not-found --wait >/dev/null 2>&1 || true
done

for label in job-workload lws-gang; do
  if [[ -z "$($H get managedcluster "${spoke1}" -o jsonpath="{.metadata.labels.scheduling\.example\.io/${label}}")" ]]; then
    echo "${spoke1} is not labeled scheduling.example.io/${label}=true, so the clusters predate the Job/LWS demos." >&2
    echo "Recreate them with ./cleanup.sh && ./setup.sh" >&2
    exit 1
  fi
done

step "1. Two spokes, two different node topologies"
note "${spoke1}: racks inside a block"
run "$S1 get nodes -L topology.example.io/block,topology.example.io/rack"
note "${spoke2}: zones only, no racks"
run "$S2 get nodes -L topology.kubernetes.io/zone"
note "Both run Kubernetes 1.37 and serve the Workload/PodGroup API:"
run "$S1 api-resources --api-group=scheduling.k8s.io"

step "2. The hub sees each spoke's topology as ManagedCluster labels"
run "$H get managedclusters -L topology.example.io/rack,topology.example.io/block,topology.kubernetes.io/zone,scheduling.example.io/workload-aware,scheduling.example.io/job-workload,scheduling.example.io/lws-gang"

step "3. A Placement that needs a WAS-capable cluster with a rack topology"
run "cat manifests/placement-rack.yaml"
run "$H apply -f manifests/placement-rack.yaml"
wait_for "a placement decision" \
  "$H -n was-demo get placementdecisions -l cluster.open-cluster-management.io/placement=rack-topology -o jsonpath='{.items[*].status.decisions[*].clusterName}' | grep -q ."
run "$H -n was-demo get placementdecisions -l cluster.open-cluster-management.io/placement=rack-topology" \
  "-o jsonpath='{\"selected cluster: \"}{.items[*].status.decisions[*].clusterName}{\"\\n\"}'"

step "4. Deliver a gang-scheduled, rack-constrained JobSet through that Placement"
note "2 Jobs x 2 pods = one gang of 4; every pod must share a topology.example.io/rack domain"
run "sed -n '/kind: JobSet/,/replicatedJobs:/p' manifests/jobset-rack-gang.yaml"
run "$H apply -f manifests/jobset-rack-gang.yaml"
wait_for "4 running rack-gang pods on ${spoke1}" "[ \$(running_pods \"$S1\" jobset.sigs.k8s.io/jobset-name=rack-gang) -ge 4 ]"
run "$H -n was-demo get manifestworkreplicaset rack-gang"

step "5. On ${spoke1}: JobSet created a Workload and a gang PodGroup with the rack constraint"
run "$S1 get workloads.scheduling.k8s.io,podgroups.scheduling.k8s.io"
run "$S1 get podgroups.scheduling.k8s.io -l jobset.sigs.k8s.io/jobset-name=rack-gang" \
  "-o custom-columns=PODGROUP:.metadata.name,MINCOUNT:.spec.schedulingPolicy.gang.minCount,TOPOLOGY:.spec.schedulingConstraints.topology[*].key,SCHEDULED:.status.conditions[0].status"

step "6. All 4 pods were bound together, inside a single rack"
pods_table "$S1" jobset.sigs.k8s.io/jobset-name=rack-gang
run "$S1 get nodes -l topology.example.io/rack -L topology.example.io/rack"

step "7. JobSet status flows back to the hub via ManifestWork feedback (synced every ~60s)"
wait_for "status feedback on the hub" \
  "$H -n ${spoke1} get manifestwork -o jsonpath='{.items[*].status.resourceStatus.manifests[0].statusFeedback.values[0].name}' | grep -q ."
run "$H -n ${spoke1} get manifestwork" \
  "-o jsonpath='{range .items[*].status.resourceStatus.manifests[0].statusFeedback.values[*]}{.name}{\"=\"}{.fieldValue.integer}{\"\\n\"}{end}'"

step "8. A zone-constrained gang lands on the zone-capable cluster instead"
run "$H apply -f manifests/placement-zone.yaml -f manifests/jobset-zone-gang.yaml"
wait_for "2 running zone-gang pods on ${spoke2}" "[ \$(running_pods \"$S2\" jobset.sigs.k8s.io/jobset-name=zone-gang) -ge 2 ]"
run "$H -n was-demo get placementdecisions -l cluster.open-cluster-management.io/placement=zone-topology" \
  "-o jsonpath='{\"selected cluster: \"}{.items[*].status.decisions[*].clusterName}{\"\\n\"}'"
pods_table "$S2" jobset.sigs.k8s.io/jobset-name=zone-gang
run "$S2 get nodes -l topology.kubernetes.io/zone -L topology.kubernetes.io/zone"

step "9. Plain batch/v1 Jobs can be gangs too (WorkloadWithJob)"
note "No JobSet needed: the in-tree Job controller compiles Job.spec.scheduling into a Workload + PodGroup."
note "The hub only targets spokes labeled scheduling.example.io/job-workload=true (gate on, probed by setup.sh)."
run "cat manifests/placement-rack-job.yaml"
run "$H apply -f manifests/placement-rack-job.yaml"
wait_for "a placement decision" \
  "$H -n was-demo get placementdecisions -l cluster.open-cluster-management.io/placement=rack-job-topology -o jsonpath='{.items[*].status.decisions[*].clusterName}' | grep -q ."
run "$H -n was-demo get placementdecisions -l cluster.open-cluster-management.io/placement=rack-job-topology" \
  "-o jsonpath='{\"selected cluster: \"}{.items[*].status.decisions[*].clusterName}{\"\\n\"}'"

step "10. Deliver a gang-scheduled, rack-constrained Job through that Placement"
note "parallelism 4 = one gang of 4 (minCount defaults to parallelism); every pod must share a rack"
run "sed -n '/kind: Job$/,/template:/p' manifests/job-rack-gang.yaml"
run "$H apply -f manifests/job-rack-gang.yaml"
wait_for "4 running rack-job pods on ${spoke1}" "[ \$(running_pods \"$S1\" batch.kubernetes.io/job-name=rack-job) -ge 4 ]"
run "$H -n was-demo get manifestworkreplicaset rack-job"

step "11. On ${spoke1}: the Job controller owns this Workload and PodGroup"
run "$S1 get workloads.scheduling.k8s.io" \
  "-o custom-columns=WORKLOAD:.metadata.name,OWNER-KIND:.metadata.ownerReferences[0].kind,OWNER:.metadata.ownerReferences[0].name"
pg=$($S1 get pods -l batch.kubernetes.io/job-name=rack-job -o jsonpath='{.items[0].spec.schedulingGroup.podGroupName}')
run "$S1 get podgroups.scheduling.k8s.io ${pg}" \
  "-o custom-columns=PODGROUP:.metadata.name,OWNER:.metadata.ownerReferences[0].kind,MINCOUNT:.spec.schedulingPolicy.gang.minCount,TOPOLOGY:.spec.schedulingConstraints.topology[*].key,SCHEDULED:.status.conditions[0].status"
pods_table "$S1" batch.kubernetes.io/job-name=rack-job
run "$S1 get nodes -l topology.example.io/rack -L topology.example.io/rack"

step "12. Job status flows back to the hub via ManifestWork feedback (synced every ~60s)"
wait_for "status feedback on the hub" \
  "$H -n ${spoke1} get manifestwork -o jsonpath='{range .items[*].status.resourceStatus.manifests[?(@.resourceMeta.kind==\"Job\")]}{.statusFeedback.values[0].name}{end}' | grep -q ."
run "$H -n ${spoke1} get manifestwork" \
  "-o jsonpath='{range .items[*].status.resourceStatus.manifests[?(@.resourceMeta.kind==\"Job\")].statusFeedback.values[*]}{.name}{\"=\"}{.fieldValue.integer}{\"\\n\"}{end}'"

step "13. LeaderWorkerSet: one gang per replica (leader + workers), each inside one rack"
note "LWS v0.11 compiles spec.scheduling into a Workload; replica mode = one PodGroup per replica, minCount = size."
note "The hub only targets spokes labeled scheduling.example.io/lws-gang=true (provider + gate on, probed by setup.sh)."
run "cat manifests/placement-rack-lws.yaml"
run "$H apply -f manifests/placement-rack-lws.yaml"
wait_for "a placement decision" \
  "$H -n was-demo get placementdecisions -l cluster.open-cluster-management.io/placement=rack-lws-topology -o jsonpath='{.items[*].status.decisions[*].clusterName}' | grep -q ."
run "$H -n was-demo get placementdecisions -l cluster.open-cluster-management.io/placement=rack-lws-topology" \
  "-o jsonpath='{\"selected cluster: \"}{.items[*].status.decisions[*].clusterName}{\"\\n\"}'"

step "14. Deliver a 2-replica, size-3 LeaderWorkerSet through that Placement"
note "2 replicas x (1 leader + 2 workers) = two gangs of 3; each gang must share a topology.example.io/rack domain"
run "sed -n '/kind: LeaderWorkerSet/,/leaderWorkerTemplate:/p' manifests/lws-rack-gang.yaml"
run "$H apply -f manifests/lws-rack-gang.yaml"
wait_for "6 running rack-lws pods on ${spoke1}" "[ \$(running_pods \"$S1\" leaderworkerset.sigs.k8s.io/name=rack-lws) -ge 6 ]"
run "$H -n was-demo get manifestworkreplicaset rack-lws"

step "15. On ${spoke1}: LWS created one Workload and a gang PodGroup per replica"
run "$S1 get workloads.scheduling.k8s.io" \
  "-o custom-columns=WORKLOAD:.metadata.name,OWNER-KIND:.metadata.ownerReferences[0].kind,OWNER:.metadata.ownerReferences[0].name"
run "$S1 get podgroups.scheduling.k8s.io -l leaderworkerset.sigs.k8s.io/name=rack-lws" \
  "-o custom-columns=PODGROUP:.metadata.name,MINCOUNT:.spec.schedulingPolicy.gang.minCount,TOPOLOGY:.spec.schedulingConstraints.topology[*].key,SCHEDULED:.status.conditions[0].status"
note "Each replica's 3 pods share one rack; the two replicas may use different racks."
run "$S1 get pods -l leaderworkerset.sigs.k8s.io/name=rack-lws" \
  "-o 'custom-columns=POD:.metadata.name,REPLICA:.metadata.labels.leaderworkerset\.sigs\.k8s\.io/group-index,STATUS:.status.phase,NODE:.spec.nodeName,PODGROUP:.spec.schedulingGroup.podGroupName'"
run "$S1 get nodes -l topology.example.io/rack -L topology.example.io/rack"

step "16. LWS status flows back to the hub via ManifestWork feedback (synced every ~60s)"
wait_for "status feedback on the hub" \
  "$H -n ${spoke1} get manifestwork -o jsonpath='{range .items[*].status.resourceStatus.manifests[?(@.resourceMeta.kind==\"LeaderWorkerSet\")]}{.statusFeedback.values[0].name}{end}' | grep -q ."
run "$H -n ${spoke1} get manifestwork" \
  "-o jsonpath='{range .items[*].status.resourceStatus.manifests[?(@.resourceMeta.kind==\"LeaderWorkerSet\")].statusFeedback.values[*]}{.name}{\"=\"}{.fieldValue.integer}{\"\\n\"}{end}'"

step "17. Why the Placement matters: send the rack gang straight to ${spoke2} (no racks)"
# Pull the JobSet out of the ManifestWorkReplicaSet so the exact same spec is used.
run "$H apply --dry-run=client -f manifests/jobset-rack-gang.yaml" \
  "-o jsonpath='{.spec.manifestWorkTemplate.workload.manifests[0]}' | $S2 apply -f -"
wait_for "the PodGroup to report a scheduling result" \
  "$S2 get podgroups.scheduling.k8s.io -l jobset.sigs.k8s.io/jobset-name=rack-gang -o jsonpath='{.items[0].status.conditions[0].reason}' | grep -q ."
pods_table "$S2" jobset.sigs.k8s.io/jobset-name=rack-gang
run "$S2 get podgroups.scheduling.k8s.io -l jobset.sigs.k8s.io/jobset-name=rack-gang" \
  "-o custom-columns=PODGROUP:.metadata.name,REASON:.status.conditions[0].reason,MESSAGE:.status.conditions[0].message"
note "No rack domain exists, so the whole gang stays Pending; the hub Placement avoided this."
run "$S2 delete jobset rack-gang"

step "Done"
note "Re-run ./demo.sh any time; it resets the demo objects first."
note "Delete the clusters with ./cleanup.sh"
