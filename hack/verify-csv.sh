#! /bin/bash

set -e

REPO_ROOT=$(realpath "$(dirname "${BASH_SOURCE[0]}")"/..)

if [[ ${PWD} != "${REPO_ROOT}" ]]; then
  echo "ERROR: Run this script from the root of the repository."
  exit 1
fi

SED_CMD="sed"
if [[ "$(uname)" == "Darwin" ]]; then
  if command -v gsed >/dev/null 2>&1; then
    SED_CMD="gsed"
  else
    echo "Error: gsed not found. Please install it with 'brew install gnu-sed'" >&2
    exit 1
  fi
fi

# Get the createdAt timestamp from the CSV files
cluster_manager_timestamp=$(yq '.metadata.annotations.createdAt' deploy/cluster-manager/olm-catalog/latest/manifests/cluster-manager.clusterserviceversion.yaml)
klusterlet_timestamp=$(yq '.metadata.annotations.createdAt' deploy/klusterlet/olm-catalog/latest/manifests/klusterlet.clusterserviceversion.yaml)

# Update the CSV files
echo "::group::Update CSV"
make update-csv
echo "::endgroup::"

# Reset the createdAt timestamp to the original value
${SED_CMD} -i "s/createdAt: .*/createdAt: \"${cluster_manager_timestamp}\"/g" deploy/cluster-manager/olm-catalog/latest/manifests/cluster-manager.clusterserviceversion.yaml
${SED_CMD} -i "s/createdAt: .*/createdAt: \"${klusterlet_timestamp}\"/g" deploy/klusterlet/olm-catalog/latest/manifests/klusterlet.clusterserviceversion.yaml

# Verify the CSV content is in sync
git diff --exit-code \
  deploy/cluster-manager/olm-catalog/latest/manifests/cluster-manager.clusterserviceversion.yaml \
  deploy/klusterlet/olm-catalog/latest/manifests/klusterlet.clusterserviceversion.yaml ||
  (echo '❌ CSV content is out of sync. Run "make update-csv" to update the CSV content.' && false)

echo "✅ CSV content is in sync."
