#!/usr/bin/env bash
# On a managed cluster older than Kubernetes 1.30: starts with ValidatingAdmissionPolicy enforcement
# and checks it is refused, then moves to ValidatingWebhook and finishes the join.
# - clusteradm join with --spiffe-admission-enforcement ValidatingAdmissionPolicy stops before it
#   applies anything;
# - a Klusterlet created with ValidatingAdmissionPolicy anyway (rendered by clusteradm join
#   --dry-run and applied with kubectl): the operator logs the error, sets Applied=False with
#   reason UnsupportedKubernetesVersion naming the version, and deploys no agent;
# - set to ValidatingWebhook: the webhook is put in place, the agents start, and the cluster joins.
set -euo pipefail
source "$(dirname "$0")/env.sh"
CLUSTERADM="${CLUSTERADM:-$SPIFFE_E2E_DIR/../../../_output/clusteradm}"
CLUSTER="${CLUSTER:-cluster1}"
KLUSTERLET=klusterlet
AGENT_NAMESPACE=open-cluster-management-agent
HUB_ID="spiffe://${TRUST_DOMAIN}/ocm/hub/grpc-server"
HUB_ENDPOINT="$(node_ip "$HUB_CLUSTER"):30091"
k() { kubectl --context "$SPOKE_CTX" "$@"; }
jp() { k get klusterlet "$KLUSTERLET" -o jsonpath="$1"; }
cond() { jp "{.status.conditions[?(@.type==\"$1\")].$2}"; }
fail() { echo "FAIL: $*" >&2; exit 1; }
join() { # join <enforcement> [extra flags]
  local enforcement=$1; shift
  "$CLUSTERADM" --context "$SPOKE_CTX" "$@" join --cluster-name "$CLUSTER" --registration-auth spiffe \
    --hub-grpc-endpoint "$HUB_ENDPOINT" --hub-spiffe-id "$HUB_ID" \
    --bundle-version-overrides "$SPIFFE_E2E_DIR/bundle.json" --spiffe-admission-enforcement "$enforcement"
}

version=$(k version -o json 2>/dev/null | jq -r .serverVersion.gitVersion)
if k get --raw /apis/admissionregistration.k8s.io/v1 | jq -e '.resources[] | select(.name == "validatingadmissionpolicies")' >/dev/null; then
  fail "Kubernetes $version serves ValidatingAdmissionPolicy: this is for clusters older than 1.30"
fi

echo "== hub: the gRPC server's SPIRE entry, then the hub"
kubectl --context "$HUB_CTX" apply -f "$SPIFFE_E2E_DIR/entries-hub.yaml"
"$CLUSTERADM" --context "$HUB_CTX" init --registration-drivers spiffe --spiffe-trust-domain "$TRUST_DOMAIN" \
  --spiffe-endpoint-type hostname --spiffe-server "$(node_ip "$HUB_CLUSTER")" \
  --bundle-version-overrides "$SPIFFE_E2E_DIR/bundle.json" --wait
kubectl --context "$HUB_CTX" apply -f "$SPIFFE_E2E_DIR/hub-nodeport.yaml"
kubectl --context "$HUB_CTX" -n open-cluster-management-hub rollout status deployment/cluster-manager-grpc-server --timeout 5m

echo "== managed cluster ($version): the operator's SPIRE entry, for its admission webhook"
CLUSTER=$CLUSTER envsubst '${CLUSTER}' < "$SPIFFE_E2E_DIR/entries-operator.yaml.tmpl" | k apply -f -

echo "== clusteradm join --spiffe-admission-enforcement ValidatingAdmissionPolicy"
if out=$(join ValidatingAdmissionPolicy 2>&1); then fail "join accepted ValidatingAdmissionPolicy: $out"; fi
grep -q "unsupported Kubernetes version $version" <<<"$out" || fail "join failed for another reason: $out"
echo "join refused: $(grep -o "unsupported Kubernetes version.*" <<<"$out")"
! k get klusterlet "$KLUSTERLET" >/dev/null 2>&1 || fail "join created the Klusterlet"
! k get namespace open-cluster-management >/dev/null 2>&1 || fail "join applied the operator"
echo "join applied nothing"

