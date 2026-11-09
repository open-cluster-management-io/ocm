#!/usr/bin/env bash
# Copyright Contributors to the Open Cluster Management project
#
# Exercises the deployed POC through kubectl: APIService availability, RBAC,
# create, get, label-selected list, status subresource patch, watch, delete,
# and deletion verification. Requires kubectl >= 1.24 for --subresource.
#
# Environment variables:
#   OCM_API_POC_SMOKE_NAMESPACE  (optional) unique temporary namespace by default
#   OCM_API_POC_KUBECONTEXT      (optional) explicit kubectl context
set -euo pipefail

namespace="${OCM_API_POC_SMOKE_NAMESPACE:-ocm-api-poc-smoke-$(date +%s)-$$}"
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
api_versions_file="$script_dir/../internal/server/apiservices.txt"
mapfile -t api_versions < <(grep -vE '^[[:space:]]*(#|$)' "$api_versions_file")
if [[ ${#api_versions[@]} -eq 0 ]]; then
  echo "No APIService group/versions configured in $api_versions_file" >&2
  exit 1
fi
resource=manifestworks.work.open-cluster-management.io

KUBECTL=(kubectl)
if [[ -n "${OCM_API_POC_KUBECONTEXT:-}" ]]; then
  KUBECTL+=(--context "$OCM_API_POC_KUBECONTEXT")
fi

tmpdir=$(mktemp -d)
namespace_created=0
watch_pid=""
cleanup() {
  if [[ -n "$watch_pid" ]]; then
    kill "$watch_pid" 2>/dev/null || true
    wait "$watch_pid" 2>/dev/null || true
  fi
  if [[ "$namespace_created" == 1 ]]; then
    "${KUBECTL[@]}" delete namespace "$namespace" --ignore-not-found --wait=true >/dev/null || true
  fi
  rm -rf "$tmpdir"
}
trap cleanup EXIT

echo "1/9 waiting for selected APIService group/versions"
for group_version in "${api_versions[@]}"; do
  group="${group_version%/*}"
  version="${group_version##*/}"
  "${KUBECTL[@]}" wait --for=condition=Available apiservice/"${version}.${group}" --timeout=120s
done

if ! "${KUBECTL[@]}" auth can-i create "$resource" >/dev/null; then
  echo "The current kubectl user cannot create ${resource}." >&2
  echo "Grant RBAC or run the smoke test with a cluster-admin context." >&2
  exit 1
fi

if "${KUBECTL[@]}" get namespace "$namespace" >/dev/null 2>&1; then
  echo "Refusing to use existing smoke-test namespace $namespace." >&2
  exit 1
fi
"${KUBECTL[@]}" create namespace "$namespace"
namespace_created=1

manifest() {
  cat <<EOF
apiVersion: work.open-cluster-management.io/v1
kind: ManifestWork
metadata:
  name: $1
  namespace: ${namespace}
  labels:
    smoke: "true"
spec:
  workload:
    manifests:
    - apiVersion: v1
      kind: ConfigMap
      metadata:
        name: $1-cm
EOF
}

echo "2/9 create manifestwork/smoke-work"
manifest smoke-work | "${KUBECTL[@]}" create -f - --validate=false

echo "3/9 get and check resourceVersion"
rv=$("${KUBECTL[@]}" get manifestwork smoke-work -n "$namespace" -o jsonpath='{.metadata.resourceVersion}')
[[ -n "$rv" ]] || { echo "manifestwork/smoke-work has no resourceVersion" >&2; exit 1; }
echo "    resourceVersion=${rv}"

echo "4/9 list with label selector"
listed=$("${KUBECTL[@]}" get manifestworks -n "$namespace" -l smoke=true -o name)
[[ "$listed" == *smoke-work* ]] || { echo "label-selected list did not return smoke-work: ${listed}" >&2; exit 1; }

echo "5/9 patch ManifestWork status subresource"
"${KUBECTL[@]}" patch manifestwork smoke-work -n "$namespace" --subresource=status --type=merge \
  -p '{"status":{"conditions":[{"type":"Applied","status":"True","reason":"SmokeTest","message":"applied by smoke test","lastTransitionTime":"2026-01-01T00:00:00Z"}]}}' >/dev/null
reason=$("${KUBECTL[@]}" get manifestwork smoke-work -n "$namespace" -o jsonpath='{.status.conditions[0].reason}')
[[ "$reason" == "SmokeTest" ]] || { echo "status patch did not persist (reason=${reason})" >&2; exit 1; }

echo "6/9 watch for a new manifestwork"
watch_out="$tmpdir/watch.out"
"${KUBECTL[@]}" get manifestworks -n "$namespace" --watch-only -o name --request-timeout=30s >"$watch_out" &
watch_pid=$!
cleanup_watch() {
  kill "$watch_pid" 2>/dev/null || true
  wait "$watch_pid" 2>/dev/null || true
  watch_pid=""
}
sleep 3
manifest smoke-work-2 | "${KUBECTL[@]}" create -f - --validate=false >/dev/null
observed=0
for _ in $(seq 1 15); do
  if grep -q smoke-work-2 "$watch_out"; then observed=1; break; fi
  sleep 1
done
if [[ "$observed" != 1 ]]; then
  echo "watch did not observe smoke-work-2 creation" >&2
  exit 1
fi
cleanup_watch

echo "7/9 delete and verify ManifestWork removal"
"${KUBECTL[@]}" delete manifestwork smoke-work smoke-work-2 -n "$namespace" --wait=true
if "${KUBECTL[@]}" get manifestwork smoke-work -n "$namespace" >/dev/null 2>&1; then
  echo "manifestwork/smoke-work still exists after deletion" >&2
  exit 1
fi

echo "8/9 exercise database-backed ManagedClusterInfo API"
cat <<EOF | "${KUBECTL[@]}" create -f - --validate=false
apiVersion: internal.open-cluster-management.io/v1beta1
kind: ManagedClusterInfo
metadata:
  name: smoke-info
  namespace: ${namespace}
spec:
  masterEndpoint: https://api.example.invalid
EOF
"${KUBECTL[@]}" get managedclusterinfos.internal.open-cluster-management.io smoke-info -n "$namespace" -o jsonpath='{.spec.masterEndpoint}' | grep -qx 'https://api.example.invalid'
"${KUBECTL[@]}" patch managedclusterinfos.internal.open-cluster-management.io smoke-info -n "$namespace" --subresource=status --type=merge \
  -p '{"status":{"clusterID":"smoke-cluster-updated"}}' >/dev/null
"${KUBECTL[@]}" get managedclusterinfos.internal.open-cluster-management.io smoke-info -n "$namespace" -o jsonpath='{.status.clusterID}' | grep -qx 'smoke-cluster-updated'
"${KUBECTL[@]}" delete managedclusterinfos.internal.open-cluster-management.io smoke-info -n "$namespace" --wait=true

echo "9/9 exercise cluster-scoped ManagedCluster API"
cat <<'EOF' | "${KUBECTL[@]}" create -f - --validate=false
apiVersion: cluster.open-cluster-management.io/v1
kind: ManagedCluster
metadata:
  name: aggregate-api-smoke-cluster
spec:
  hubAcceptsClient: false
EOF
"${KUBECTL[@]}" get managedclusters.cluster.open-cluster-management.io aggregate-api-smoke-cluster -o name | grep -q aggregate-api-smoke-cluster
"${KUBECTL[@]}" delete managedclusters.cluster.open-cluster-management.io aggregate-api-smoke-cluster --wait=true

echo "Smoke test passed: all selected APIService versions, ManifestWork CRUD/status/watch, ManagedClusterInfo status isolation, and cluster-scoped ManagedCluster CRUD."
