#!/bin/bash

fail() {
    echo "$*" >&2
    exit 1
}

configure_kind() {
    repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
    export HUB=${HUB:-ocm-conformance-hub}
    export CLUSTER1=${CLUSTER1:-ocm-conformance-cluster1}
    export CLUSTER2=${CLUSTER2:-ocm-conformance-cluster2}
    hub_context="kind-${HUB}"
    inventory_namespace=conformance-inventory
    supported_features=ClusterManagerLabel,InventoryMemberID,InventoryMemberUniqueness,ControlPlaneHealthy,Joined,KubernetesVersion
}

initialize_run() {
    run_dir=$(mktemp -d "${TMPDIR:-/tmp}/ocm-conformance.XXXXXX")
    export KUBECONFIG="${run_dir}/kubeconfig"
    report_dir="${run_dir}/reports"
    echo "Run artifacts: ${run_dir}"
    trap 'echo "Kubeconfig: ${KUBECONFIG}"; echo "Reports: ${report_dir}"' EXIT

    cd "${repo_root}/test/conformance" || fail "Conformance module not found at ${repo_root}/test/conformance."
}

# The suite's implementation and suite metadata flags are only validated in submission mode.
# The three expected clusters are cluster1, cluster2 and the hub registered as local-cluster.
run_observer() {
    go test -count=1 -v ./... -args \
        --kubeconfig "${KUBECONFIG}" --context "${hub_context}" \
        --namespace "${inventory_namespace}" --cluster-manager open-cluster-management \
        --expected-clusters 3 --supported-features "${supported_features}" \
        --report-format html,yaml --report-dir "${report_dir}"
}
