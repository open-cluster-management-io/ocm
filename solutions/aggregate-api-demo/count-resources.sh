#!/usr/bin/env bash
# Copyright Contributors to the Open Cluster Management project
#
# Reports where objects live after a demo run:
#   1. Objects stored in PostgreSQL by the POC aggregate API server
#   2. Objects served by the Kubernetes API (OCM APIs + Kubernetes-native)
#   3. Raw etcd key counts and database size (what actually occupies etcd)
#
# Works against both a baseline hub (no POC: PostgreSQL section reports none)
# and an aggregate hub (POC deployed). Counts object metadata only; it never
# reads or prints Secret values.
#
# Usage: ./count-resources.sh <hub-kube-context>

set -euo pipefail

context="${1:?Usage: ./count-resources.sh <hub-kube-context>}"
poc_namespace=ocm-api-poc

K=(kubectl --context "$context")

title() {
  echo
  echo "=== $1 ==="
}

columnize() {
  # Reads "label|count" or "label<TAB>count" lines and prints an aligned table.
  awk -F'[|\t]' 'BEGIN { printf "%-52s %10s\n", "RESOURCE", "COUNT" }
    NF >= 2 { printf "%-52s %10d\n", $1, $2 }'
}

title "1/3 Objects stored in PostgreSQL (aggregate API server)"
if "${K[@]}" -n "$poc_namespace" get deployment ocm-aggregate-api >/dev/null 2>&1; then
  psql_count() {
    "${K[@]}" -n "$poc_namespace" exec -i deployment/ocm-api-poc-postgres -- \
      psql -U manifestwork_poc -d manifestwork_poc -t -A -F'|' -c "$1" 2>/dev/null
  }
  echo "generic object store (poc_objects):"
  psql_count "SELECT resource || '/' || api_version, count(*) FROM poc_objects GROUP BY resource, api_version ORDER BY resource, api_version;" | columnize
  echo
  echo "typed ManifestWork store (manifestworks):"
  psql_count "SELECT 'work.open-cluster-management.io/v1/manifestworks', count(*) FROM manifestworks;" | columnize
  echo
  echo "watch event journal rows:"
  psql_count "SELECT 'poc_object_events', count(*) FROM poc_object_events UNION ALL SELECT 'manifestwork_events', count(*) FROM manifestwork_events;" | columnize
else
  echo "Not deployed (baseline hub): all OCM objects are stored in etcd via CRDs."
fi

title "2/3 Objects served by the Kubernetes API on the hub"
echo "OCM resources (served by the aggregate API server when the POC is deployed):"
{
  for entry in \
    "managedclusters cluster.open-cluster-management.io/v1" \
    "managedclustersets cluster.open-cluster-management.io/v1beta2" \
    "managedclustersetbindings cluster.open-cluster-management.io/v1beta2" \
    "placements cluster.open-cluster-management.io/v1beta1" \
    "placementdecisions cluster.open-cluster-management.io/v1beta1" \
    "addonplacementscores cluster.open-cluster-management.io/v1alpha1" \
    "manifestworks work.open-cluster-management.io/v1" \
    "manifestworkreplicasets work.open-cluster-management.io/v1alpha1" \
    "managedclusteraddons addon.open-cluster-management.io/v1beta1" \
    "clustermanagementaddons addon.open-cluster-management.io/v1beta1" \
    "addondeploymentconfigs addon.open-cluster-management.io/v1beta1" \
    "addontemplates addon.open-cluster-management.io/v1beta1" \
    "managedclusterinfos internal.open-cluster-management.io/v1beta1"; do
    resource="${entry%% *}"
    count=$("${K[@]}" get "$resource" -A --no-headers 2>/dev/null | wc -l)
    printf '%s\t%s\n' "$entry" "$count"
  done
} | columnize

echo
echo "Kubernetes-native resources (always stored in hub etcd):"
{
  for resource in secrets configmaps leases events namespaces serviceaccounts roles rolebindings clusterroles clusterrolebindings certificatesigningrequests pods deployments; do
    count=$("${K[@]}" get "$resource" -A --no-headers 2>/dev/null | wc -l)
    printf '%s\t%s\n' "$resource" "$count"
  done
} | columnize

