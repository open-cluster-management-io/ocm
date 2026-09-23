#!/bin/bash

cd -- "$(dirname -- "${BASH_SOURCE[0]}")"

set -e

hubctx="${1:-kind-hub}"
cluster="${2:-cluster1}"
version="${3:-v0.0.10}"
crd_url="https://raw.githubusercontent.com/kluster-manager/fluxcd-addon/${version}/crds/fluxcd.open-cluster-management.io_fluxcdconfigs.yaml"

kubectl config use-context "${hubctx}"

echo "Installing the FluxCDConfig CRD (${version})"
kubectl apply -f "${crd_url}"

echo "Installing the fluxcd-addon manager (${version}) on the hub"
kubectl apply -f manifests/manager
kubectl -n fluxcd-addon rollout status deployment/fluxcd-addon-manager --timeout=120s

echo "Enabling fluxcd-addon on ${cluster}"
clusteradm addon enable --names fluxcd-addon --namespace flux-system --clusters "${cluster}" --context "${hubctx}"
kubectl wait managedclusteraddon/fluxcd-addon -n "${cluster}" --for=condition=Available --timeout=180s

echo "Deploying the HelmRepository + HelmRelease (recipe) to ${cluster}"
clusteradm create work flux-helmrelease-demo -f manifests/release --cluster "${cluster}" --overwrite
kubectl wait manifestwork/flux-helmrelease-demo -n "${cluster}" --for=condition=Available --timeout=180s

echo "Done. Verify with:"
echo "  kubectl --context <spoke-context> -n flux-system get pods,helmrepository,helmrelease"
