#!/bin/bash

set -e

source "$(dirname "${BASH_SOURCE[0]}")/common.sh"

configure_kind
initialize_run
existing_clusters=$(kind get clusters)
if ! grep -qxF -- "${HUB}" <<< "${existing_clusters}"; then
    fail "Kind hub '${HUB}' not found. Create the conformance environment with make test-cluster-inventory-conformance-kind."
fi
kind get kubeconfig --name "${HUB}" > "${KUBECONFIG}"
run_observer
