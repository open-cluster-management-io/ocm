#!/bin/bash
set -e

hub=${HUB_NAME:-hub2}
hosting=${HOSTING_CLUSTER_NAME:-hosting}
cluster=${CLUSTER_NAME:-cluster1}

hubctx="kind-${hub}"
hostingctx="kind-${hosting}"
clusterctx="kind-${cluster}"

kind create cluster --name "${hub}"
kind create cluster --name "${hosting}"

kubectl config use ${hubctx}
echo "Initialize the ocm hub cluster"
joincmd=$(clusteradm init --use-bootstrap-token | grep clusteradm)
kubectl wait --for=condition=HubRegistrationDegraded=false clustermanager cluster-manager --timeout=60s

kubectl config use ${clusterctx}
kubectl config view --flatten --minify > kubeconfig.${cluster}

kubectl config use ${hostingctx}
echo "Join ${cluster} to ${hub}"
$(echo ${joincmd} --singleton --force-internal-endpoint-lookup --mode hosted --force-internal-endpoint-lookup-managed --managed-cluster-kubeconfig kubeconfig.${cluster} --wait | sed "s/<cluster_name>/$cluster/g")
rm kubeconfig.${cluster}

kubectl config use ${hubctx}
echo "Accept join of ${cluster} on ${hub}"
clusteradm accept --clusters ${cluster} --wait

kubectl get managedclusters
