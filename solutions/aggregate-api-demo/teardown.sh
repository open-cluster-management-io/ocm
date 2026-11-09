#!/usr/bin/env bash
# Copyright Contributors to the Open Cluster Management project
#
# Tears down the aggregate API demo: restores CRD-backed APIServices to local
# serving (via the POC uninstall script) and deletes the three kind clusters.
# Deleting the clusters destroys all demo data, including PostgreSQL.

set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"

hub=${HUB:-agg-hub}
c1=${CLUSTER1:-agg-cluster1}
c2=${CLUSTER2:-agg-cluster2}
poc_dir="$(cd ../../poc/aggregate-api-server && pwd)"

original_context=$(kubectl config current-context 2>/dev/null || true)
restore_context() {
  if [[ -n "$original_context" ]]; then
    kubectl config use-context "$original_context" >/dev/null 2>&1 || true
  fi
}
trap restore_context EXIT

# Restore the CRD-backed APIServices and remove the POC namespace, but only if
# the hub cluster still exists.
if kubectl config get-contexts -o name | grep -qx "kind-${hub}"; then
  if kubectl --context "kind-${hub}" -n ocm-api-poc get deployment ocm-aggregate-api >/dev/null 2>&1; then
    OCM_API_POC_KUBECONTEXT="kind-${hub}" OCM_API_POC_NAMESPACE=ocm-api-poc \
      bash "${poc_dir}/deploy/uninstall.sh" || echo "POC uninstall failed; continuing with cluster deletion." >&2
  else
    echo "POC is not deployed on kind-${hub}; skipping uninstall."
  fi
else
  echo "Cluster kind-${hub} does not exist; skipping uninstall."
fi

kind delete clusters "${hub}" "${c1}" "${c2}"
echo "Aggregate API demo clusters deleted."
