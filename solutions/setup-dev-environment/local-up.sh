#!/bin/bash

cd "$(dirname "${BASH_SOURCE[0]}")"

set -e

hub=${HUB:-hub}
c1=${CLUSTER1:-cluster1}
c2=${CLUSTER2:-cluster2}
local_cluster=local-cluster
build_images=${BUILD_IMAGES:-false}

hubctx="kind-${hub}"
c1ctx="kind-${c1}"
c2ctx="kind-${c2}"

kind create cluster --name "${hub}"
kind create cluster --name "${c1}"
kind create cluster --name "${c2}"

install_args="--wait"
if [[ "${build_images}" == true ]]; then
    repo_root=$(cd ../.. && pwd)
    # A non-latest tag keeps the Pods on the loaded images instead of pulling the released ones.
    make -C "${repo_root}" IMAGE_TAG=local images-kind
    for cluster in "${hub}" "${c1}" "${c2}"; do
        make -C "${repo_root}" IMAGE_TAG=local KIND_CLUSTER_NAME="${cluster}" load-images-kind
    done

    bundle_overrides=$(mktemp)
    trap 'rm -f "${bundle_overrides}"' EXIT
    echo '{"ocm":"local"}' > "${bundle_overrides}"
    # The images are already loaded, so there is nothing to wait for before the RBAC below is applied.
    install_args="--wait=false --bundle-version-overrides ${bundle_overrides}"
fi

echo -e "Initialize the ocm hub cluster\n"
clusteradm init ${install_args} ${HUB_FEATURE_GATES:+--feature-gates ${HUB_FEATURE_GATES}} --context "${hubctx}"
if [[ "${build_images}" == true ]]; then
    # clusteradm embeds the released RBAC, which can lack permissions this checkout's operator needs.
    kubectl --context "${hubctx}" apply -k "${repo_root}/deploy/cluster-manager/config/rbac"
    for condition in Applied Progressing=false HubRegistrationDegraded=false; do
        kubectl --context "${hubctx}" wait --for="condition=${condition}" --timeout=300s clustermanager/cluster-manager
    done
fi
joincmd=$(clusteradm get token --context "${hubctx}" | grep "clusteradm join")

echo -e "Join cluster1 to hub\n"
$(echo "${joincmd}" --force-internal-endpoint-lookup ${install_args} --context "${c1ctx}" | sed "s/<cluster_name>/${c1}/g")

echo -e "Join cluster2 to hub\n"
$(echo "${joincmd}" --force-internal-endpoint-lookup ${install_args} --context "${c2ctx}" | sed "s/<cluster_name>/${c2}/g")

echo -e "Join hub to itself as ${local_cluster}\n"
$(echo "${joincmd}" --force-internal-endpoint-lookup ${install_args} --context "${hubctx}" | sed "s/<cluster_name>/${local_cluster}/g")

if [[ "${build_images}" == true ]]; then
    for ctx in "${c1ctx}" "${c2ctx}" "${hubctx}"; do
        kubectl --context "${ctx}" apply -k "${repo_root}/deploy/klusterlet/config/rbac"
    done
fi

managed_clusters="${c1},${c2},${local_cluster}"
echo -e "Accept join of ${managed_clusters}\n"
clusteradm accept --context "${hubctx}" --clusters "${managed_clusters}" --wait

kubectl label managedcluster "${local_cluster}" local-cluster=true --overwrite --context "${hubctx}"

kubectl get managedclusters --all-namespaces --context "${hubctx}"
