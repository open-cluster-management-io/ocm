#!/bin/bash

cd $(dirname ${BASH_SOURCE})

set -e

hubctx="${1:-kind-hub}"
cluster="${2:-cluster1}"

kubectl config use-context ${hubctx}

echo "Installing the Flux Helm operator (source-controller + helm-controller) on ${cluster}"
clusteradm create work flux-helm-operator -f manifests/operator --cluster ${cluster} --overwrite

echo "Waiting for the operator ManifestWork to become Available"
kubectl wait manifestwork/flux-helm-operator -n ${cluster} --for=condition=Available --timeout=120s

echo "Deploying the HelmRepository + HelmRelease (recipe) to ${cluster}"
clusteradm create work flux-helmrelease-demo -f manifests/release --cluster ${cluster} --overwrite

echo "Waiting for the release ManifestWork to become Available"
kubectl wait manifestwork/flux-helmrelease-demo -n ${cluster} --for=condition=Available --timeout=180s

echo "Done. Verify with:"
echo "  kubectl --context <spoke-context> -n flux-system get pods,helmrepository,helmrelease"
echo "  kubectl --context <spoke-context> -n podinfo get all"
