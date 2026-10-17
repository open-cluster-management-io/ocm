#!/bin/bash
# Copyright Contributors to the Open Cluster Management project

set -euo pipefail

# shellcheck source-path=SCRIPTDIR
# shellcheck source=common.sh
source "$(dirname "${BASH_SOURCE[0]}")/common.sh"

configure_kind
require git
initialize_run
implementation_revision=$(git -C "$repo_root" rev-parse HEAD)
implementation_dirty=false
[[ -z "$(git -C "$repo_root" status --porcelain)" ]] || implementation_dirty=true
hubctl() { kubectl --context "$hub_context" "$@"; }

BUILD_IMAGES=true HUB_FEATURE_GATES="${HUB_FEATURE_GATES:+$HUB_FEATURE_GATES,}ClusterProfile=true" \
    "$repo_root/solutions/setup-dev-environment/local-up.sh"

managed_clusters=("$CLUSTER1" "$CLUSTER2" local-cluster)
managed_resources=("${managed_clusters[@]/#/managedcluster/}")
profile_resources=("${managed_clusters[@]/#/clusterprofile/}")
for condition in ManagedClusterJoined ManagedClusterConditionAvailable; do
    hubctl wait --for="condition=$condition" --timeout=300s "${managed_resources[@]}"
done
# The observer reads ClusterProfile v1alpha2; fail here if the hub CRD does not serve it.
hubctl get --raw /apis/multicluster.x-k8s.io/v1alpha2 >/dev/null

hubctl apply -f - <<EOF
apiVersion: v1
kind: Namespace
metadata:
  name: $inventory_namespace
---
apiVersion: cluster.open-cluster-management.io/v1beta2
kind: ManagedClusterSet
metadata:
  name: conformance
spec:
  clusterSelector:
    selectorType: LabelSelector
    labelSelector: {}
---
apiVersion: cluster.open-cluster-management.io/v1beta2
kind: ManagedClusterSetBinding
metadata:
  name: conformance
  namespace: $inventory_namespace
spec:
  clusterSet: conformance
EOF

inventory_wait() { hubctl -n "$inventory_namespace" wait --timeout=180s "$@"; }
inventory_wait --for=condition=Bound managedclustersetbinding/conformance
inventory_wait --for=create "${profile_resources[@]}"
for condition in Joined ControlPlaneHealthy; do
    inventory_wait --for="condition=$condition" "${profile_resources[@]}"
done
inventory_wait --for=jsonpath='{.status.version.kubernetes}' "${profile_resources[@]}"

printf 'Repeat observer: make -C %q test-cluster-inventory-conformance HUB=%q CONTAINER_CLI=%q\n' \
    "$repo_root" "$HUB" "$container_cli"
run_observer --version "git-$implementation_revision" --revision "$implementation_revision" "--dirty=$implementation_dirty"
