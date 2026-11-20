# Shared settings of the SPIFFE e2e scripts. Everything uses its own kubeconfig, so the
# caller's kubectl context and other kind clusters are left alone.
SPIFFE_E2E_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
export TRUST_DOMAIN="${TRUST_DOMAIN:-ocm-hub.example.org}"
export HUB_CLUSTER="${HUB_CLUSTER:-ocm-e2e-hub}"
export SPOKE_CLUSTER="${SPOKE_CLUSTER:-ocm-e2e-spoke}"
export KUBECONFIG="${SPIFFE_E2E_KUBECONFIG:-${SPIFFE_E2E_DIR}/../../../_output/spiffe-e2e.kubeconfig}"
export HUB_CTX="kind-${HUB_CLUSTER}"
export SPOKE_CTX="kind-${SPOKE_CLUSTER}"
# The federated cluster: its own SPIRE, in another trust domain (setup-federation.sh).
export EAST_CLUSTER="${EAST_CLUSTER:-ocm-e2e-east}"
export EAST_TRUST_DOMAIN="${EAST_TRUST_DOMAIN:-east.example.org}"
export EAST_CTX="kind-${EAST_CLUSTER}"
SPIRE_REPO="https://spiffe.github.io/helm-charts-hardened/"
SPIRE_CRDS_VERSION=0.6.1
SPIRE_VERSION=0.30.4
SPIRE_CLASS=spire-server-spire

# node_ip prints a kind node's address on the kind network.
node_ip() {
  docker inspect -f '{{.NetworkSettings.Networks.kind.IPAddress}}' "$1-control-plane"
}

# work_agent prints the Deployment and leader lease of the agent that runs the work agent:
# klusterlet-agent in Singleton mode, the work agent in Default mode.
work_agent() {
  if kubectl --context "$SPOKE_CTX" -n open-cluster-management-agent get deployment klusterlet-agent >/dev/null 2>&1; then
    echo "klusterlet-agent klusterlet-agent-lock"
  else
    echo "work-agent work-agent-lock"
  fi
}

# wait_agents_lead: after the agents restart, wait until the new pod of the agent that runs the
# work agent holds its leader lease; an old pod that didn't release it makes the new one wait for
# the lease to expire.
wait_agents_lead() {
  local name lease pod holder
  read -r name lease < <(work_agent)
  for _ in $(seq 60); do
    pod=$(kubectl --context "$SPOKE_CTX" -n open-cluster-management-agent get pods -o name | grep -m1 "$name" || true)
    holder=$(kubectl --context "$SPOKE_CTX" -n open-cluster-management-agent get lease "$lease" \
      -o jsonpath='{.spec.holderIdentity}' 2>/dev/null || true)
    [[ -n "$pod" && "$holder" == "${pod#pod/}"_* ]] && return 0
    sleep 5
  done
  echo "FAIL: the new $name pod didn't become leader (lease $lease)"; exit 1
}
