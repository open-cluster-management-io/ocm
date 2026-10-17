#!/bin/bash
# Copyright Contributors to the Open Cluster Management project

fail() {
    printf '%s\n' "$*" >&2
    exit 1
}

require() {
    command -v "$1" >/dev/null || fail "Required command not found: $1"
}

configure_kind() {
    repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
    export HUB=${HUB:-ocm-conformance-hub}
    export CLUSTER1=${CLUSTER1:-ocm-conformance-cluster1}
    export CLUSTER2=${CLUSTER2:-ocm-conformance-cluster2}
    hub_context="kind-$HUB"
    inventory_namespace=conformance-inventory
    supported_features=ClusterManagerLabel,InventoryMemberID,InventoryMemberUniqueness,ControlPlaneHealthy,Joined,KubernetesVersion

    require kind
    container_cli=${CONTAINER_CLI:-${KIND_EXPERIMENTAL_PROVIDER:-}}
    if [[ -z "$container_cli" ]]; then
        container_cli=$(command -v docker || command -v podman) || fail "Local conformance requires Docker or Podman."
    else
        require "$container_cli"
    fi
    case "${container_cli##*/}" in
        docker|podman) export KIND_EXPERIMENTAL_PROVIDER="${container_cli##*/}" ;;
        *) fail "CONTAINER_CLI must be docker or podman, got: $container_cli" ;;
    esac
}

initialize_run() {
    require kubectl
    require go
    run_dir=$(mktemp -d "${TMPDIR:-/tmp}/ocm-conformance.XXXXXX")
    export KUBECONFIG="$run_dir/kubeconfig"
    report_dir="$run_dir/reports"
    printf 'Run artifacts: %s\n' "$run_dir"
    trap 'printf "Kubeconfig: %s\nReports: %s\n" "$KUBECONFIG" "$report_dir"' EXIT

    cd "$repo_root/test/conformance" || fail "Conformance module not found at $repo_root/test/conformance."
    suite_source=$(go list -m -f '{{if .Replace}}{{.Replace.Path}}@{{.Replace.Version}}{{else}}{{.Path}}@{{.Version}}{{end}}' sigs.k8s.io/cluster-inventory-api/conformance)
    suite_version=${suite_source##*@}
    suite_revision=$(go list -m -f '{{.Origin.Hash}}' "$suite_source")
    [[ "$suite_revision" =~ ^[0-9a-f]{40}$ ]] || fail "Could not determine suite source revision for $suite_source."
}

run_observer() {
    if ! kubectl --context "$hub_context" --request-timeout=10s get namespace "$inventory_namespace" >/dev/null; then
        fail "Cannot read inventory namespace '$inventory_namespace' in '$HUB'. Prepare the conformance environment with make test-cluster-inventory-conformance-kind."
    fi
    local observer_args=(
        --kubeconfig "$KUBECONFIG" --context "$hub_context"
        --namespace "$inventory_namespace" --cluster-manager open-cluster-management
        --expected-clusters 3 --supported-features "$supported_features"
        --run-mode development --organization open-cluster-management-io --project ocm
        --mode default --url https://github.com/open-cluster-management-io/ocm
        --suite-version "$suite_version" --suite-revision "$suite_revision"
        --report-format "html,yaml" --report-dir "$report_dir"
    )
    go test -count=1 -v ./... -args "${observer_args[@]}" "$@"
}
