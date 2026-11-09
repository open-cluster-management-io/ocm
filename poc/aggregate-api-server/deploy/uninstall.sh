#!/usr/bin/env bash
# Copyright Contributors to the Open Cluster Management project
#
# Removes only POC-owned resources. For APIServices whose group/version is
# backed by an installed CRD (takeover deployments), the APIService is restored
# to local CRD serving instead of deleted. Namespace deletion removes the
# disposable PostgreSQL data.
set -euo pipefail

namespace="${OCM_API_POC_NAMESPACE:-ocm-api-poc}"
owner="ocm-aggregate-api-poc"
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
api_versions_file="$script_dir/../internal/server/apiservices.txt"
mapfile -t api_versions < <(grep -vE '^[[:space:]]*(#|$)' "$api_versions_file")
if [[ ${#api_versions[@]} -eq 0 ]]; then
  echo "No APIService group/versions configured in $api_versions_file" >&2
  exit 1
fi

KUBECTL=(kubectl)
if [[ -n "${OCM_API_POC_KUBECONTEXT:-}" ]]; then
  KUBECTL+=(--context "$OCM_API_POC_KUBECONTEXT")
elif [[ "${OCM_API_POC_CONFIRM_CURRENT_CONTEXT:-no}" != yes ]]; then
  echo "Refusing cleanup without OCM_API_POC_KUBECONTEXT or OCM_API_POC_CONFIRM_CURRENT_CONTEXT=yes." >&2
  exit 1
fi

tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT
"${KUBECTL[@]}" get apiservices.apiregistration.k8s.io -o json >"$tmpdir/apiservices.json" || {
  echo "Cannot inspect APIServices; refusing cleanup." >&2
  exit 1
}
"${KUBECTL[@]}" get crd -o json >"$tmpdir/crds.json" || {
  echo "Cannot inspect CRDs; refusing cleanup." >&2
  exit 1
}
"${KUBECTL[@]}" get clusterrolebindings.rbac.authorization.k8s.io -o json >"$tmpdir/clusterrolebindings.json" || {
  echo "Cannot inspect ClusterRoleBindings; refusing cleanup." >&2
  exit 1
}
"${KUBECTL[@]}" get namespaces -o json >"$tmpdir/namespaces.json" || {
  echo "Cannot inspect namespaces; refusing cleanup." >&2
  exit 1
}

# Verify ownership of everything we intend to touch, then classify each POC
# APIService as restore (CRD exists for its group/version) or delete.
python3 - "$tmpdir/apiservices.json" "$tmpdir/crds.json" "$tmpdir/clusterrolebindings.json" "$tmpdir/namespaces.json" "$namespace" "$owner" "${api_versions[@]}" >"$tmpdir/apiservice-cleanup.txt" <<'PY'
import json
import sys

apiservices_path, crds_path, bindings_path, namespaces_path, namespace, owner = sys.argv[1:7]
versions = sys.argv[7:]

with open(apiservices_path, encoding="utf-8") as stream:
    apiservices = {item["metadata"]["name"]: item for item in json.load(stream)["items"]}
with open(crds_path, encoding="utf-8") as stream:
    crds = json.load(stream)["items"]
with open(bindings_path, encoding="utf-8") as stream:
    bindings = {item["metadata"]["name"]: item for item in json.load(stream)["items"]}
with open(namespaces_path, encoding="utf-8") as stream:
    namespaces = {item["metadata"]["name"]: item for item in json.load(stream)["items"]}

resources = []
for group_version in versions:
    group, version = group_version.split("/", 1)
    resources.append((group_version, f"{version}.{group}", apiservices.get(f"{version}.{group}")))
resources.append(("clusterrolebinding", "ocm-aggregate-api-auth-delegator", bindings.get("ocm-aggregate-api-auth-delegator")))
resources.append(("namespace", namespace, namespaces.get(namespace)))

for kind, name, item in resources:
    if item is None:
        continue
    actual = item.get("metadata", {}).get("labels", {}).get("app.kubernetes.io/managed-by")
    if actual != owner:
        print(f"Refusing cleanup: {name} is not labeled as owned by {owner}.", file=sys.stderr)
        sys.exit(1)

# A group/version is restorable when any CRD in that group serves the version.
restorable = set()
for crd in crds:
    spec = crd["spec"]
    for version in spec.get("versions", []):
        restorable.add(f'{spec["group"]}/{version["name"]}')

for group_version, name, item in resources[:-2]:
    if item is None:
        continue
    action = "restore" if group_version in restorable else "delete"
    print(f"{action} {group_version}")
PY

for group_version in "${api_versions[@]}"; do
  group="${group_version%/*}"
  version="${group_version##*/}"
  apiservice="${version}.${group}"
  action=""
  while read -r a gv; do
    [[ "$gv" == "$group_version" ]] && action="$a"
  done <"$tmpdir/apiservice-cleanup.txt"
  [[ -z "$action" ]] && continue
  if [[ "$action" == restore ]]; then
    echo "Restoring ${apiservice} to local CRD serving"
    # Merge patch: null removes the service/caBundle fields and our label;
    # re-adding the automanaged label lets kube-aggregator manage it again.
    "${KUBECTL[@]}" patch apiservice "$apiservice" --type=merge -p '
{
  "metadata": {
    "labels": {
      "kube-aggregator.kubernetes.io/automanaged": "true",
      "app.kubernetes.io/managed-by": null
    }
  },
  "spec": {
    "service": null,
    "caBundle": null,
    "groupPriorityMinimum": 1000,
    "versionPriority": 15
  }
}'
  else
    "${KUBECTL[@]}" delete apiservice "$apiservice" --ignore-not-found
  fi
done
"${KUBECTL[@]}" delete clusterrolebinding ocm-aggregate-api-auth-delegator --ignore-not-found
"${KUBECTL[@]}" delete namespace "$namespace" --ignore-not-found --wait=true
echo "POC resources removed from context $("${KUBECTL[@]}" config current-context)."
