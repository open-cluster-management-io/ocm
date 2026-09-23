#!/usr/bin/env bash

# Deploy the Bookinfo sample across two managed clusters and verify cross-cluster traffic.
#
# Prerequisite: ./setup-mesh-addon.sh completed successfully.

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"
source ./scripts/common.sh

require_command kubectl
require_command curl

require_context "${CTX_CLUSTER1}"
require_context "${CTX_CLUSTER2}"

require_istio_crds "${CTX_CLUSTER1}"
require_istio_crds "${CTX_CLUSTER2}"

echo "==> Deploying Bookinfo workloads split across cluster1 and cluster2"

kubectl --context "${CTX_CLUSTER1}" create namespace bookinfo --dry-run=client -o yaml | \
  kubectl --context "${CTX_CLUSTER1}" apply -f -
kubectl --context "${CTX_CLUSTER1}" label namespace bookinfo istio.io/rev="${ISTIO_REVISION}" --overwrite
kubectl --context "${CTX_CLUSTER1}" apply -n bookinfo -f "${BOOKINFO_MANIFEST_URL}" -l 'app,version notin (v3)'
kubectl --context "${CTX_CLUSTER1}" apply -n bookinfo -f "${BOOKINFO_MANIFEST_URL}" -l 'account'

kubectl --context "${CTX_CLUSTER2}" create namespace bookinfo --dry-run=client -o yaml | \
  kubectl --context "${CTX_CLUSTER2}" apply -f -
kubectl --context "${CTX_CLUSTER2}" label namespace bookinfo istio.io/rev="${ISTIO_REVISION}" --overwrite
kubectl --context "${CTX_CLUSTER2}" apply -n bookinfo -f "${BOOKINFO_MANIFEST_URL}" -l 'app,version in (v3)'
kubectl --context "${CTX_CLUSTER2}" apply -n bookinfo -f "${BOOKINFO_MANIFEST_URL}" -l 'service=reviews'
kubectl --context "${CTX_CLUSTER2}" apply -n bookinfo -f "${BOOKINFO_MANIFEST_URL}" -l 'account=reviews'
kubectl --context "${CTX_CLUSTER2}" apply -n bookinfo -f "${BOOKINFO_MANIFEST_URL}" -l 'app=ratings'
kubectl --context "${CTX_CLUSTER2}" apply -n bookinfo -f "${BOOKINFO_MANIFEST_URL}" -l 'account=ratings'

wait_for_bookinfo_pods "${CTX_CLUSTER1}"
wait_for_bookinfo_pods "${CTX_CLUSTER2}"

echo "==> Ensuring Istio sidecars are injected (restart if Istio became ready after first deploy)"
ensure_bookinfo_sidecars "${CTX_CLUSTER1}"
ensure_bookinfo_sidecars "${CTX_CLUSTER2}"

# Wait for any pod churn to settle before capturing IPs
echo "==> Waiting for pods to stabilize before configuring routing"
sleep 5
wait_for_bookinfo_pods "${CTX_CLUSTER1}"
wait_for_bookinfo_pods "${CTX_CLUSTER2}"

# Configure routing with fresh pod IPs (must be after all restarts complete)
configure_cross_cluster_routing

# Sanity check: confirm ServiceEntry address matches current pod IP
current_pod_ip="$(get_reviews_v3_pod_ip)"
serviceentry_ip="$(kubectl --context "${CTX_CLUSTER2}" -n istio-system get serviceentry \
  reviews.bookinfo.svc.cluster2.global -o jsonpath='{.spec.endpoints[0].address}' 2>/dev/null || true)"
if [[ "${current_pod_ip}" != "${serviceentry_ip}" ]]; then
  echo "warning: ServiceEntry IP (${serviceentry_ip}) does not match current pod IP (${current_pod_ip}). Updating..." >&2
  configure_cross_cluster_routing
fi

echo "==> Verifying productpage routes traffic to reviews-v3 in cluster2"
productpage_port="${PRODUCTPAGE_PORT:-19080}"
reviews_v3_hits=0

# Get reviews-v3 pod name for sidecar stats verification
reviews_v3_pod="$(kubectl --context "${CTX_CLUSTER2}" -n bookinfo get pod \
  -l app=reviews,version=v3 --field-selector=status.phase=Running \
  -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"

