#!/bin/bash
# Copyright Contributors to the Open Cluster Management project

set -euo pipefail

# shellcheck source-path=SCRIPTDIR
# shellcheck source=common.sh
source "$(dirname "${BASH_SOURCE[0]}")/common.sh"

configure_kind
initialize_run
existing_clusters=$(kind get clusters)
if ! grep -qxF -- "$HUB" <<< "$existing_clusters"; then
    fail "Kind hub '$HUB' not found. Create the conformance environment with make test-cluster-inventory-conformance-kind."
fi
kind get kubeconfig --name "$HUB" > "$KUBECONFIG"
# Existing images have no known build metadata to pass to the observer.
# shellcheck disable=SC2119
run_observer
