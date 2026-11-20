#!/usr/bin/env bash
# Installs the hub with the spiffe driver only, and joins the managed cluster as $CLUSTER in
# $MODE (Default or Singleton) with no token: it follows the order the proposal requires.
# SPIFFE_ADMISSION_ENFORCEMENT is --spiffe-admission-enforcement: ValidatingAdmissionPolicy (the
# default here; Kubernetes 1.30 or later) or ValidatingWebhook.
set -euo pipefail
source "$(dirname "$0")/env.sh"
CLUSTERADM="${CLUSTERADM:-$SPIFFE_E2E_DIR/../../../_output/clusteradm}"
CLUSTER="${CLUSTER:-cluster1}"
MODE="${MODE:-Default}"
HUB_ID="spiffe://${TRUST_DOMAIN}/ocm/hub/grpc-server"
HUB_ENDPOINT="$(node_ip "$HUB_CLUSTER"):30091"

echo "== hub: the gRPC server's SPIRE entry, then the hub"
kubectl --context "$HUB_CTX" apply -f "$SPIFFE_E2E_DIR/entries-hub.yaml"
"$CLUSTERADM" --context "$HUB_CTX" init --registration-drivers spiffe --spiffe-trust-domain "$TRUST_DOMAIN" \
  --spiffe-endpoint-type hostname --spiffe-server "$(node_ip "$HUB_CLUSTER")" \
  --bundle-version-overrides "$SPIFFE_E2E_DIR/bundle.json" --wait
kubectl --context "$HUB_CTX" apply -f "$SPIFFE_E2E_DIR/hub-nodeport.yaml"
kubectl --context "$HUB_CTX" -n open-cluster-management-hub rollout status deployment/cluster-manager-grpc-server --timeout 5m

echo "== managed cluster: the operator's SPIRE entry, for its admission webhook"
CLUSTER=$CLUSTER envsubst '${CLUSTER}' < "$SPIFFE_E2E_DIR/entries-operator.yaml.tmpl" |
  kubectl --context "$SPOKE_CTX" apply -f -

echo "== managed cluster: join with no token ($MODE)"
singleton=()
[[ "$MODE" == Singleton ]] && singleton=(--singleton)
"$CLUSTERADM" --context "$SPOKE_CTX" join --cluster-name "$CLUSTER" --registration-auth spiffe \
  --hub-grpc-endpoint "$HUB_ENDPOINT" --hub-spiffe-id "$HUB_ID" \
  --bundle-version-overrides "$SPIFFE_E2E_DIR/bundle.json" "${singleton[@]}" \
  --spiffe-admission-enforcement "${SPIFFE_ADMISSION_ENFORCEMENT:-ValidatingAdmissionPolicy}" --wait

echo "== managed cluster: the agents' SPIRE entries, now that the Klusterlet is SPIFFEAdmissionReady"
CLUSTER=$CLUSTER AGENT_NAMESPACE=open-cluster-management-agent KLUSTERLET=klusterlet \
  envsubst '${CLUSTER} ${AGENT_NAMESPACE} ${KLUSTERLET}' < "$SPIFFE_E2E_DIR/entries-spoke.yaml.tmpl" |
  kubectl --context "$SPOKE_CTX" apply -f -

echo "== hub: accept"
"$CLUSTERADM" --context "$HUB_CTX" accept --clusters "$CLUSTER" --wait
kubectl --context "$HUB_CTX" wait --for=condition=ManagedClusterConditionAvailable "managedcluster/$CLUSTER" --timeout 5m
kubectl --context "$HUB_CTX" get managedcluster "$CLUSTER"
