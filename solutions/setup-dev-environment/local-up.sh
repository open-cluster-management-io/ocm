#!/bin/bash

cd $(dirname ${BASH_SOURCE})

set -e

hub=${HUB:-hub}
c1=${CLUSTER1:-cluster1}
c2=${CLUSTER2:-cluster2}
local_cluster=local-cluster

hubctx="kind-${hub}"
c1ctx="kind-${c1}"
c2ctx="kind-${c2}"

kind create cluster --name "${hub}"
kind create cluster --name "${c1}"
kind create cluster --name "${c2}"

echo -e "Initialize the ocm hub cluster\n"
clusteradm init --wait --context ${hubctx}
joincmd=$(clusteradm get token --context ${hubctx} | grep clusteradm)

echo -e "Join cluster1 to hub\n"
$(echo ${joincmd} --force-internal-endpoint-lookup --wait --context ${c1ctx} | sed "s/<cluster_name>/$c1/g")

echo -e "Join cluster2 to hub\n"
$(echo ${joincmd} --force-internal-endpoint-lookup --wait --context ${c2ctx} | sed "s/<cluster_name>/$c2/g")

echo -e "Join hub to itself as ${local_cluster}\n"
$(echo ${joincmd} --force-internal-endpoint-lookup --wait --context ${hubctx} | sed "s/<cluster_name>/${local_cluster}/g")

managed_clusters="${c1},${c2},${local_cluster}"
echo -e "Accept join of ${managed_clusters}\n"
clusteradm accept --context ${hubctx} --clusters ${managed_clusters} --wait

kubectl label managedcluster "${local_cluster}" local-cluster=true --overwrite --context ${hubctx}

kubectl get managedclusters --all-namespaces --context ${hubctx}
