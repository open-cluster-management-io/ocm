#!/usr/bin/env bash
# Creates a hub and a managed cluster in kind and installs SPIRE: one server on the hub, an agent
# on each cluster, both in trust domain $TRUST_DOMAIN. Then checks that a pod on the managed
# cluster gets an SVID. The OCM side comes in later scripts.
set -euo pipefail
source "$(dirname "$0")/env.sh"
mkdir -p "$(dirname "$KUBECONFIG")"

for c in "$HUB_CLUSTER" "$SPOKE_CLUSTER"; do
  if ! kind get clusters | grep -qx "$c"; then
    kind create cluster --name "$c" --kubeconfig "$KUBECONFIG" --wait 120s
  fi
done

# The hub's SPIRE server reaches the managed cluster's API on the kind network: it validates the
# agents' service account tokens there and watches its pods and ClusterSPIFFEIDs.
spoke_kubeconfig=$(kind get kubeconfig --name "$SPOKE_CLUSTER" --internal |
  sed "s#https://${SPOKE_CLUSTER}-control-plane:6443#https://$(node_ip "$SPOKE_CLUSTER"):6443#")

helm_spire() {
  local ctx=$1; shift
  helm --kube-context "$ctx" upgrade --install spire-crds spire-crds --repo "$SPIRE_REPO" \
    --version "$SPIRE_CRDS_VERSION" -n spire-server --create-namespace --wait
  helm --kube-context "$ctx" upgrade --install spire spire --repo "$SPIRE_REPO" \
    --version "$SPIRE_VERSION" -n spire-server --set global.spire.trustDomain="$TRUST_DOMAIN" "$@"
}

helm_spire "$HUB_CTX" -f "$SPIFFE_E2E_DIR/values-hub.yaml" \
  --set global.spire.clusterName="$HUB_CLUSTER" \
  --set-string "spire-server.kubeConfigs.${SPOKE_CLUSTER}.kubeConfigBase64=$(echo "$spoke_kubeconfig" | base64 -w0)" \
  --wait --timeout 5m

server_port=$(kubectl --context "$HUB_CTX" -n spire-server get service spire-server \
  -o jsonpath='{.spec.ports[?(@.name=="grpc")].nodePort}')
helm_spire "$SPOKE_CTX" -f "$SPIFFE_E2E_DIR/values-spoke.yaml" \
  --set global.spire.clusterName="$SPOKE_CLUSTER" \
  --set "spire-agent.server.address=$(node_ip "$HUB_CLUSTER")" \
  --set "spire-agent.server.port=${server_port}"

echo "Waiting for the managed cluster's SPIRE agent (the hub publishes its trust bundle)..."
kubectl --context "$SPOKE_CTX" -n spire-server rollout status daemonset/spire-agent --timeout 5m
kubectl --context "$SPOKE_CTX" -n spire-server rollout status daemonset/spire-spiffe-csi-driver --timeout 5m

"$SPIFFE_E2E_DIR/smoke.sh"
