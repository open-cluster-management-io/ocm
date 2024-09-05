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

kind create cluster --name "${hub}" --image kindest/node:v1.29.0@sha256:eaa1450915475849a73a9227b8f201df25e55e268e5d619312131292e324d570
kind create cluster --name "${c1}" --image kindest/node:v1.29.0@sha256:eaa1450915475849a73a9227b8f201df25e55e268e5d619312131292e324d570
kind create cluster --name "${c2}" --image kindest/node:v1.29.0@sha256:eaa1450915475849a73a9227b8f201df25e55e268e5d619312131292e324d570
kind create cluster --name "${c3}" --image kindest/node:v1.29.0@sha256:eaa1450915475849a73a9227b8f201df25e55e268e5d619312131292e324d570

echo "Initialize the ocm hub cluster"

clusteradm init --feature-gates="ManifestWorkReplicaSet=true,ManagedClusterAutoApproval=true" --bundle-version="latest" --wait --context ${hubctx}
joincmd=$(clusteradm get token --context ${hubctx} | grep clusteradm)

echo "Join cluster1 to hub"
$(echo ${joincmd} --force-internal-endpoint-lookup --wait --context ${c1ctx} | sed "s/<cluster_name>/$c1/g")

echo "Join cluster2 to hub"
$(echo ${joincmd} --force-internal-endpoint-lookup --wait --context ${c2ctx} | sed "s/<cluster_name>/$c2/g")

echo "Join cluster3 to hub"
$(echo ${joincmd} --force-internal-endpoint-lookup --wait --context ${c3ctx} | sed "s/<cluster_name>/$c3/g")

echo "Accept join of cluster1 and cluster2"
clusteradm accept --context ${hubctx} --clusters ${c1},${c2},${c3} --wait

kubectl get managedclusters --all-namespaces --context ${hubctx}

echo "Install Kueue (this can be replaced with OCM Manifestwork in the future)"
kubectl apply --server-side -f https://github.com/kubernetes-sigs/kueue/releases/download/v0.7.1/manifests.yaml --context ${hubctx}
kubectl apply --server-side -f https://github.com/kubernetes-sigs/kueue/releases/download/v0.7.1/manifests.yaml --context ${c1ctx}
kubectl apply --server-side -f https://github.com/kubernetes-sigs/kueue/releases/download/v0.7.1/manifests.yaml --context ${c2ctx}
kubectl apply --server-side -f https://github.com/kubernetes-sigs/kueue/releases/download/v0.7.1/manifests.yaml --context ${c3ctx}

echo "Install Jobset for MultiKueue (this can be replaced with OCM Manifestwork in the future)"
kubectl apply --server-side -f https://github.com/kubernetes-sigs/jobset/releases/download/v0.5.2/manifests.yaml --context ${hubctx}
kubectl apply --server-side -f https://github.com/kubernetes-sigs/jobset/releases/download/v0.5.2/manifests.yaml --context ${c1ctx}
kubectl apply --server-side -f https://github.com/kubernetes-sigs/jobset/releases/download/v0.5.2/manifests.yaml --context ${c2ctx}
kubectl apply --server-side -f https://github.com/kubernetes-sigs/jobset/releases/download/v0.5.2/manifests.yaml --context ${c3ctx}

kubectl config use-context ${hubctx}

echo "Patch permission"
kubectl patch clusterrole cluster-manager --type='json' -p "$(cat env/patch-clusterrole.json)"

echo "Patch image"
kubectl patch deployment cluster-manager -n open-cluster-management --type=json -p='[
  {"op": "replace", "path": "/spec/template/spec/containers/0/image", "value": "quay.io/haoqing/registration-operator:latest"},
  {"op": "replace", "path": "/spec/template/spec/containers/0/imagePullPolicy", "value": "Always"}
]'
kubectl patch clustermanager cluster-manager --type=json -p='[{"op": "replace", "path": "/spec/registrationImagePullSpec", "value": "quay.io/haoqing/registration:latest"}]'
kubectl patch clustermanager cluster-manager --type=json -p='[{"op": "replace", "path": "/spec/placementImagePullSpec", "value": "quay.io/haoqing/placement:latest"}]'

echo "Install CRDs"
kubectl create -f env/multicluster.x-k8s.io_clusterprofiles.yaml

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

echo "Enable MultiKueue on the hub"
kubectl patch deployment kueue-controller-manager -n kueue-system --type='json' -p='[{"op": "replace", "path": "/spec/template/spec/containers/0/args", "value": ["--config=/controller_manager_config.yaml", "--zap-log-level=2", "--feature-gates=MultiKueue=true"]}]' 

echo "Setup queue on the spoke"
kubectl apply -f env/single-clusterqueue-setup-mwrs.yaml

echo "Setup credentials for clusterprofile"
kubectl apply -f env/cp-c1.yaml
kubectl apply -f env/cp-c2.yaml
kubectl apply -f env/cp-c3.yaml
kubectl apply -f env/msa-c1.yaml
kubectl apply -f env/msa-c2.yaml
kubectl apply -f env/msa-c3.yaml

echo "Setup faked GPU on the spoke"
kubectl label managedcluster cluster2 accelerator=nvidia-tesla-t4
kubectl label managedcluster cluster3 accelerator=nvidia-tesla-t4

echo "IMPORTANT: RUN BELOW COMMAND MANUALLY on cluster2 and cluster3 !!!"
echo "kubectl edit-status node cluster2-control-plane --context ${c2ctx}" with nvidia.com/gpu: "3"
echo "kubectl edit-status node cluster3-control-plane --context ${c3ctx}" with nvidia.com/gpu: "3"
