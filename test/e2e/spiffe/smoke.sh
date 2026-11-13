#!/usr/bin/env bash
# Checks that a pod on the managed cluster that looks like cluster1's work agent gets its SVID.
set -euo pipefail
source "$(dirname "$0")/env.sh"
ns=open-cluster-management-agent

kubectl --context "$SPOKE_CTX" create namespace "$ns" --dry-run=client -o yaml | kubectl --context "$SPOKE_CTX" apply -f -
kubectl --context "$SPOKE_CTX" -n "$ns" create serviceaccount klusterlet-work-sa --dry-run=client -o yaml |
  kubectl --context "$SPOKE_CTX" apply -f -
CLUSTER=cluster1 AGENT_NAMESPACE=$ns KLUSTERLET=klusterlet envsubst '${CLUSTER} ${AGENT_NAMESPACE} ${KLUSTERLET}' \
  < "$SPIFFE_E2E_DIR/entries-spoke.yaml.tmpl" | kubectl --context "$SPOKE_CTX" apply -f -
kubectl --context "$SPOKE_CTX" -n "$ns" delete pod svid-check --ignore-not-found
kubectl --context "$SPOKE_CTX" apply -f "$SPIFFE_E2E_DIR/svid-check-pod.yaml"

want="spiffe://${TRUST_DOMAIN}/ocm/cluster/cluster1/agent/work"
for _ in $(seq 60); do
  phase=$(kubectl --context "$SPOKE_CTX" -n "$ns" get pod svid-check -o jsonpath='{.status.phase}')
  if [[ "$phase" == Succeeded ]]; then break; fi
  sleep 5
done
out=$(kubectl --context "$SPOKE_CTX" -n "$ns" logs svid-check)
echo "$out"

# Clean up: the real agents come with the Klusterlet, and its admission policies need a namespace
# the operator trusts.
kubectl --context "$SPOKE_CTX" delete namespace "$ns" --wait=false
CLUSTER=cluster1 AGENT_NAMESPACE=$ns KLUSTERLET=klusterlet envsubst '${CLUSTER} ${AGENT_NAMESPACE} ${KLUSTERLET}' \
  < "$SPIFFE_E2E_DIR/entries-spoke.yaml.tmpl" | kubectl --context "$SPOKE_CTX" delete -f - --ignore-not-found

if ! grep -q "SPIFFE ID:.*${want}" <<<"$out"; then
  echo "FAIL: the pod didn't get ${want}" >&2
  exit 1
fi
echo "OK: the managed cluster's pod got ${want}"
