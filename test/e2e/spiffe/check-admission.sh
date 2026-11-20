#!/usr/bin/env bash
# The klusterlet operator's admission enforcement on the managed cluster, after install-ocm.sh:
# - the operator has the klusterlet-spiffe ClusterRole, which join installs with a spiffe Klusterlet;
# - the enforcement join was given (SPIFFE_ADMISSION_ENFORCEMENT) is in place, and a Job in the
#   agent namespace is refused;
# - on 1.30 or later, switching to the other enforcement and back keeps the Job refused throughout,
#   puts the new objects in place before the old ones go, and doesn't restart the agents;
# - on older clusters, asking for ValidatingAdmissionPolicy is refused as an unsupported Kubernetes
#   version (Applied=False, UnsupportedKubernetesVersion), and the webhook stays in place.
set -euo pipefail
source "$(dirname "$0")/env.sh"
KLUSTERLET="${KLUSTERLET:-klusterlet}"
AGENT_NAMESPACE="${AGENT_NAMESPACE:-open-cluster-management-agent}"
k() { kubectl --context "$SPOKE_CTX" "$@"; }
jp() { k get klusterlet "$KLUSTERLET" -o jsonpath="$1"; }
cond() { jp "{.status.conditions[?(@.type==\"$1\")].$2}"; }
fail() { echo "FAIL: $*" >&2; exit 1; }
wait_for() { # wait_for <description> <command...>
  local what=$1; shift
  for _ in $(seq 60); do "$@" && return 0; sleep 5; done
  fail "$what"
}
recorded() { [[ "$(jp '{.status.spiffe.admissionEnforcement}')" == "$1" && "$(cond SPIFFEAdmissionReady status)" == True ]]; }
count() { k get "$1" -o name 2>/dev/null | grep -c "ocm-spiffe-.*$KLUSTERLET\|ocm-spiffe-addon-namespace-label" || true; }
agent_pods() { k -n "$AGENT_NAMESPACE" get pods -o jsonpath='{.items[*].metadata.uid}'; }
job_refused() { # job_refused <enforcement>
  local out
  if out=$(k -n "$AGENT_NAMESPACE" create job spiffe-e2e-probe --image=busybox -- true 2>&1); then
    k -n "$AGENT_NAMESPACE" delete job spiffe-e2e-probe --wait=false >/dev/null
    fail "$1: a Job in $AGENT_NAMESPACE was admitted"
  fi
  grep -q "not allowed in OCM agent namespaces" <<<"$out" || fail "$1: the Job was refused for another reason: $out"
  echo "$1: a Job in $AGENT_NAMESPACE is refused: ${out##*: }"
}
set_enforcement() { # set_enforcement <value>
  k patch klusterlet "$KLUSTERLET" --type merge -p \
    "{\"spec\":{\"registrationConfiguration\":{\"registrationDriver\":{\"spiffe\":{\"admissionEnforcement\":\"$1\"}}}}}" >/dev/null
}
policies_gone() { [[ $(count validatingadmissionpolicies) == 0 ]]; }
webhooks_gone() { [[ $(count validatingwebhookconfigurations) == 0 ]]; }
in_place() { # in_place <enforcement>: its three objects exist and the other's are gone
  if [[ $1 == ValidatingAdmissionPolicy ]]; then
    [[ $(count validatingadmissionpolicies) == 3 ]] && webhooks_gone
  else
    [[ $(count validatingwebhookconfigurations) == 3 ]] && { ! served || policies_gone; }
  fi
}
served() { k get --raw /apis/admissionregistration.k8s.io/v1 | jq -e '.resources[] | select(.name == "validatingadmissionpolicies")' >/dev/null; }
other() { [[ $1 == ValidatingAdmissionPolicy ]] && echo ValidatingWebhook || echo ValidatingAdmissionPolicy; }
show_webhook() { echo "the webhook serves as $(jp '{.status.spiffe.webhookSPIFFEID}') at $(jp '{.status.spiffe.webhookDNSName}')"; }

echo "== managed cluster: Kubernetes $(k version -o json 2>/dev/null | jq -r .serverVersion.gitVersion)"
k get clusterrole klusterlet-spiffe >/dev/null || fail "no klusterlet-spiffe ClusterRole"
k get clusterrolebinding klusterlet-spiffe >/dev/null || fail "no klusterlet-spiffe ClusterRoleBinding"
echo "the operator has the klusterlet-spiffe ClusterRole"

start=$(jp '{.spec.registrationConfiguration.registrationDriver.spiffe.admissionEnforcement}')
[[ "$start" == "${SPIFFE_ADMISSION_ENFORCEMENT:-$start}" ]] || fail "admissionEnforcement $start, joined with $SPIFFE_ADMISSION_ENFORCEMENT"
echo "== admissionEnforcement $start"
wait_for "$start on record" recorded "$start"
wait_for "$start in place" in_place "$start"
[[ $start == ValidatingWebhook ]] && show_webhook
k -n "$AGENT_NAMESPACE" get deployments
job_refused "$start"
pods=$(agent_pods)

if served; then
  next=$(other "$start")
  echo "-- the cluster serves ValidatingAdmissionPolicy: switch to $next"
  set_enforcement "$next"
  wait_for "$next on record" recorded "$next"
  wait_for "$next in place, $start removed" in_place "$next"
  [[ $next == ValidatingWebhook ]] && show_webhook
  job_refused "$next"
  echo "-- back to $start"
  set_enforcement "$start"
  wait_for "$start on record" recorded "$start"
  wait_for "$start in place, $next removed" in_place "$start"
  job_refused "$start"
  [[ "$(agent_pods)" == "$pods" ]] || fail "the agents were restarted by the switches"
  echo "the agents kept running through both switches"
else
  [[ $start == ValidatingWebhook ]] || fail "admissionEnforcement $start on a cluster without the policy API"
  echo "-- ValidatingAdmissionPolicy on a cluster without the API: refused as an unsupported version"
  set_enforcement ValidatingAdmissionPolicy
  wait_for "Applied=False/UnsupportedKubernetesVersion" \
    bash -c "[[ \"\$(kubectl --context $SPOKE_CTX get klusterlet $KLUSTERLET -o jsonpath='{.status.conditions[?(@.type==\"Applied\")].reason}')\" == UnsupportedKubernetesVersion ]]"
  echo "Applied: $(cond Applied status) $(cond Applied reason): $(cond Applied message)"
  job_refused ValidatingWebhook
  echo "-- back to ValidatingWebhook"
  set_enforcement ValidatingWebhook
  wait_for "Applied=True" bash -c "[[ \"\$(kubectl --context $SPOKE_CTX get klusterlet $KLUSTERLET -o jsonpath='{.status.conditions[?(@.type==\"Applied\")].status}')\" == True ]]"
  recorded ValidatingWebhook || fail "enforcement $(jp '{.status.spiffe.admissionEnforcement}') after going back"
  [[ "$(agent_pods)" == "$pods" ]] || fail "the agents were restarted"
fi
echo "PASS: check-admission"
