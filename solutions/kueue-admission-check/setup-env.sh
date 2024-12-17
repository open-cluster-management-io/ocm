#!/bin/bash

cd $(dirname ${BASH_SOURCE})

set -e

hub=${CLUSTER1:-hub}
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

echo "Initialize the ocm hub cluster with ClusterProfile enabled"
clusteradm init --feature-gates="ManifestWorkReplicaSet=true,ManagedClusterAutoApproval=true,ClusterProfile=true" --bundle-version="v0.15.0" --wait --context ${hubctx}
joincmd=$(clusteradm get token --context ${hubctx} | grep clusteradm)

echo "Join clusters to hub"
$(echo ${joincmd} --force-internal-endpoint-lookup --wait --context ${c1ctx} | sed "s/<cluster_name>/$c1/g")
$(echo ${joincmd} --force-internal-endpoint-lookup --wait --context ${c2ctx} | sed "s/<cluster_name>/$c2/g")
$(echo ${joincmd} --force-internal-endpoint-lookup --wait --context ${c3ctx} | sed "s/<cluster_name>/$c3/g")

echo "Accept join of clusters"
clusteradm accept --context ${hubctx} --clusters ${c1},${c2},${c3} --wait

kubectl get managedclusters --all-namespaces --context ${hubctx}

# install kueue, jobset, workflow
for ctx in "${all_ctx[@]}"; do
    echo "Install Kueue, Jobset on $ctx"
    kubectl apply --server-side -f "$kueue_manifest" --context "$ctx"
    kubectl apply --server-side -f "$jobset_manifest" --context "$ctx"
done

for ctx in "${spoke_ctx[@]}"; do
    echo "Install Kubeflow MPI Operator, Training Operator on $ctx"
    kubectl apply --server-side -f "$mpi_operator_manifest" --context "$ctx" || true
    kubectl apply --server-side -k "$training_operator_kustomize" --context "$ctx" || true
done

kubectl config use-context ${hubctx}
# patch some ocm resoures and images
echo "Patch permission"
kubectl patch clusterrole cluster-manager --type='json' -p "$(cat env/patch-clusterrole.json)"

echo "Patch image"
# quay.io/haoqing/registration-operator:kueue-v0.9.1 grants more permission for registration and placement.
# quay.io/haoqing/registration-operator:kueue-v0.9.1 creates worker’s kubeconfig secret for multikueue.
# quay.io/haoqing/placement:kueue-v0.9.1 implements the admission check controller.
# The source code is in repo https://github.com/haoqing0110/OCM/tree/br_ocm-v0.15.1-kueue-v0.9.1.
kubectl patch deployment cluster-manager -n open-cluster-management --type=json -p='[
  {"op": "replace", "path": "/spec/template/spec/containers/0/image", "value": "quay.io/haoqing/registration-operator:kueue-v0.9.1"},
  {"op": "replace", "path": "/spec/template/spec/containers/0/imagePullPolicy", "value": "Always"}
]'
kubectl patch clustermanager cluster-manager --type=json -p='[
  {"op": "replace", "path": "/spec/registrationImagePullSpec", "value": "quay.io/haoqing/registration:kueue-v0.9.1"},
  {"op": "replace", "path": "/spec/placementImagePullSpec", "value": "quay.io/haoqing/placement:kueue-v0.9.1"}
]'

# install addons
echo "Install managed-serviceaccount"
git clone git@github.com:open-cluster-management-io/managed-serviceaccount.git || true
cd managed-serviceaccount
helm uninstall -n open-cluster-management-addon managed-serviceaccount || true
helm install \
   -n open-cluster-management-addon --create-namespace \
   managed-serviceaccount charts/managed-serviceaccount/ \
   --set tag=latest \
   --set featureGates.ephemeralIdentity=true \
   --set enableAddOnDeploymentConfig=true \
   --set hubDeployMode=AddOnTemplate
cd -
rm -rf managed-serviceaccount

echo "Install managed-serviceaccount mca"
clusteradm create clusterset spoke
clusteradm clusterset set spoke --clusters ${c1},${c2},${c3}
clusteradm clusterset bind spoke --namespace default
kubectl apply -f env/placement.yaml || true
kubectl patch clustermanagementaddon managed-serviceaccount --type='json' -p="$(cat env/patch-mg-sa-cma.json)" || true

echo "Install cluster-permission"
git clone git@github.com:open-cluster-management-io/cluster-permission.git || true
cd cluster-permission
kubectl apply -f config/crds
kubectl apply -f config/rbac
kubectl apply -f config/deploy
cd -
rm -rf cluster-permission

echo "Install resource-usage-collect-addon"
git clone git@github.com:open-cluster-management-io/addon-contrib.git || true
cd addon-contrib/resource-usage-collect-addon
make deploy
cd -
rm -rf addon-contrib

# prepare credentials for multikueue
echo "Setup queue on the spoke"
kubectl apply -f env/single-clusterqueue-setup-mwrs.yaml

echo "Setup credentials for clusterprofile"
for CLUSTER in "${spoke_clusters[@]}"; do
    sed "s/CLUSTER_NAME/$CLUSTER/g" env/clusterpermission.yaml | kubectl apply -f -
    sed "s/CLUSTER_NAME/$CLUSTER/g" env/msa.yaml | kubectl apply -f -
done

echo "Setup faked GPU on the spoke"
kubectl label managedcluster cluster2 accelerator=nvidia-tesla-t4
kubectl label managedcluster cluster3 accelerator=nvidia-tesla-t4

echo "IMPORTANT: RUN BELOW COMMAND MANUALLY on cluster2 and cluster3 !!!"
echo "kubectl edit-status node cluster2-control-plane --context ${c2ctx}" with nvidia.com/gpu: "3"
echo "kubectl edit-status node cluster3-control-plane --context ${c3ctx}" with nvidia.com/gpu: "3"
