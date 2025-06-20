#!/bin/bash

cd $(dirname ${BASH_SOURCE})

set -e

hub=${CLUSTER1:-local-cluster}
c1=${CLUSTER1:-cluster1}
c2=${CLUSTER2:-cluster2}
c3=${CLUSTER2:-cluster3}

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

# ocm setup
echo "Parepare kind clusters"
for cluster in "${all_clusters[@]}"; do
  kind create cluster --name "$cluster" --image kindest/node:v1.29.0
done

echo "Initialize the ocm hub cluster"
clusteradm init --feature-gates="ManifestWorkReplicaSet=true,ManagedClusterAutoApproval=true" --bundle-version="latest" --wait --context ${hubctx}
joincmd=$(clusteradm get token --context ${hubctx} | grep clusteradm)

echo "Join clusters to hub"
$(echo ${joincmd} --force-internal-endpoint-lookup --wait --context ${hubctx} | sed "s/<cluster_name>/$hub/g")
$(echo ${joincmd} --force-internal-endpoint-lookup --wait --context ${c1ctx} | sed "s/<cluster_name>/$c1/g")
$(echo ${joincmd} --force-internal-endpoint-lookup --wait --context ${c2ctx} | sed "s/<cluster_name>/$c2/g")
$(echo ${joincmd} --force-internal-endpoint-lookup --wait --context ${c3ctx} | sed "s/<cluster_name>/$c3/g")

echo "Accept join of clusters"
clusteradm accept --context ${hubctx} --clusters ${hub},${c1},${c2},${c3} --wait

kubectl config use-context ${hubctx}

# label local-cluster
kubectl label managedclusters ${hub} local-cluster=true
kubectl get managedclusters --all-namespaces

# install kueue, jobset, workflow
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

# install ocm addons
echo "Install managed-serviceaccount"
helm repo add ocm https://open-cluster-management.io/helm-charts/
helm repo update
helm install \
   -n open-cluster-management-addon --create-namespace \
   managed-serviceaccount ocm/managed-serviceaccount \
   --set tag=latest \
   --set featureGates.ephemeralIdentity=true \
   --set enableAddOnDeploymentConfig=true \
   --set hubDeployMode=AddOnTemplate

echo "Install cluster-permission"
git clone git@github.com:open-cluster-management-io/cluster-permission.git || true
cd cluster-permission
helm install cluster-permission chart/ \
  -n open-cluster-management --create-namespace \
  --set global.imageOverrides.cluster_permission=quay.io/open-cluster-management/cluster-permission:latest \
  --set global.pullPolicy=Always
cd -
rm -rf cluster-permission

echo "Install kueue-addon"
git clone git@github.com:open-cluster-management-io/addon-contrib.git || true
# cd addon-contrib/kueue-addon
cd /root/go/src/open-cluster-management-io/addon-contrib/kueue-addon
git checkout br_kueue-helm-chart
helm install kueue-addon charts/kueue-addon/ \
  -n open-cluster-management-addon --create-namespace \
  --set skipClusterSetBinding=true
cd -

echo "Install resource-usage-collect-addon"
cd addon-contrib/resource-usage-collect-addon
git checkout br_resource-addon
helm install resource-usage-collect-addon chart/ \
  -n open-cluster-management-addon --create-namespace \
  --set skipClusterSetBinding=true \
  --set global.image.repository=quay.io/haoqing/resource-usage-collect-addon
cd -

rm -rf addon-contrib

echo "Setup faked GPU on the spoke"
kubectl label managedcluster cluster2 accelerator=nvidia-tesla-t4
kubectl label managedcluster cluster3 accelerator=nvidia-tesla-t4

echo "IMPORTANT: RUN BELOW COMMAND MANUALLY on cluster2 and cluster3 !!!"
echo "kubectl edit-status node cluster2-control-plane --context ${c2ctx}" with nvidia.com/gpu: "3"
echo "kubectl edit-status node cluster3-control-plane --context ${c3ctx}" with nvidia.com/gpu: "3"
