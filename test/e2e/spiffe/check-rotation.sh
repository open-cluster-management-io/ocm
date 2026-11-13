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
    got=$(kubectl --context "$SPOKE_CTX" -n default get cm rotation -o jsonpath='{.data.seq}' 2>/dev/null | cat)
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
