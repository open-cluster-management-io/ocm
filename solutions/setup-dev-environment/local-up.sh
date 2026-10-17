#!/bin/bash

set -euo pipefail

hub=${HUB:-hub}
c1=${CLUSTER1:-cluster1}
c2=${CLUSTER2:-cluster2}
local_cluster=local-cluster
build_images=${BUILD_IMAGES:-false}

fail() {
    printf '%s\n' "$*" >&2
    exit 1
}

case "$build_images" in
    true|false) ;;
    *) fail "BUILD_IMAGES must be true or false, got: $build_images" ;;
esac

require() {
    command -v "$1" >/dev/null || fail "Required command not found: $1"
}

for command in kind kubectl clusteradm sed; do
    require "$command"
done

if [[ "$hub" == "$c1" || "$hub" == "$c2" || "$c1" == "$c2" || "$c1" == "$local_cluster" || "$c2" == "$local_cluster" ]]; then
    fail "HUB, CLUSTER1 and CLUSTER2 must be distinct; managed cluster names must not be local-cluster."
fi

if [[ "$build_images" == true ]]; then
    [[ -n "${BASH_SOURCE[0]:-}" ]] || fail "BUILD_IMAGES=true requires local-up.sh from a local OCM checkout."
    repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
    [[ -f "$repo_root/Makefile" ]] || fail "OCM Makefile not found in $repo_root. Run local-up.sh from an OCM checkout."
    for command in make go; do
        require "$command"
    done
    container_cli=${CONTAINER_CLI:-${KIND_EXPERIMENTAL_PROVIDER:-}}
    if [[ -z "$container_cli" ]]; then
        container_cli=$(command -v docker || command -v podman) || fail "BUILD_IMAGES=true requires Docker or Podman."
    else
        require "$container_cli"
    fi
    container_runtime=${container_cli##*/}
    case "$container_runtime" in
        docker|podman) export KIND_EXPERIMENTAL_PROVIDER="$container_runtime" ;;
        *) fail "CONTAINER_CLI must be docker or podman, got: $container_cli" ;;
    esac
    "$container_cli" info >/dev/null
    image_registry=${IMAGE_REGISTRY:-quay.io/open-cluster-management}
    image_registry=${image_registry%/}
    image_tag=${IMAGE_TAG:-local}
    [[ "$image_tag" =~ ^[a-zA-Z0-9_][a-zA-Z0-9_.-]{0,127}$ ]] || fail "Invalid IMAGE_TAG: $image_tag"
    [[ "$image_tag" != latest ]] || fail "BUILD_IMAGES=true requires a non-latest IMAGE_TAG so Pods use the loaded images; use IMAGE_TAG=local."
fi

clusters=("$hub" "$c1" "$c2")
existing_clusters=$(kind get clusters)
for cluster in "${clusters[@]}"; do
    if grep -qxF -- "$cluster" <<< "$existing_clusters"; then
        fail "Kind cluster '$cluster' already exists. Choose unused HUB, CLUSTER1 and CLUSTER2 names."
    fi
done

hubctx="kind-${hub}"
c1ctx="kind-${c1}"
c2ctx="kind-${c2}"

for cluster in "${clusters[@]}"; do
    kind create cluster --name "$cluster" --wait 300s --retain
done

init_args=(init --context "$hubctx")
if [[ -n "${HUB_FEATURE_GATES:-}" ]]; then
    init_args+=(--feature-gates "$HUB_FEATURE_GATES")
fi

install_args=(--wait)
if [[ "$build_images" == true ]]; then
    architecture=$(kubectl --context "$hubctx" get nodes -o jsonpath='{.items[0].status.nodeInfo.architecture}')
    [[ -n "$architecture" ]] || fail "Could not determine node architecture in $hubctx."
    make_args=(-C "$repo_root" "CONTAINER_CLI=$container_cli" "KIND_CLUSTER_ARCH=$architecture"
        "IMAGE_REGISTRY=$image_registry" "IMAGE_TAG=$image_tag")
    make "${make_args[@]}" images-kind
    for cluster in "${clusters[@]}"; do
        make "${make_args[@]}" load-images-kind "KIND_CLUSTER_NAME=$cluster"
    done

    bundle_overrides=$(mktemp "${TMPDIR:-/tmp}/ocm-local-up.XXXXXX")
    trap 'rm -f "$bundle_overrides"' EXIT
    printf '{"ocm":"%s"}\n' "$image_tag" > "$bundle_overrides"
    install_args=(--wait=false --image-registry "$image_registry" --bundle-version-overrides "$bundle_overrides")
fi
init_args+=("${install_args[@]}")

echo -e "Initialize the ocm hub cluster\n"
clusteradm "${init_args[@]}"
if [[ "$build_images" == true ]]; then
    # clusteradm embeds released RBAC, which can lack permissions needed by this checkout's operator.
    kubectl --context "$hubctx" apply -k "$repo_root/deploy/cluster-manager/config/rbac"
    for condition in Applied Progressing=false HubRegistrationDegraded=false; do
        kubectl --context "$hubctx" wait --for="condition=$condition" --timeout=300s clustermanager/cluster-manager
    done
fi
join_command=$(clusteradm get token --context "$hubctx" | sed -n 's/^.*clusteradm join /join /p')
[[ -n "$join_command" && "$join_command" != *$'\n'* ]] || fail "clusteradm get token did not return exactly one join command."

join_cluster() {
    local cluster_name=$1 context=$2
    local args
    read -r -a args <<< "${join_command//<cluster_name>/$cluster_name}"
    args+=(--force-internal-endpoint-lookup --context "$context")
    if [[ -n "${SPOKE_FEATURE_GATES:-}" ]]; then
        args+=(--feature-gates "$SPOKE_FEATURE_GATES")
    fi
    clusteradm "${args[@]}" "${install_args[@]}"
    if [[ "$build_images" == true ]]; then
        kubectl --context "$context" apply -k "$repo_root/deploy/klusterlet/config/rbac"
    fi
}

echo -e "Join cluster1 to hub\n"
join_cluster "$c1" "$c1ctx"

echo -e "Join cluster2 to hub\n"
join_cluster "$c2" "$c2ctx"

echo -e "Join hub to itself as ${local_cluster}\n"
join_cluster "$local_cluster" "$hubctx"

managed_clusters="${c1},${c2},${local_cluster}"
echo -e "Accept join of ${managed_clusters}\n"
clusteradm accept --context "${hubctx}" --clusters "${managed_clusters}" --wait

kubectl label managedcluster "${local_cluster}" local-cluster=true --overwrite --context "${hubctx}"

kubectl get managedclusters --all-namespaces --context "${hubctx}"
