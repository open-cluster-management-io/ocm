#!/usr/bin/env bash
# SVID rotation under load: the agents' SVIDs live $TTL; a ManifestWork is updated every $EVERY
# for $DURATION, and every update must reach the managed cluster. The hub closes each connection
# when its SVID expires; the agent reconnects with the rotated SVID (a 5-10s gap, then a resync).
set -euo pipefail
source "$(dirname "$0")/env.sh"
CLUSTER="${CLUSTER:-cluster1}" TTL="${TTL:-2m}" EVERY="${EVERY:-20}" DURATION="${DURATION:-300}"

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
for e in registration work klusterlet; do
  kubectl --context "$SPOKE_CTX" patch clusterspiffeid "ocm-${CLUSTER}-$e" --type merge -p "{\"spec\":{\"ttl\":\"$TTL\"}}" | cat
done
# Restart the agents so they start on short-lived SVIDs. Deleting the pods: only the operator may
# change the agent Deployments (a rollout restart is refused by the admission policy).
kubectl --context "$SPOKE_CTX" -n open-cluster-management-agent delete pods --all | cat
kubectl --context "$SPOKE_CTX" -n open-cluster-management-agent rollout status deployment --timeout 5m | cat
wait_agents_lead

start=$(date +%s) i=0 failed=0
while (( $(date +%s) - start < DURATION )); do
  i=$((i + 1))
  cat <<YAML | kubectl --context "$HUB_CTX" apply -f - >/dev/null
apiVersion: work.open-cluster-management.io/v1
kind: ManifestWork
metadata: {name: rotation, namespace: ${CLUSTER}}
spec:
  workload:
    manifests:
    - apiVersion: v1
      kind: ConfigMap
      metadata: {name: rotation, namespace: default}
      data: {seq: "$i"}
YAML
  ok=false
  for _ in $(seq 30); do
    got=$(kubectl --context "$SPOKE_CTX" -n default get cm rotation -o jsonpath='{.data.seq}' 2>/dev/null || true)
    if [[ "$got" == "$i" ]]; then ok=true; break; fi
    sleep 1
  done
  $ok || { failed=$((failed + 1)); echo "update $i didn't reach the managed cluster within 30s"; }
  sleep "$EVERY"
done
closed=$(kubectl --context "$HUB_CTX" -n open-cluster-management-hub logs deploy/cluster-manager-grpc-server --since="${DURATION}s" |
  grep -ci "expir" || true)
echo "updates: $i, missed: $failed, hub log lines about SVID expiry: $closed"
kubectl --context "$HUB_CTX" -n "$CLUSTER" delete manifestwork rotation | cat
(( failed == 0 ))