# Capture baseline sidecar connection count before traffic test
stats_before=0
if [[ -n "${reviews_v3_pod}" ]]; then
  stats_before="$(kubectl --context "${CTX_CLUSTER2}" -n bookinfo exec "${reviews_v3_pod}" \
    -c istio-proxy -- pilot-agent request GET stats 2>/dev/null | \
    awk -F: '/server\.total_connections/{print $2}' || echo 0)"
  stats_before="${stats_before:-0}"
fi

if lsof -i ":${productpage_port}" -sTCP:LISTEN >/dev/null 2>&1; then
  echo "error: port ${productpage_port} is already in use. Set PRODUCTPAGE_PORT to another value." >&2
  exit 1
fi

kubectl --context "${CTX_CLUSTER1}" -n bookinfo port-forward svc/productpage "${productpage_port}:9080" >/dev/null 2>&1 &
port_forward_pid=$!
trap 'kill "${port_forward_pid}" 2>/dev/null || true' EXIT
sleep 3

if ! kill -0 "${port_forward_pid}" 2>/dev/null; then
  echo "error: port-forward to productpage failed on port ${productpage_port}." >&2
  exit 1
fi

for _ in $(seq 1 30); do
  # Bookinfo registers /productpage without a trailing slash; /productpage/ returns 404.
  body="$(curl -sf "http://127.0.0.1:${productpage_port}/productpage" || true)"
  # reviews-v3 uses <font color="red"> in Bookinfo HTML (not CSS color: red).
  if echo "${body}" | grep -Eqi 'reviews-v3-|color[[:space:]]*=[[:space:]]*"red"'; then
    reviews_v3_hits=$((reviews_v3_hits + 1))
  fi
done

# Capture sidecar connection count after traffic test (before killing port-forward)
stats_after=0
if [[ -n "${reviews_v3_pod}" ]]; then
  stats_after="$(kubectl --context "${CTX_CLUSTER2}" -n bookinfo exec "${reviews_v3_pod}" \
    -c istio-proxy -- pilot-agent request GET stats 2>/dev/null | \
    awk -F: '/server\.total_connections/{print $2}' || echo 0)"
  stats_after="${stats_after:-0}"
fi

kill "${port_forward_pid}" 2>/dev/null || true
trap - EXIT

if [[ "${reviews_v3_hits}" -lt 1 ]]; then
  echo "error: expected at least one request routed to reviews-v3 (red stars) but got ${reviews_v3_hits}/30" >&2
  echo "hint: inspect VirtualService, ServiceEntry, and east-west gateway resources on both clusters." >&2
  exit 1
fi

echo "success: observed ${reviews_v3_hits}/30 requests routed to reviews-v3 in cluster2"

# Cross-verify that traffic actually flowed through Istio mesh (not bypassing the service mesh)
echo "==> Cross-verifying Istio mesh routing via reviews-v3 sidecar stats"

if [[ -z "${reviews_v3_pod}" ]]; then
  echo "warning: could not find reviews-v3 pod on cluster2; skipping mesh routing verification." >&2
else
  # Calculate delta in total connections to reviews-v3 sidecar during traffic test.
  # If traffic routed through the Istio mesh, new connections will appear here.
  new_connections=$((stats_after - stats_before))
  
  if [[ "${new_connections}" -gt 0 ]]; then
    echo "success: reviews-v3 sidecar received ${new_connections} new connection(s) via Istio mesh during traffic test"
  else
    echo "warning: no new connections recorded in reviews-v3 sidecar (before=${stats_before}, after=${stats_after})." >&2
    echo "hint: verify with: kubectl --context ${CTX_CLUSTER2} -n bookinfo exec ${reviews_v3_pod} -c istio-proxy -- pilot-agent request GET stats | grep total_connections" >&2
  fi
fi

echo
echo "Optional browser check:"
echo "  kubectl --context ${CTX_CLUSTER1} -n bookinfo port-forward svc/productpage --address 0.0.0.0 9080:9080"
echo "  open http://localhost:9080/productpage and refresh to see red stars from reviews-v3"
