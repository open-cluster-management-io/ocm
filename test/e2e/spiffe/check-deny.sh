#!/usr/bin/env bash
# Revocation by denial: with hubAcceptsClient false, the hub closes the cluster's connections and
# refuses new ones, so ManifestWork updates stop reaching the managed cluster. Accepting again
# restores it without any step on the managed cluster.
set -euo pipefail
source "$(dirname "$0")/env.sh"
CLUSTER="${CLUSTER:-cluster1}"

apply_cm() {
  cat <<YAML | kubectl --context "$HUB_CTX" apply -f - >/dev/null
apiVersion: work.open-cluster-management.io/v1
kind: ManifestWork
metadata: {name: deny, namespace: ${CLUSTER}}
spec:
  workload:
    manifests:
    - apiVersion: v1
      kind: ConfigMap
      metadata: {name: deny, namespace: default}
      data: {v: "$1"}
YAML
}
seen() { kubectl --context "$SPOKE_CTX" -n default get cm deny -o jsonpath='{.data.v}' 2>/dev/null | cat; }
wait_for() { for _ in $(seq "$2"); do [[ "$(seen)" == "$1" ]] && return 0; sleep 1; done; return 1; }

apply_cm before
wait_for before 60 || { echo "FAIL: work doesn't reach the cluster before the test"; exit 1; }

kubectl --context "$HUB_CTX" patch managedcluster "$CLUSTER" --type merge -p '{"spec":{"hubAcceptsClient":false}}' | cat
sleep 10
apply_cm denied
if wait_for denied 60; then echo "FAIL: a denied cluster still receives work"; exit 1; fi
echo "OK: the denied cluster receives no work"
kubectl --context "$HUB_CTX" -n open-cluster-management-hub logs deploy/cluster-manager-grpc-server --since=90s |
  grep -i "closed.*${CLUSTER}\|${CLUSTER}.*closed" | head -3 | cut -c1-200 || true

kubectl --context "$HUB_CTX" patch managedcluster "$CLUSTER" --type merge -p '{"spec":{"hubAcceptsClient":true}}' | cat
wait_for denied 180 || { echo "FAIL: work didn't resume after accepting again"; exit 1; }
echo "OK: accepting again resumes delivery with no step on the managed cluster"
kubectl --context "$HUB_CTX" -n "$CLUSTER" delete manifestwork deny | cat
