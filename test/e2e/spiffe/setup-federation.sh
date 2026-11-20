#!/usr/bin/env bash
# Adds the federated cluster $EAST_CLUSTER, with its own SPIRE server and agent in trust domain
# $EAST_TRUST_DOMAIN, and federates the two trust domains: each SPIRE server serves its trust
# bundle on a bundle endpoint (https_spiffe), and each trusts the other's. Run after setup.sh.
set -euo pipefail
source "$(dirname "$0")/env.sh"

if ! kind get clusters | grep -qx "$EAST_CLUSTER"; then
  kind create cluster --name "$EAST_CLUSTER" --kubeconfig "$KUBECONFIG" --wait 120s
fi

spire() {
  local ctx=$1 td=$2; shift 2
  helm --kube-context "$ctx" upgrade --install spire-crds spire-crds --repo "$SPIRE_REPO" \
    --version "$SPIRE_CRDS_VERSION" -n spire-server --create-namespace --wait
  helm --kube-context "$ctx" upgrade --install spire spire --repo "$SPIRE_REPO" \
    --version "$SPIRE_VERSION" -n spire-server --set global.spire.trustDomain="$td" "$@"
}

echo "== $EAST_CLUSTER: SPIRE server and agent in $EAST_TRUST_DOMAIN"
spire "$EAST_CTX" "$EAST_TRUST_DOMAIN" -f "$SPIFFE_E2E_DIR/values-east.yaml" \
  --set global.spire.clusterName="$EAST_CLUSTER" --wait --timeout 5m

echo "== $HUB_CLUSTER: turn on the hub server's bundle endpoint"
spoke_kubeconfig=$(kind get kubeconfig --name "$SPOKE_CLUSTER" --internal |
  sed "s#https://${SPOKE_CLUSTER}-control-plane:6443#https://$(node_ip "$SPOKE_CLUSTER"):6443#")
spire "$HUB_CTX" "$TRUST_DOMAIN" -f "$SPIFFE_E2E_DIR/values-hub.yaml" \
  --set global.spire.clusterName="$HUB_CLUSTER" \
  --set-string "spire-server.kubeConfigs.${SPOKE_CLUSTER}.kubeConfigBase64=$(echo "$spoke_kubeconfig" | base64 -w0)" \
  --wait --timeout 5m

# federate <from-ctx> <from-cluster> <from-td> <to-ctx>: makes the server of <to-ctx> trust the
# trust domain <from-td>, reading its bundle from the bundle endpoint of <from-ctx>.
federate() {
  local from_ctx=$1 from_cluster=$2 from_td=$3 to_ctx=$4
  local port bundle
  port=$(kubectl --context "$from_ctx" -n spire-server get service spire-server \
    -o jsonpath='{.spec.ports[?(@.name=="federation")].nodePort}')
  bundle=$(kubectl --context "$from_ctx" -n spire-server exec spire-server-0 -c spire-server -- \
    /opt/spire/bin/spire-server bundle show -format spiffe)
  cat <<EOF | kubectl --context "$to_ctx" apply -f -
apiVersion: spire.spiffe.io/v1alpha1
kind: ClusterFederatedTrustDomain
metadata:
  name: ${from_td//./-}
spec:
  className: $SPIRE_CLASS
  trustDomain: $from_td
  bundleEndpointURL: https://$(node_ip "$from_cluster"):$port
  bundleEndpointProfile:
    type: https_spiffe
    endpointSPIFFEID: spiffe://$from_td/spire/server
  trustDomainBundle: |-
$(echo "$bundle" | sed 's/^/    /')
EOF
}

echo "== federate $TRUST_DOMAIN and $EAST_TRUST_DOMAIN"
federate "$HUB_CTX" "$HUB_CLUSTER" "$TRUST_DOMAIN" "$EAST_CTX"
federate "$EAST_CTX" "$EAST_CLUSTER" "$EAST_TRUST_DOMAIN" "$HUB_CTX"

# Each server lists the other trust domain once it has fetched its bundle.
for side in "$HUB_CTX:$EAST_TRUST_DOMAIN" "$EAST_CTX:$TRUST_DOMAIN"; do
  ctx=${side%%:*} td=${side#*:}
  for _ in $(seq 60); do
    if kubectl --context "$ctx" -n spire-server exec spire-server-0 -c spire-server -- \
      /opt/spire/bin/spire-server bundle list -id "spiffe://$td" >/dev/null 2>&1; then
      echo "$ctx trusts $td"
      continue 2
    fi
    sleep 5
  done
  echo "$ctx doesn't trust $td" >&2
  exit 1
done
