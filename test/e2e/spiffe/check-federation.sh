#!/usr/bin/env bash
# Joins the federated cluster $EAST_CLUSTER as $CLUSTER with SVIDs from $EAST_TRUST_DOMAIN, and
# checks trust domain pinning across trust domains. Run setup-federation.sh first; the hub runs
# the spiffe driver (install-ocm.sh).
#   1. the hub refuses agents from a trust domain that isn't in agentTrustDomains;
#   2. once it is listed, the cluster registers, pinned to $EAST_TRUST_DOMAIN, and is accepted;
#   3. work reaches the cluster;
#   4. SVIDs of $EAST_TRUST_DOMAIN can't act for a cluster pinned to another trust domain.
set -euo pipefail
source "$(dirname "$0")/env.sh"
CLUSTERADM="${CLUSTERADM:-$SPIFFE_E2E_DIR/../../../_output/clusteradm}"
CLUSTER="${CLUSTER:-cluster3}"
IMAGE_REGISTRY="${IMAGE_REGISTRY:-quay.io/open-cluster-management}"
IMAGE_TAG="${IMAGE_TAG:-spiffe-e2e}"
HUB_ID="spiffe://${TRUST_DOMAIN}/ocm/hub/grpc-server"
HUB_ENDPOINT="$(node_ip "$HUB_CLUSTER"):30091"
ns=open-cluster-management-agent
failed=0
pass() { echo "PASS $*"; }
fail() { echo "FAIL $*"; failed=1; }
mc() { kubectl --context "$HUB_CTX" get managedcluster "$CLUSTER" "$@"; }
pin() { mc -o jsonpath='{.metadata.annotations.open-cluster-management\.io/spiffe-trust-domain}' 2>/dev/null; }
renewed() {
  kubectl --context "$HUB_CTX" -n "$CLUSTER" get lease managed-cluster-lease -o jsonpath='{.spec.renewTime}' 2>/dev/null
}
# agent_trust_domains <json list>: sets agentTrustDomains of the ClusterManager's spiffe driver.
agent_trust_domains() {
  local i
  i=$(kubectl --context "$HUB_CTX" get clustermanager cluster-manager -o json |
    jq '.spec.registrationConfiguration.registrationDrivers | map(.authType) | index("spiffe")')
  kubectl --context "$HUB_CTX" patch clustermanager cluster-manager --type json -p \
    "[{\"op\":\"add\",\"path\":\"/spec/registrationConfiguration/registrationDrivers/$i/spiffe/agentTrustDomains\",\"value\":$1}]"
}

echo "== load the OCM images into $EAST_CLUSTER"
for i in registration work placement registration-operator addon-manager; do
  kind load docker-image --name "$EAST_CLUSTER" "$IMAGE_REGISTRY/$i:$IMAGE_TAG" >/dev/null
done

echo "== hub: the gRPC server's SVID carries the $EAST_TRUST_DOMAIN bundle; that trust domain isn't accepted yet"
kubectl --context "$HUB_CTX" patch clusterspiffeid ocm-hub-grpc-server --type merge \
  -p "{\"spec\":{\"federatesWith\":[\"$EAST_TRUST_DOMAIN\"]}}"
agent_trust_domains '[]'

echo "== $EAST_CLUSTER: join as $CLUSTER from $EAST_TRUST_DOMAIN, no token"
"$CLUSTERADM" --context "$EAST_CTX" join --cluster-name "$CLUSTER" --registration-auth spiffe \
  --hub-grpc-endpoint "$HUB_ENDPOINT" --hub-spiffe-id "$HUB_ID" --spiffe-trust-domain "$EAST_TRUST_DOMAIN" \
  --bundle-version-overrides "$SPIFFE_E2E_DIR/bundle.json" --wait
# The agents' entries also carry the hub's bundle, to verify the hub's SVID.
CLUSTER=$CLUSTER AGENT_NAMESPACE=$ns KLUSTERLET=klusterlet \
  envsubst '${CLUSTER} ${AGENT_NAMESPACE} ${KLUSTERLET}' < "$SPIFFE_E2E_DIR/entries-spoke.yaml.tmpl" |
  sed "s#^  className: .*#&\n  federatesWith: [\"$TRUST_DOMAIN\"]#" | kubectl --context "$EAST_CTX" apply -f -

