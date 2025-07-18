#!/bin/bash

cd $(dirname ${BASH_SOURCE})

set -euo pipefail

# Parse command line arguments
FORCE=false
while [[ $# -gt 0 ]]; do
  case $1 in
    --force)
      FORCE=true
      shift
      ;;
    *)
      echo "Unknown option: $1"
      echo "Usage: $0 [--force]"
      exit 1
      ;;
  esac
done

hub=${HUB:-local-cluster}
c1=${CLUSTER1:-cluster1}
c2=${CLUSTER2:-cluster2}
c3=${CLUSTER3:-cluster3}

hubctx="kind-${hub}"
c1ctx="kind-${c1}"
c2ctx="kind-${c2}"
c3ctx="kind-${c3}"

spoke_clusters=(${c1} ${c2} ${c3})
all_clusters=(${hub} ${spoke_clusters[@]})
spoke_ctx=(${c1ctx} ${c2ctx} ${c3ctx})
all_ctx=(${hubctx} ${spoke_ctx[@]})

kueue_manifest="https://github.com/kubernetes-sigs/kueue/releases/download/v0.9.1/manifests.yaml"
jobset_manifest="https://github.com/kubernetes-sigs/jobset/releases/download/v0.7.1/manifests.yaml"
mpi_operator_manifest="https://github.com/kubeflow/mpi-operator/releases/download/v0.6.0/mpi-operator.yaml"
training_operator_kustomize="github.com/kubeflow/training-operator.git/manifests/overlays/standalone?ref=v1.8.1"

# Function to create kind clusters
create_clusters() {
  if [[ "$FORCE" == "true" ]]; then
    echo "Deleting existing clusters due to --force flag..."
    for cluster in "${all_clusters[@]}"; do
      kind delete cluster --name "$cluster" || true
    done
  fi

  echo "Prepare kind clusters"
  for cluster in "${all_clusters[@]}"; do
    kind create cluster --name "$cluster" --image kindest/node:v1.29.0 || true
  done
}

# Function to setup OCM
setup_ocm() {
  echo "Initialize the ocm hub cluster"
  clusteradm init --wait --context ${hubctx}
  joincmd=$(clusteradm get token --context ${hubctx} | grep clusteradm)

  echo "Join clusters to hub"
  eval "${joincmd//<cluster_name>/${hub}} --force-internal-endpoint-lookup --wait --context ${hubctx}"
  eval "${joincmd//<cluster_name>/${c1}} --force-internal-endpoint-lookup --wait --context ${c1ctx}"
  eval "${joincmd//<cluster_name>/${c2}} --force-internal-endpoint-lookup --wait --context ${c2ctx}"
  eval "${joincmd//<cluster_name>/${c3}} --force-internal-endpoint-lookup --wait --context ${c3ctx}"

  echo "Accept join of clusters"
  clusteradm accept --context ${hubctx} --clusters ${hub},${c1},${c2},${c3} --wait

  # label local-cluster
  kubectl label managedclusters ${hub} local-cluster=true --context ${hubctx}
  kubectl get managedclusters --all-namespaces --context ${hubctx}
}

# Function to install Kueue, jobset, workflow
install_kueue() {
  for ctx in "${all_ctx[@]}"; do
      echo "Install Kueue, Jobset on $ctx"
      kubectl apply --server-side -f "$kueue_manifest" --context "$ctx"
      echo "waiting for kueue-system pods to be ready"
      kubectl wait --for=condition=Ready pods --all -n kueue-system --timeout=300s --context "$ctx"
      kubectl apply --server-side -f "$jobset_manifest" --context "$ctx"
  done

  for ctx in "${spoke_ctx[@]}"; do
      echo "Install Kubeflow MPI Operator, Training Operator on $ctx"
      kubectl apply --server-side -f "$mpi_operator_manifest" --context "$ctx" || true
      kubectl apply --server-side -k "$training_operator_kustomize" --context "$ctx" || true
  done
}

# Function to install OCM addons
install_ocm_addons() {
  kubectl config use-context ${hubctx}

  echo "Add ocm helm repo"
  helm repo add ocm https://open-cluster-management.io/helm-charts/
  helm repo update

  echo "Install managed-serviceaccount"
  helm upgrade --install \
     -n open-cluster-management-addon --create-namespace \
     managed-serviceaccount ocm/managed-serviceaccount \
     --set featureGates.ephemeralIdentity=true \
     --set enableAddOnDeploymentConfig=true \
     --set hubDeployMode=AddOnTemplate

  echo "Install cluster-permission"
  helm upgrade --install \
    -n open-cluster-management --create-namespace \
     cluster-permission ocm/cluster-permission \
    --set global.imageOverrides.cluster_permission=quay.io/open-cluster-management/cluster-permission:latest

  echo "Install kueue-addon"
  helm upgrade --install \
      -n open-cluster-management-addon --create-namespace \
      kueue-addon ocm/kueue-addon \
      --set skipClusterSetBinding=true

  echo "Install resource-usage-collect-addon"
  git clone https://github.com/open-cluster-management-io/addon-contrib.git || true
  cd addon-contrib/resource-usage-collect-addon
  helm install resource-usage-collect-addon chart/ \
    -n open-cluster-management-addon --create-namespace \
    --set skipClusterSetBinding=true \
    --set global.image.repository=quay.io/haoqing/resource-usage-collect-addon
  cd -

  rm -rf addon-contrib
}

# Function to setup fake GPU
setup_fake_gpu() {
  echo "Setup fake GPU on the spoke clusters"
  kubectl label managedcluster cluster2 accelerator=nvidia-tesla-t4 --context ${hubctx}
  kubectl label managedcluster cluster3 accelerator=nvidia-tesla-t4 --context ${hubctx}

  kubectl patch node cluster2-control-plane --subresource=status --type='merge' --patch='{
    "status": {
      "capacity": {
        "nvidia.com/gpu": "3"
      },
      "allocatable": {
        "nvidia.com/gpu": "3"
      }
    }
  }' --context ${c2ctx}

  kubectl patch node cluster3-control-plane --subresource=status --type='merge' --patch='{
    "status": {
      "capacity": {
        "nvidia.com/gpu": "3"
      },
      "allocatable": {
        "nvidia.com/gpu": "3"
      }
    }
  }' --context ${c3ctx}

  echo "Fake GPU resources added successfully to cluster2 and cluster3!"
}

# Main execution
create_clusters
setup_ocm
install_kueue
install_ocm_addons
setup_fake_gpu