#!/bin/bash
set -e

hub=${HUB_NAME:-hub2}
cluster=${CLUSTER_NAME:-cluster1}

hubctx="kind-${hub}"
clusterctx="kind-${cluster}"

trap 'rm -f import.yaml' EXIT

kind create cluster --name "${hub}"

kubectl config use "${hubctx}"
echo "Initialize the ocm hub cluster"
joincmd=$(clusteradm init --use-bootstrap-token | grep "clusteradm join")
kubectl wait --for=condition=HubRegistrationDegraded=false clustermanager cluster-manager --timeout=180s

kubectl config use "${clusterctx}"
echo "Join ${cluster} to ${hub}"
$(echo "${joincmd}" --singleton --force-internal-endpoint-lookup --dry-run --output-file import.yaml | sed "s/<cluster_name>/${cluster}/g")
kubectl create ns "open-cluster-management-agent-${hub}"
sed '/^apiVersion:/i\
---' import.yaml | sed "s/open-cluster-management-agent/open-cluster-management-agent-${hub}/g" | sed "s/name: \"klusterlet\"/name: \"klusterlet-${hub}\"/g" | kubectl apply -f -

kubectl config use "${hubctx}"
echo "Accept join of ${cluster} on ${hub}"
clusteradm accept --clusters "${cluster}" --wait

kubectl get managedclusters
