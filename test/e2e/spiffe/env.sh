# Shared settings of the SPIFFE e2e scripts. Everything uses its own kubeconfig, so the
# caller's kubectl context and other kind clusters are left alone.
SPIFFE_E2E_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
export TRUST_DOMAIN="${TRUST_DOMAIN:-ocm-hub.example.org}"
export HUB_CLUSTER="${HUB_CLUSTER:-ocm-e2e-hub}"
export SPOKE_CLUSTER="${SPOKE_CLUSTER:-ocm-e2e-spoke}"
export KUBECONFIG="${SPIFFE_E2E_KUBECONFIG:-${SPIFFE_E2E_DIR}/../../../_output/spiffe-e2e.kubeconfig}"
export HUB_CTX="kind-${HUB_CLUSTER}"
export SPOKE_CTX="kind-${SPOKE_CLUSTER}"
SPIRE_REPO="https://spiffe.github.io/helm-charts-hardened/"
SPIRE_CRDS_VERSION=0.6.1
SPIRE_VERSION=0.30.4
SPIRE_CLASS=spire-server-spire

# node_ip prints a kind node's address on the kind network.
node_ip() {
  docker inspect -f '{{.NetworkSettings.Networks.kind.IPAddress}}' "$1-control-plane"
}
