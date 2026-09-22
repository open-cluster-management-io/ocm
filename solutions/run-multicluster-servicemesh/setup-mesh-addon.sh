#!/usr/bin/env bash

# Install the multicluster-mesh addon, deploy Istio meshes, and federate them.
#
# Prerequisite: OCM hub + managed clusters created via ../setup-dev-environment/local-up.sh

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"
source ./scripts/common.sh

require_command kubectl
require_command helm
require_command docker
require_command git

require_context "${CTX_HUB}"
require_context "${CTX_CLUSTER1}"
require_context "${CTX_CLUSTER2}"

images=(
  "docker.io/istio/operator:${ISTIO_VERSION}"
  "docker.io/istio/pilot:${ISTIO_VERSION}"
  "docker.io/istio/proxyv2:${ISTIO_VERSION}"
  "docker.io/istio/examples-bookinfo-productpage-v1:1.16.2"
  "docker.io/istio/examples-bookinfo-details-v1:1.16.2"
  "docker.io/istio/examples-bookinfo-ratings-v1:1.16.2"
  "docker.io/istio/examples-bookinfo-reviews-v1:1.16.2"
  "docker.io/istio/examples-bookinfo-reviews-v2:1.16.2"
  "docker.io/istio/examples-bookinfo-reviews-v3:1.16.2"
)

for cluster in "${CLUSTER1}" "${CLUSTER2}"; do
  preload_images_into_kind "${cluster}" "${images[@]}"
done

echo "==> Installing multicluster-mesh addon from ${MULTICLUSTER_MESH_REPO} (${MULTICLUSTER_MESH_REF})"
if [[ ! -d "${MULTICLUSTER_MESH_DIR}/.git" ]]; then
  git clone --depth 1 --branch "${MULTICLUSTER_MESH_REF}" "${MULTICLUSTER_MESH_REPO}" "${MULTICLUSTER_MESH_DIR}"
fi

helm upgrade --install "${ADDON_RELEASE}" "${MULTICLUSTER_MESH_DIR}/charts/multicluster-mesh" \
  --namespace "${ADDON_NAMESPACE}" \
  --create-namespace \
  --kube-context "${CTX_HUB}"

echo "==> Waiting for multicluster-mesh addon to become available on managed clusters"
wait_for_managedclusteraddons

echo "==> Deploying Istio control planes via MeshDeployment"
kubectl --context "${CTX_HUB}" apply -f "${MANIFESTS_DIR}/meshdeployment.yaml"

echo "==> Waiting for Istio control planes on managed clusters"
wait_for_istio_control_plane "${CTX_CLUSTER1}"
wait_for_istio_control_plane "${CTX_CLUSTER2}"

echo "==> Federating service meshes via MeshFederation"
kubectl --context "${CTX_HUB}" apply -f "${MANIFESTS_DIR}/meshfederation.yaml"

echo "==> Waiting for east-west gateways (created by mesh federation)"
wait_for_eastwest_gateway "${CTX_CLUSTER1}"
wait_for_eastwest_gateway "${CTX_CLUSTER2}"

echo
echo "Multicluster service mesh addon setup complete."
echo "Next: run ./verify-bookinfo-traffic.sh to deploy Bookinfo and verify cross-cluster routing."
