#!/usr/bin/env bash
# Copyright Contributors to the Open Cluster Management project

set -Eeuo pipefail

hub=${HUB:-sql-hub}
spoke1=${CLUSTER1:-sql-spoke1}
spoke2=${CLUSTER2:-sql-spoke2}
runtime_dir=${RUNTIME_DIR:-/tmp/opencode/ocm-sql-hub-poc-${hub}}

if [[ -n "${CONTAINER_ENGINE:-}" ]]; then
    engine=${CONTAINER_ENGINE}
elif [[ -f "${runtime_dir}/container-engine" ]]; then
    engine=$(cat "${runtime_dir}/container-engine")
elif command -v docker >/dev/null 2>&1; then
    engine=docker
else
    engine=podman
fi
command -v "${engine}" >/dev/null 2>&1 || { echo "ERROR: container engine '${engine}' is not installed" >&2; exit 1; }
case "${engine}" in
    podman) export KIND_EXPERIMENTAL_PROVIDER=podman ;;
    docker) export KIND_EXPERIMENTAL_PROVIDER=docker ;;
    *) echo "ERROR: CONTAINER_ENGINE must be docker or podman" >&2; exit 1 ;;
esac
command -v kind >/dev/null 2>&1 || { echo "ERROR: kind is required" >&2; exit 1; }

if [[ -f "${runtime_dir}/.ocm-sql-hosted-poc" ]]; then
    existing_clusters=$(kind get clusters 2>/dev/null || true)
    for name in "${hub}" "${spoke1}" "${spoke2}"; do
        if grep -qx "${name}" <<<"${existing_clusters}"; then
            echo "Deleting POC kind cluster ${name}"
            kind delete cluster --name "${name}"
        fi
    done

    rm -f "${runtime_dir}/ocm-api-host.kubeconfig" "${runtime_dir}/ocm-api-internal.kubeconfig" \
        "${runtime_dir}/ocm-api.kubeconfig" "${runtime_dir}"/*-bootstrap.kubeconfig \
        "${runtime_dir}"/bootstrap-patch.* \
        "${runtime_dir}/host-kind.yaml" "${runtime_dir}/hosted-values.yaml" \
        "${runtime_dir}/external-bootstrap-rbac.yaml" "${runtime_dir}/addon-template.yaml" \
        "${runtime_dir}/addon-workload.yaml" \
        "${runtime_dir}/container-engine" "${runtime_dir}/.ocm-sql-hosted-poc"
    if [[ -d "${runtime_dir}/pki" ]]; then
        rm -f "${runtime_dir}/pki"/*
        rmdir "${runtime_dir}/pki"
    fi
    rmdir "${runtime_dir}" 2>/dev/null || echo "Runtime directory has extra files; left in place: ${runtime_dir}"
else
    echo "No POC ownership marker at '${runtime_dir}'; leaving kind clusters untouched."
fi

echo "POC clusters and generated credentials removed. The shared kind network is retained."
