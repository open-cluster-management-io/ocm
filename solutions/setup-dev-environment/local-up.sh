#!/bin/bash

cd $(dirname ${BASH_SOURCE})

set -e

hub=${CLUSTER1:-hub}
c1=${CLUSTER1:-cluster1}
c2=${CLUSTER2:-cluster2}

hubctx="kind-${hub}"
c1ctx="kind-${c1}"
c2ctx="kind-${c2}"

kind create cluster --name "${hub}"
kind create cluster --name "${c1}"
kind create cluster --name "${c2}"

kubectl config use ${hubctx}
echo "Initialize the ocm hub cluster"
joincmd=$(clusteradm init --use-bootstrap-token | grep clusteradm)

kubectl config use ${c1ctx}
echo "Join cluster1 to hub"
$(echo ${joincmd} --force-internal-endpoint-lookup --wait | sed "s/<cluster_name>/$c1/g")

kubectl config use ${c2ctx}
echo "Join cluster2 to hub"
$(echo ${joincmd} --force-internal-endpoint-lookup --wait | sed "s/<cluster_name>/$c2/g")

kubectl config use ${hubctx}
echo "Accept join of cluster1 and cluster2"
clusteradm accept --clusters ${c1},${c2} --wait

kubectl get managedclusters --all-namespaces
