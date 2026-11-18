#!/usr/bin/env bash
# Cluster delete, in the documented order: remove the cluster's SPIRE entries, then delete the
# ManagedCluster. The work agent still holds its last SVID, and the hub lets it clean up: what the
# cluster's ManifestWorks applied is removed from the managed cluster and the ManifestWorks go
# away. The delete guard holds the cluster for maxSVIDLifetime + maxClockSkew, then it is gone and
# is not created again. Leaves the cluster deleted and its SPIRE entries saved in $ENTRIES_FILE.
set -euo pipefail
source "$(dirname "$0")/env.sh"
CLUSTER="${CLUSTER:-cluster1}"
ENTRIES_FILE="${ENTRIES_FILE:-$SPIFFE_E2E_DIR/../../../_output/${CLUSTER}-entries.yaml}"

seconds() { # 3m, 1m30s or 45s to seconds
  local d=$1 s=0
  [[ $d =~ ([0-9]+)h ]] && s=$((s + BASH_REMATCH[1] * 3600))
  [[ $d =~ ([0-9]+)m ]] && s=$((s + BASH_REMATCH[1] * 60))
  [[ $d =~ ([0-9]+)s ]] && s=$((s + BASH_REMATCH[1]))
  echo $s
}
spiffe=$(kubectl --context "$HUB_CTX" get clustermanager cluster-manager \
  -o jsonpath='{.spec.registrationConfiguration.registrationDrivers[?(@.authType=="spiffe")].spiffe}')
hold=$(( $(seconds "$(jq -r .maxSVIDLifetime <<<"$spiffe")") + $(seconds "$(jq -r .maxClockSkew <<<"$spiffe")") ))
echo "the delete guard should hold the cluster for ${hold}s"

# wait_agents_lead: after the agents restart, wait until the new work agent holds its leader lease;
# an old pod that didn't release it makes the new one wait for the lease to expire.
wait_agents_lead() {
  local pod holder
  for _ in $(seq 60); do
    pod=$(kubectl --context "$SPOKE_CTX" -n open-cluster-management-agent get pods -o name | grep -m1 work-agent || true)
    holder=$(kubectl --context "$SPOKE_CTX" -n open-cluster-management-agent get lease work-agent-lock \
      -o jsonpath='{.spec.holderIdentity}' 2>/dev/null || true)
    [[ -n "$pod" && "$holder" == "${pod#pod/}"_* ]] && return 0
    sleep 5
  done
  echo "FAIL: the new work agent didn't become leader"; exit 1
}
# Start from fresh agents: a new connection and a new SVID. Otherwise, after short SVIDs (for
# example check-rotation's), the hub may close the connection at the old SVID's expiry just as the
# entries go, and the agent's reconnect backoff can outlast its last SVID, so it can't clean up.
kubectl --context "$SPOKE_CTX" -n open-cluster-management-agent delete pods --all | cat
kubectl --context "$SPOKE_CTX" -n open-cluster-management-agent rollout status deployment --timeout 5m | cat
wait_agents_lead

cat <<YAML | kubectl --context "$HUB_CTX" apply -f - >/dev/null
apiVersion: work.open-cluster-management.io/v1
kind: ManifestWork
metadata: {name: delete-cleanup, namespace: ${CLUSTER}}
spec:
  workload:
    manifests:
    - apiVersion: v1
      kind: ConfigMap
      metadata: {name: delete-cleanup, namespace: default}
      data: {v: "1"}
YAML
on_spoke() { kubectl --context "$SPOKE_CTX" -n default get cm delete-cleanup >/dev/null 2>&1; }
# Up to 150s: a reconnect after a restart can wait up to 2 minutes (questions.md Q18).
for _ in $(seq 150); do on_spoke && break; sleep 1; done
on_spoke || { echo "FAIL: the ManifestWork wasn't applied before the test"; exit 1; }

entries=$(kubectl --context "$SPOKE_CTX" get clusterspiffeid -o name | grep "/ocm-${CLUSTER}-" || true)
# shellcheck disable=SC2086
kubectl --context "$SPOKE_CTX" get $entries -o yaml >"$ENTRIES_FILE"
# shellcheck disable=SC2086
kubectl --context "$SPOKE_CTX" delete $entries | cat
start=$(date +%s)
kubectl --context "$HUB_CTX" delete managedcluster "$CLUSTER" --wait=false | cat

fail=0
for _ in $(seq 120); do on_spoke || break; sleep 1; done
if on_spoke; then echo "FAIL: the ManifestWork's ConfigMap is still on the managed cluster"; fail=1
else echo "OK: the work agent removed the ManifestWork's ConfigMap after $(( $(date +%s) - start ))s"; fi

for _ in $(seq $((hold + 120))); do
  kubectl --context "$HUB_CTX" get managedcluster "$CLUSTER" >/dev/null 2>&1 || break
  sleep 1
done
took=$(( $(date +%s) - start ))
if kubectl --context "$HUB_CTX" get managedcluster "$CLUSTER" >/dev/null 2>&1; then
  echo "FAIL: the cluster is still there after ${took}s, finalizers:" \
    "$(kubectl --context "$HUB_CTX" get managedcluster "$CLUSTER" -o jsonpath='{.metadata.finalizers}')"
  kubectl --context "$HUB_CTX" -n "$CLUSTER" get manifestworks 2>&1 | cat
  exit 1
fi
if (( took < hold )); then echo "FAIL: the cluster was gone after ${took}s, before the guard's ${hold}s"; fail=1
else echo "OK: the cluster was held and is gone after ${took}s (guard ${hold}s)"; fi

sleep 60
if kubectl --context "$HUB_CTX" get managedcluster "$CLUSTER" >/dev/null 2>&1; then
  echo "FAIL: the cluster was created again"; fail=1
else echo "OK: not created again 60s later"; fi
echo "the agent's refusals during the delete:"
kubectl --context "$SPOKE_CTX" -n "${AGENT_NAMESPACE:-open-cluster-management-agent}" logs deploy/"${AGENT_DEPLOYMENT:-klusterlet-agent}" \
  --since=$(( $(date +%s) - start + 10 ))s 2>/dev/null | grep -o "access denied: cluster ${CLUSTER} is being deleted[^\"]*" |
  sed -E 's/ from spiffe:[^ ]*//' | sort | uniq -c | cut -c1-200 || true
exit $fail
