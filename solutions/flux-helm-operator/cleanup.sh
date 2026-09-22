#!/bin/bash

cd -- "$(dirname -- "${BASH_SOURCE[0]}")"

set -e

hubctx="${1:-kind-hub}"
cluster="${2:-cluster1}"
version="${3:-v0.0.10}"
crd_url="https://raw.githubusercontent.com/kluster-manager/fluxcd-addon/${version}/crds/fluxcd.open-cluster-management.io_fluxcdconfigs.yaml"

kubectl config use-context "${hubctx}"

# Order matters here, see the Cleanup section in the README.
echo "Removing the HelmRelease/HelmRepository ManifestWork from ${cluster}"
kubectl -n "${cluster}" delete manifestwork flux-helmrelease-demo --ignore-not-found --wait --timeout=120s

echo "Disabling fluxcd-addon on ${cluster}"
kubectl -n "${cluster}" delete managedclusteraddon fluxcd-addon --ignore-not-found --wait --timeout=120s

echo "Removing the fluxcd-addon manager from the hub"
kubectl delete clustermanagementaddon fluxcd-addon --ignore-not-found --wait --timeout=60s
kubectl delete -f manifests/manager --ignore-not-found
kubectl delete -f "${crd_url}" --ignore-not-found
