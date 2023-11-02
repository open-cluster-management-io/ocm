#!/bin/bash
set -e

hub=${HUB_NAME:-hub1}
cluster=${CLUSTER_NAME:-cluster1}

hubctx="kind-${hub}"
clusterctx="kind-${cluster}"

kind create cluster --name "${hub}"
kind create cluster --name "${cluster}"

kubectl config use ${hubctx}
echo "Initialize the ocm hub cluster"
joincmd=$(clusteradm init --use-bootstrap-token | grep clusteradm)
kubectl wait --for=condition=HubRegistrationDegraded=false clustermanager cluster-manager --timeout=60s

kubectl config use ${clusterctx}
echo "Join ${cluster} to ${hub}"
$(echo ${joincmd} --singleton --force-internal-endpoint-lookup --wait | sed "s/<cluster_name>/$cluster/g")

kubectl config use ${hubctx}
echo "Accept join of ${cluster} on ${hub}"
clusteradm accept --clusters ${cluster} --wait

kubectl get managedclusters
