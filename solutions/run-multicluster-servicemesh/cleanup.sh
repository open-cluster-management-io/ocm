#!/usr/bin/env bash

# Remove Bookinfo demo resources, mesh federation/deployment, and the multicluster-mesh addon.
#
# Set DELETE_KIND_CLUSTERS=true to also delete the hub/cluster1/cluster2 KinD clusters.

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"
source ./scripts/common.sh

require_command kubectl
require_command helm

if kubectl config get-contexts -o name | grep -qx "${CTX_HUB}"; then
  echo "==> Removing Bookinfo demo and mesh CRs from hub"
  kubectl --context "${CTX_HUB}" delete -f "${MANIFESTS_DIR}/meshfederation.yaml" --ignore-not-found
  kubectl --context "${CTX_HUB}" delete -f "${MANIFESTS_DIR}/meshdeployment.yaml" --ignore-not-found
fi

for ctx in "${CTX_CLUSTER1}" "${CTX_CLUSTER2}"; do
  if kubectl config get-contexts -o name | grep -qx "${ctx}"; then
    echo "==> Removing Bookinfo and Istio routing resources from ${ctx}"
    kubectl --context "${ctx}" delete namespace bookinfo --ignore-not-found --wait=false
    kubectl --context "${ctx}" -n istio-system delete serviceentry reviews.bookinfo.svc.cluster2.global --ignore-not-found
    kubectl --context "${ctx}" -n istio-system delete destinationrule reviews-bookinfo-cluster2 --ignore-not-found
  fi
done

if helm --kube-context "${CTX_HUB}" -n "${ADDON_NAMESPACE}" status "${ADDON_RELEASE}" >/dev/null 2>&1; then
  echo "==> Uninstalling multicluster-mesh helm release"
  helm --kube-context "${CTX_HUB}" -n "${ADDON_NAMESPACE}" uninstall "${ADDON_RELEASE}"
fi

if [[ "${DELETE_KIND_CLUSTERS:-false}" == "true" ]]; then
  require_command kind
  echo "==> Deleting KinD clusters ${HUB}, ${CLUSTER1}, and ${CLUSTER2}"
  kind delete cluster --name "${HUB}"
  kind delete cluster --name "${CLUSTER1}"
  kind delete cluster --name "${CLUSTER2}"
fi

echo "Cleanup complete."