echo "== 1. a trust domain the hub doesn't list is refused"
sleep 60
if mc >/dev/null 2>&1; then
  fail "1 $CLUSTER was created although $EAST_TRUST_DOMAIN isn't in agentTrustDomains"
else
  pass "1 no $CLUSTER while $EAST_TRUST_DOMAIN isn't in agentTrustDomains"
fi
kubectl --context "$HUB_CTX" -n open-cluster-management-hub logs deployment/cluster-manager-grpc-server --since=2m |
  grep -m3 "$EAST_TRUST_DOMAIN" || true

echo "== 2. listed: $CLUSTER registers, pinned to $EAST_TRUST_DOMAIN, and is accepted"
agent_trust_domains "[\"$EAST_TRUST_DOMAIN\"]"
for _ in $(seq 60); do [[ "$(pin)" == "$EAST_TRUST_DOMAIN" ]] && break; sleep 5; done
registered_by=$(mc -o jsonpath='{.metadata.annotations.open-cluster-management\.io/spiffe-registered-by}' 2>/dev/null || true)
if [[ "$(pin)" == "$EAST_TRUST_DOMAIN" && "$registered_by" == "spiffe://$EAST_TRUST_DOMAIN/ocm/cluster/$CLUSTER/agent/registration" ]]; then
  pass "2 $CLUSTER pinned to $EAST_TRUST_DOMAIN, registered by $registered_by"
else
  fail "2 $CLUSTER pin '$(pin)', registered by '$registered_by'"
fi
"$CLUSTERADM" --context "$HUB_CTX" accept --clusters "$CLUSTER" --spiffe-trust-domain "$EAST_TRUST_DOMAIN" --wait
if kubectl --context "$HUB_CTX" wait --for=condition=ManagedClusterConditionAvailable "managedcluster/$CLUSTER" --timeout 5m; then
  pass "2 $CLUSTER accepted and available"
else
  fail "2 $CLUSTER isn't available"
fi

echo "== 3. work reaches $CLUSTER"
cat <<EOF | kubectl --context "$HUB_CTX" apply -f -
apiVersion: work.open-cluster-management.io/v1
kind: ManifestWork
metadata: {name: federation-check, namespace: $CLUSTER}
spec:
  workload:
    manifests:
    - apiVersion: v1
      kind: ConfigMap
      metadata: {name: federation-check, namespace: default}
      data: {from: hub}
EOF
for _ in $(seq 40); do
  kubectl --context "$EAST_CTX" -n default get configmap federation-check >/dev/null 2>&1 && break
  sleep 5
done
if kubectl --context "$EAST_CTX" -n default get configmap federation-check >/dev/null 2>&1; then
  pass "3 the ManifestWork's ConfigMap is on $EAST_CLUSTER"
else
  fail "3 no ConfigMap on $EAST_CLUSTER"
fi

echo "== 4. pinned to another trust domain, $CLUSTER refuses $EAST_TRUST_DOMAIN SVIDs"
kubectl --context "$HUB_CTX" annotate managedcluster "$CLUSTER" --overwrite \
  "open-cluster-management.io/spiffe-trust-domain=$TRUST_DOMAIN"
before=$(renewed)
sleep 90
after=$(renewed)
# The hub returns the refusal to the agent, which logs it.
refusals=$(kubectl --context "$EAST_CTX" -n "$ns" logs deployment/klusterlet-registration-agent --since=2m |
  grep "pinned to trust domain" | grep -c "$EAST_TRUST_DOMAIN" || true)
if [[ "$before" == "$after" && "$refusals" -gt 0 ]]; then
  pass "4 the lease stayed at $before; the hub refused $refusals request(s) as pinned to $TRUST_DOMAIN"
else
  fail "4 lease $before -> $after, $refusals refusal(s) logged"
fi
kubectl --context "$HUB_CTX" annotate managedcluster "$CLUSTER" --overwrite \
  "open-cluster-management.io/spiffe-trust-domain=$EAST_TRUST_DOMAIN"
for _ in $(seq 40); do [[ "$(renewed)" != "$after" ]] && break; sleep 5; done
if [[ "$(renewed)" != "$after" ]]; then
  pass "4 the pin restored, the lease is renewed again"
else
  fail "4 the lease isn't renewed after the pin was restored"
fi

exit $failed