echo
echo "Kubernetes-native objects inside the managed-cluster namespaces:"
for namespace in cluster1 cluster2 agg-cluster1 agg-cluster2 local-cluster; do
  if [[ "$namespace" != agg-cluster2 ]] && "${K[@]}" get namespace "$namespace" >/dev/null 2>&1; then
    echo "namespace ${namespace}:"
    {
      for resource in secrets configmaps leases serviceaccounts roles rolebindings; do
        count=$("${K[@]}" -n "$namespace" get "$resource" --no-headers 2>/dev/null | wc -l)
        printf '%s/%s\t%s\n' "$namespace" "$resource" "$count"
      done
    } | columnize
  fi
done

echo
echo "POC infrastructure (aggregate server + PostgreSQL, in hub etcd):"
{
  for entry in "apiservices-taken-over apiregistration.k8s.io" "pods-in-${poc_namespace} v1/pods"; do
    label="${entry%% *}"
    if [[ "$entry" == apiservices-* ]]; then
      count=$("${K[@]}" get apiservices -l app.kubernetes.io/managed-by=ocm-aggregate-api-poc --no-headers 2>/dev/null | wc -l)
    else
      count=$("${K[@]}" -n "$poc_namespace" get pods --no-headers 2>/dev/null | wc -l)
    fi
    printf '%s\t%s\n' "$label" "$count"
  done
} | columnize

title "3/3 Raw etcd occupancy on the hub"
etcd_pod=$("${K[@]}" -n kube-system get pods -l component=etcd -o name 2>/dev/null | head -1 | cut -d/ -f2)
if [[ -z "$etcd_pod" ]]; then
  echo "No etcd pod found; skipping etcd inspection."
  exit 0
fi
etcdctl=(etcdctl --endpoints=https://127.0.0.1:2379
  --cacert=/etc/kubernetes/pki/etcd/ca.crt
  --cert=/etc/kubernetes/pki/etcd/server.crt
  --key=/etc/kubernetes/pki/etcd/server.key)
etcd_keys=$("${K[@]}" -n kube-system exec "$etcd_pod" -- "${etcdctl[@]}" get /registry/ --prefix --keys-only 2>/dev/null | grep -v '^$' || true)
if [[ -z "$etcd_keys" ]]; then
  echo "Could not read etcd keys (etcdctl failed); skipping."
  exit 0
fi
echo "etcd keys by resource prefix (/registry/...):"
echo "$etcd_keys" | awk '
  /^\/registry\// {
    rest = substr($0, 11)
    split(rest, seg, "/")
    if (index(seg[1], ".") > 0) { key = seg[1] "/" seg[2] } else { key = seg[1] }
    counts[key]++
  }
  END { for (k in counts) printf "%s\t%d\n", k, counts[k] }' | sort | columnize
echo
echo "etcd database size:"
"${K[@]}" -n kube-system exec "$etcd_pod" -- "${etcdctl[@]}" endpoint status -w json 2>/dev/null |
  python3 -c 'import json,sys; s=json.load(sys.stdin)[0]["Status"]; print(f"dbSize: {int(s["dbSize"])/1024/1024:.2f} MiB, revisions: {s["header"]["revision"]}")'
ocm_keys=$(echo "$etcd_keys" | awk '
  /^\/registry\// {
    rest = substr($0, 11)
    split(rest, seg, "/")
    if (index(seg[1], ".") > 0 && index(seg[1], "k8s.io") == 0) { n++ }
  }
  END { print n+0 }')
native_keys=$(echo "$etcd_keys" | awk '
  /^\/registry\// {
    rest = substr($0, 11)
    split(rest, seg, "/")
    if (index(seg[1], ".") == 0 || index(seg[1], "k8s.io") > 0) { n++ }
  }
  END { print n+0 }')
echo
echo "Summary: OCM custom-resource keys in etcd: ${ocm_keys}; Kubernetes-native keys in etcd: ${native_keys}."
echo "On an aggregate hub, OCM objects live in PostgreSQL (section 1) and OCM etcd"
echo "keys should be limited to shadowed CRD definitions and objects created"
echo "before the takeover; on a baseline hub they are all in etcd."
