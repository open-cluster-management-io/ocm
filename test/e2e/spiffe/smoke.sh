#!/usr/bin/env bash
# Checks that a pod on the managed cluster that looks like a work agent gets its SVID. It uses a
# cluster name, namespace and SPIRE entries of its own (cluster "smoke", namespace spiffe-smoke),
# so it never touches an installed Klusterlet's agent namespace or entries, and it removes what it
# created however it ends.
set -euo pipefail
source "$(dirname "$0")/env.sh"
cluster=smoke ns=spiffe-smoke
k() { kubectl --context "$SPOKE_CTX" "$@"; }
entries() { CLUSTER=$cluster AGENT_NAMESPACE=$ns KLUSTERLET=$cluster envsubst '${CLUSTER} ${AGENT_NAMESPACE} ${KLUSTERLET}' \
  < "$SPIFFE_E2E_DIR/entries-spoke.yaml.tmpl"; }
cleanup() {
  local status=$?
  k delete namespace "$ns" --ignore-not-found --wait=false >/dev/null || true
  entries | k delete -f - --ignore-not-found >/dev/null || true
  exit $status
}
trap cleanup EXIT

k create namespace "$ns" --dry-run=client -o yaml | k apply -f -
k -n "$ns" create serviceaccount klusterlet-work-sa --dry-run=client -o yaml | k apply -f -
entries | k apply -f -
k -n "$ns" delete pod svid-check --ignore-not-found
sed "s/namespace: open-cluster-management-agent/namespace: $ns/" "$SPIFFE_E2E_DIR/svid-check-pod.yaml" | k apply -f -

want="spiffe://${TRUST_DOMAIN}/ocm/cluster/${cluster}/agent/work"
for _ in $(seq 60); do
  phase=$(k -n "$ns" get pod svid-check -o jsonpath='{.status.phase}')
  if [[ "$phase" == Succeeded ]]; then break; fi
  sleep 5
done
out=$(k -n "$ns" logs svid-check)
echo "$out"

if ! grep -q "SPIFFE ID:.*${want}" <<<"$out"; then
  echo "FAIL: the pod didn't get ${want}" >&2
  exit 1
fi
echo "OK: the managed cluster's pod got ${want}"
