#!/usr/bin/env bash
# On a managed cluster older than Kubernetes 1.30, with ValidatingWebhook enforcement: asking for
# ValidatingAdmissionPolicy is refused as an unsupported Kubernetes version, and nothing changes.
# - clusteradm join with --spiffe-admission-enforcement ValidatingAdmissionPolicy stops before it
#   applies anything;
# - setting it on the Klusterlet: Applied=False with reason UnsupportedKubernetesVersion naming the
#   version, the operator logs the error, and the webhook stays in place, on record and enforcing,
#   with the same agents running and the cluster available.
# It leaves the Klusterlet asking for ValidatingAdmissionPolicy; check-enforcement.sh
# ValidatingWebhook moves it back.
set -euo pipefail
source "$(dirname "$0")/env.sh"
CLUSTERADM="${CLUSTERADM:-$SPIFFE_E2E_DIR/../../../_output/clusteradm}"
KLUSTERLET="${KLUSTERLET:-klusterlet}"
CLUSTER="${CLUSTER:-cluster1}"
AGENT_NAMESPACE="${AGENT_NAMESPACE:-open-cluster-management-agent}"
k() { kubectl --context "$SPOKE_CTX" "$@"; }
jp() { k get klusterlet "$KLUSTERLET" -o jsonpath="$1"; }
cond() { jp "{.status.conditions[?(@.type==\"$1\")].$2}"; }
fail() { echo "FAIL: $*" >&2; exit 1; }
agent_pods() { k -n "$AGENT_NAMESPACE" get pods -o jsonpath='{range .items[*]}{.metadata.name}={.metadata.uid} {end}'; }
webhooks() { k get validatingwebhookconfigurations -o jsonpath='{range .items[*]}{.metadata.name}={.metadata.uid} {end}' | tr ' ' '\n' | grep ocm-spiffe | sort | tr '\n' ' '; }

version=$(k version -o json 2>/dev/null | jq -r .serverVersion.gitVersion)
echo "== Kubernetes $version"
if k get --raw /apis/admissionregistration.k8s.io/v1 | jq -e '.resources[] | select(.name == "validatingadmissionpolicies")' >/dev/null; then
  fail "the cluster serves ValidatingAdmissionPolicy: this check is for clusters older than 1.30"
fi
[[ "$(jp '{.status.spiffe.admissionEnforcement}')" == ValidatingWebhook ]] || fail "not on the webhook"
pods=$(agent_pods) configs=$(webhooks)
echo "agents: $pods"
echo "webhook configurations: $configs"

echo "-- clusteradm join --spiffe-admission-enforcement ValidatingAdmissionPolicy"
spec_before=$(jp '{.metadata.generation}')
if out=$("$CLUSTERADM" --context "$SPOKE_CTX" join --cluster-name "$CLUSTER" --registration-auth spiffe \
  --hub-grpc-endpoint 127.0.0.1:1 --hub-spiffe-id spiffe://ocm-hub.example.org/ocm/hub/grpc-server \
  --spiffe-admission-enforcement ValidatingAdmissionPolicy 2>&1); then
  fail "join accepted ValidatingAdmissionPolicy: $out"
fi
grep -q "unsupported Kubernetes version $version" <<<"$out" || fail "join failed for another reason: $out"
echo "join refused: $(grep -o "unsupported Kubernetes version.*" <<<"$out")"
[[ "$(jp '{.metadata.generation}')" == "$spec_before" ]] || fail "join changed the Klusterlet"
echo "join applied nothing"

echo "-- the Klusterlet set to ValidatingAdmissionPolicy"
k patch klusterlet "$KLUSTERLET" --type merge -p \
  '{"spec":{"registrationConfiguration":{"registrationDriver":{"spiffe":{"admissionEnforcement":"ValidatingAdmissionPolicy"}}}}}' >/dev/null
for _ in $(seq 60); do [[ "$(cond Applied reason)" == UnsupportedKubernetesVersion ]] && break; sleep 5; done
[[ "$(cond Applied status)" == False && "$(cond Applied reason)" == UnsupportedKubernetesVersion ]] ||
  fail "Applied $(cond Applied status)/$(cond Applied reason)"
[[ "$(cond Applied message)" == "unsupported Kubernetes version $version:"* ]] || fail "message: $(cond Applied message)"
echo "Applied: $(cond Applied status) $(cond Applied reason): $(cond Applied message)"
logged=""
for _ in $(seq 24); do
  logs=$(k -n open-cluster-management logs deploy/klusterlet --since=30m)
  logged=$(grep -m1 "unsupported Kubernetes version" <<<"$logs" || true)
  [[ -n "$logged" ]] && break
  sleep 5
done
[[ -n "$logged" ]] || fail "the operator logged no error"
echo "$logged"

echo "-- nothing changed"
[[ "$(jp '{.status.spiffe.admissionEnforcement}')" == ValidatingWebhook ]] ||
  fail "the record moved to $(jp '{.status.spiffe.admissionEnforcement}')"
[[ "$(webhooks)" == "$configs" ]] || fail "the webhook configurations changed: $(webhooks)"
out=$(k -n "$AGENT_NAMESPACE" create job spiffe-e2e-probe --image=busybox -- true 2>&1) && fail "a Job was admitted"
grep -q "admission webhook.*not allowed in OCM agent namespaces" <<<"$out" || fail "the Job was refused for another reason: $out"
echo "a Job is still refused by the webhook: ${out##*: }"
[[ "$(agent_pods)" == "$pods" ]] || fail "agents were restarted: $(agent_pods)"
kubectl --context "$HUB_CTX" wait --for=condition=ManagedClusterConditionAvailable "managedcluster/$CLUSTER" --timeout 1m | cat
echo "the webhook stays on record and in place, the agents keep running, the cluster stays available"
echo "PASS: check-unsupported"