echo "== the Klusterlet created with ValidatingAdmissionPolicy anyway"
rendered=$(mktemp)
join ValidatingWebhook --dry-run --output-file "$rendered" >/dev/null
grep -q "admissionEnforcement: ValidatingWebhook" "$rendered" || fail "no admissionEnforcement in the rendered join"
sed -i 's/admissionEnforcement: ValidatingWebhook/admissionEnforcement: ValidatingAdmissionPolicy/' "$rendered"
# clusteradm writes the objects one after another, without document separators: a new object
# starts where a top-level apiVersion or kind repeats.
python3 - "$rendered" <<'PY'
import sys
lines, out, seen = open(sys.argv[1]).read().splitlines(), [], set()
for line in lines:
    key = line.split(":", 1)[0] if line[:1].isalpha() else None
    if key in ("apiVersion", "kind"):
        if key in seen:
            out.append("---")
            seen = set()
        seen.add(key)
    out.append(line)
open(sys.argv[1], "w").write("\n".join(out) + "\n")
PY
# The CRD first, then the rest once the API server serves it.
k apply -f "$rendered" >/dev/null 2>&1 || true
k wait --for=condition=Established crd/klusterlets.operator.open-cluster-management.io --timeout 2m
k apply -f "$rendered" | grep -E "klusterlet|namespace" | cat
rm -f "$rendered"
for _ in $(seq 60); do [[ "$(cond Applied reason 2>/dev/null)" == UnsupportedKubernetesVersion ]] && break; sleep 5; done
[[ "$(cond Applied status)" == False && "$(cond Applied reason)" == UnsupportedKubernetesVersion ]] ||
  fail "Applied $(cond Applied status)/$(cond Applied reason): $(cond Applied message)"
[[ "$(cond Applied message)" == "unsupported Kubernetes version $version:"* ]] || fail "message: $(cond Applied message)"
echo "Applied: $(cond Applied status) $(cond Applied reason): $(cond Applied message)"
logged=""
for _ in $(seq 24); do
  logged=$(k -n open-cluster-management logs deploy/klusterlet | grep -m1 "unsupported Kubernetes version" || true)
  [[ -n "$logged" ]] && break
  sleep 5
done
[[ -n "$logged" ]] || fail "the operator logged no error"
echo "the operator logged: $logged"
[[ -z "$(k -n "$AGENT_NAMESPACE" get deployments -o name 2>/dev/null)" ]] || fail "agents were deployed: $(k -n "$AGENT_NAMESPACE" get deployments -o name)"
[[ -z "$(k get validatingwebhookconfigurations -o name | grep ocm-spiffe || true)" ]] || fail "webhook configurations were created"
echo "no agent and no admission object were deployed"

echo "== admissionEnforcement ValidatingWebhook"
k patch klusterlet "$KLUSTERLET" --type merge -p \
  '{"spec":{"registrationConfiguration":{"registrationDriver":{"spiffe":{"admissionEnforcement":"ValidatingWebhook"}}}}}' >/dev/null
for _ in $(seq 60); do [[ "$(cond SPIFFEAdmissionReady status 2>/dev/null)" == True ]] && break; sleep 5; done
[[ "$(cond SPIFFEAdmissionReady status)" == True ]] || fail "SPIFFEAdmissionReady $(cond SPIFFEAdmissionReady reason): $(cond SPIFFEAdmissionReady message)"
for _ in $(seq 24); do [[ "$(cond Applied status)" == True ]] && break; sleep 5; done
[[ "$(cond Applied status)" == True ]] || fail "Applied $(cond Applied reason): $(cond Applied message)"
echo "SPIFFEAdmissionReady True, Applied True, on record: $(jp '{.status.spiffe.admissionEnforcement}')"

echo "== managed cluster: the agents' SPIRE entries, now that the Klusterlet is SPIFFEAdmissionReady"
CLUSTER=$CLUSTER AGENT_NAMESPACE=$AGENT_NAMESPACE KLUSTERLET=$KLUSTERLET \
  envsubst '${CLUSTER} ${AGENT_NAMESPACE} ${KLUSTERLET}' < "$SPIFFE_E2E_DIR/entries-spoke.yaml.tmpl" | k apply -f -

echo "== hub: accept"
"$CLUSTERADM" --context "$HUB_CTX" accept --clusters "$CLUSTER" --wait
kubectl --context "$HUB_CTX" wait --for=condition=ManagedClusterConditionAvailable "managedcluster/$CLUSTER" --timeout 5m
kubectl --context "$HUB_CTX" get managedcluster "$CLUSTER"
echo "PASS: install-ocm-policy-first"
