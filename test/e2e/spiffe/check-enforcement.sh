#!/usr/bin/env bash
# check-enforcement.sh [<enforcement to migrate to>]
# Checks the Klusterlet's admission enforcement on the managed cluster: it is the one in its spec
# and on record, its three objects are in place and the other enforcement's are gone, a Job in the
# agent namespace is refused, the agents run, and the cluster is available on the hub.
# With an argument, it first migrates to that enforcement and also checks that the migration
# restarted no agent and ran no recovery: the rules were enforced throughout.
set -euo pipefail
source "$(dirname "$0")/env.sh"
KLUSTERLET="${KLUSTERLET:-klusterlet}"
CLUSTER="${CLUSTER:-cluster1}"
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
count() { k get "$1" -o name 2>/dev/null | grep -c "ocm-spiffe-.*$KLUSTERLET\|ocm-spiffe-addon-namespace-label" || true; }
served() { k api-resources --api-group=admissionregistration.k8s.io -o name | grep -qx validatingadmissionpolicies.admissionregistration.k8s.io; }
recorded() { [[ "$(jp '{.status.spiffe.admissionEnforcement}')" == "$1" && "$(cond SPIFFEAdmissionReady status)" == True ]]; }
in_place() { # in_place <enforcement>: its three objects exist and the other's are gone
  if [[ $1 == ValidatingAdmissionPolicy ]]; then
    [[ $(count validatingadmissionpolicies) == 3 && $(count validatingadmissionpolicybindings) == 3 &&
      $(count validatingwebhookconfigurations) == 0 ]]
  else
    [[ $(count validatingwebhookconfigurations) == 3 ]] &&
      { ! served || [[ $(count validatingadmissionpolicies) == 0 && $(count validatingadmissionpolicybindings) == 0 ]]; }
  fi
}
agent_pods() { k -n "$AGENT_NAMESPACE" get pods -o jsonpath='{range .items[*]}{.metadata.name}={.metadata.uid} {end}'; }
job_refused() {
  local out
  if out=$(k -n "$AGENT_NAMESPACE" create job spiffe-e2e-probe --image=busybox -- true 2>&1); then
    k -n "$AGENT_NAMESPACE" delete job spiffe-e2e-probe --wait=false >/dev/null
    fail "$1: a Job in $AGENT_NAMESPACE was admitted"
  fi
  grep -q "not allowed in OCM agent namespaces" <<<"$out" || fail "$1: the Job was refused for another reason: $out"
  local by=policy
  grep -q "admission webhook" <<<"$out" && by=webhook
  echo "$1: a Job in $AGENT_NAMESPACE is refused by the $by: ${out##*: }"
}
check() { # check <enforcement>
  echo "== Kubernetes $(k version -o json 2>/dev/null | jq -r .serverVersion.gitVersion), admissionEnforcement $1"
  [[ "$(jp '{.spec.registrationConfiguration.registrationDriver.spiffe.admissionEnforcement}')" == "$1" ]] ||
    fail "spec says $(jp '{.spec.registrationConfiguration.registrationDriver.spiffe.admissionEnforcement}')"
  wait_for "$1 on record and SPIFFEAdmissionReady" recorded "$1"
  wait_for "$1 in place, the other enforcement gone" in_place "$1"
  echo "on record: $(jp '{.status.spiffe.admissionEnforcement}'), SPIFFEAdmissionReady $(cond SPIFFEAdmissionReady status) ($(cond SPIFFEAdmissionReady reason))"
  if [[ $1 == ValidatingWebhook ]]; then
    k get validatingwebhookconfigurations -o name | grep ocm-spiffe
    echo "the webhook serves as $(jp '{.status.spiffe.webhookSPIFFEID}') at $(jp '{.status.spiffe.webhookDNSName}')"
  else
    k get validatingadmissionpolicies -o name | grep ocm-spiffe
  fi
  job_refused "$1"
  k -n "$AGENT_NAMESPACE" rollout status deployment --timeout 2m | cat
  [[ "$(cond Applied status)" == True ]] || fail "Applied is $(cond Applied status): $(cond Applied message)"
  kubectl --context "$HUB_CTX" wait --for=condition=ManagedClusterConditionAvailable "managedcluster/$CLUSTER" --timeout 2m | cat
}

if [[ $# -eq 0 ]]; then
  check "$(jp '{.spec.registrationConfiguration.registrationDriver.spiffe.admissionEnforcement}')"
  echo "PASS: check-enforcement"
  exit 0
fi

to=$1
from=$(jp '{.spec.registrationConfiguration.registrationDriver.spiffe.admissionEnforcement}')
[[ "$from" != "$to" ]] || fail "already $to"
before=$(agent_pods)
echo "== migrate from $from to $to; agents: $before"
k patch klusterlet "$KLUSTERLET" --type merge -p \
  "{\"spec\":{\"registrationConfiguration\":{\"registrationDriver\":{\"spiffe\":{\"admissionEnforcement\":\"$to\"}}}}}" >/dev/null
check "$to"
[[ "$(agent_pods)" == "$before" ]] || fail "the migration restarted agents: $(agent_pods)"
echo "the agents were not restarted"
if k -n open-cluster-management logs deploy/klusterlet --since=10m | grep -q "waiting for pods to go before recording"; then
  fail "the migration ran recovery"
fi
echo "no recovery ran"
echo "PASS: migrated from $from to $to"
