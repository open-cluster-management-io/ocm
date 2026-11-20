#!/usr/bin/env bash
# SVID rotation under load: the agents' SVIDs live $TTL; a ManifestWork is updated every $EVERY
# for $DURATION, and every update must reach the managed cluster. The hub closes each connection
# when its SVID expires; the agent reconnects with the rotated SVID (a 5-10s gap, then a resync).
set -euo pipefail
source "$(dirname "$0")/env.sh"
CLUSTER="${CLUSTER:-cluster1}" TTL="${TTL:-2m}" EVERY="${EVERY:-20}" DURATION="${DURATION:-300}"

for e in registration work klusterlet; do
  kubectl --context "$SPOKE_CTX" patch clusterspiffeid "ocm-${CLUSTER}-$e" --type merge -p "{\"spec\":{\"ttl\":\"$TTL\"}}" | cat
done
# Restart the agents so they start on short-lived SVIDs. Deleting the pods: only the operator may
# change the agent Deployments (a rollout restart is refused by the admission policy).
kubectl --context "$SPOKE_CTX" -n open-cluster-management-agent delete pods --all | cat
kubectl --context "$SPOKE_CTX" -n open-cluster-management-agent rollout status deployment --timeout 5m | cat
wait_agents_lead

start=$(date +%s) i=0 failed=0 last_ok=0
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
    if [[ "$got" == "$i" ]]; then ok=true; last_ok=$(date +%s); break; fi
    sleep 1
  done
  $ok || { failed=$((failed + 1)); echo "update $i didn't reach the managed cluster within 30s"; }
  sleep "$EVERY"
done
# Evidence of rotation: the hub closed this cluster's agent connections at SVID expiry during the
# run, and updates still arrived after the first such close.
expiries=$(kubectl --context "$HUB_CTX" -n open-cluster-management-hub logs deploy/cluster-manager-grpc-server \
  --timestamps --since-time="$(date -u -d "@$start" +%Y-%m-%dT%H:%M:%SZ)" |
  grep "closing connection of spiffe://[^ ]*/cluster/${CLUSTER}/.*SVID expired" || true)
closed=$(grep -c . <<<"$expiries" || true)
first_close=0
[[ -n "$expiries" ]] && first_close=$(date -d "$(head -1 <<<"$expiries" | cut -d' ' -f1)" +%s)
echo "updates: $i, missed: $failed, connections closed at SVID expiry: $closed"
kubectl --context "$HUB_CTX" -n "$CLUSTER" delete manifestwork rotation | cat
(( failed == 0 )) || exit 1
if [[ -z "$expiries" ]]; then
  echo "FAIL: no SVID of ${CLUSTER}'s agents expired during the run (TTL $TTL, ${DURATION}s); rotation wasn't exercised"; exit 1
fi
if (( last_ok <= first_close )); then
  echo "FAIL: no update arrived after the first connection closed at SVID expiry"; exit 1
fi
echo "OK: the agents rotated their SVIDs and every update arrived, also after the rotation"
