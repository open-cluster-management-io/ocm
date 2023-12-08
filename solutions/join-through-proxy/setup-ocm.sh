#!/bin/bash
set -e

proxy=${HUB_NAME:-proxy}
hub=${HUB_NAME:-hub}
cluster1=${CLUSTER_NAME:-cluster1}
cluster2=${CLUSTER_NAME:-cluster2}

hubctx="kind-${hub}"
cluster1ctx="kind-${cluster1}"
cluster2ctx="kind-${cluster2}"

# setup hub
kind create cluster --name "${hub}"
kubectl config use ${hubctx}
echo "Initialize the ocm hub cluster"
joincmd=$(clusteradm init --use-bootstrap-token | grep clusteradm)
kubectl wait --for=condition=HubRegistrationDegraded=false clustermanager cluster-manager --timeout=60s

# register cluster1
kind create cluster --name "${cluster1}"
kubectl config use ${cluster1ctx}
echo "Join ${cluster1} to ${hub} through HTTP proxy http://${proxy}-control-plane:31280"
$(echo ${joincmd} --singleton --force-internal-endpoint-lookup --proxy-url=http://${proxy}-control-plane:31280 --wait | sed "s/<cluster_name>/$cluster1/g")

kubectl config use ${hubctx}
echo "Accept join of ${cluster1} on ${hub}"
clusteradm accept --clusters ${cluster1} --wait

kubectl get managedclusters

# register cluster2
kind create cluster --name "${cluster2}"
kubectl config use ${cluster2ctx}
echo "Join ${cluster2} to ${hub} through HTTPS proxy https://${proxy}-control-plane:31290"
$(echo ${joincmd} --singleton --force-internal-endpoint-lookup --proxy-url=https://${proxy}-control-plane:31290 --proxy-ca-file=tls/root-ca.crt --wait | sed "s/<cluster_name>/$cluster2/g")

kubectl config use ${hubctx}
echo "Accept join of ${cluster2} on ${hub}"
clusteradm accept --clusters ${cluster2} --wait

kubectl get managedclusters