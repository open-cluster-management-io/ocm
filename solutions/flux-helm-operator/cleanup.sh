#!/bin/bash

cd -- "$(dirname -- "${BASH_SOURCE[0]}")"

set -e

hubctx="${1:-kind-hub}"
cluster="${2:-cluster1}"

kubectl config use-context ${hubctx}

# Order matters: delete the recipe (HelmRelease/HelmRepository) first and wait
# for it to fully clear before removing the operator. helm-controller needs to
# be alive to run `helm uninstall` and release its finalizer on the HelmRelease;
# deleting both ManifestWorks at once leaves that finalizer orphaned and the
# ManifestWork/CRD deletion deadlocked.
echo "Removing the HelmRelease/HelmRepository ManifestWork from ${cluster}"
kubectl -n ${cluster} delete manifestwork flux-helmrelease-demo --ignore-not-found --wait --timeout=120s

echo "Removing the Flux operator ManifestWork from ${cluster}"
kubectl -n ${cluster} delete manifestwork flux-helm-operator --ignore-not-found --wait --timeout=120s
