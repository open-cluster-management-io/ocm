#!/usr/bin/env bash
# Copyright Contributors to the Open Cluster Management project
#
# Deploys the SQL-backed OCM APIService POC. An APIService owns an entire
# group/version, so never point this at an OCM hub that must keep serving
# these APIs from CRDs; use a disposable cluster.
#
# Takeover mode (OCM_API_POC_TAKEOVER=yes) is for the isolated demo flow in
# solutions/aggregate-api-demo: it expects CRDs for the selected group/versions
# to already exist and re-points their local APIServices at this server, so the
# CRDs stay installed but shadowed. Cleanup restores them to local serving.
#
# Environment variables:
#   OCM_API_POC_IMAGE         (required) image for the apiserver deployment
#   OCM_API_POC_DB_PASSWORD   (required) disposable PostgreSQL app password
#   OCM_API_POC_PULL_POLICY   (optional) Always | IfNotPresent | Never (default Never)
#   OCM_API_POC_NAMESPACE     (optional) default ocm-api-poc
#   OCM_API_POC_KUBECONTEXT   (optional) explicit kubectl context
#   OCM_API_POC_TAKEOVER      (optional) yes to shadow existing CRD-backed
#                             local APIServices instead of refusing (default no)
set -euo pipefail

: "${OCM_API_POC_IMAGE:?Set OCM_API_POC_IMAGE to the image the apiserver deployment should run}"
: "${OCM_API_POC_DB_PASSWORD:?Set OCM_API_POC_DB_PASSWORD to a disposable test password}"
case "${OCM_API_POC_TAKEOVER:-no}" in
yes | no) ;;
*)
  echo "OCM_API_POC_TAKEOVER must be yes or no" >&2
  exit 1
  ;;
esac
takeover="${OCM_API_POC_TAKEOVER}"
case "${OCM_API_POC_PULL_POLICY:-Never}" in
Always | IfNotPresent | Never) ;;
*)
  echo "OCM_API_POC_PULL_POLICY must be Always, IfNotPresent, or Never" >&2
  exit 1
  ;;
esac
if [[ ! "$OCM_API_POC_DB_PASSWORD" =~ ^[A-Za-z0-9_-]+$ ]]; then
  echo "Use only letters, digits, underscore, and hyphen in the disposable test password." >&2
  exit 1
fi

namespace="${OCM_API_POC_NAMESPACE:-ocm-api-poc}"
service=ocm-aggregate-api
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
api_versions_file="$script_dir/../internal/server/apiservices.txt"
mapfile -t api_versions < <(grep -vE '^[[:space:]]*(#|$)' "$api_versions_file")
if [[ ${#api_versions[@]} -eq 0 ]]; then
  echo "No APIService group/versions configured in $api_versions_file" >&2
  exit 1
fi
tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT

KUBECTL=(kubectl)
if [[ -n "${OCM_API_POC_KUBECONTEXT:-}" ]]; then
  contexts=$(kubectl config get-contexts -o name)
  if ! grep -qx "$OCM_API_POC_KUBECONTEXT" <<<"$contexts"; then
    echo "kubectl context '$OCM_API_POC_KUBECONTEXT' does not exist" >&2
    exit 1
  fi
  KUBECTL+=(--context "$OCM_API_POC_KUBECONTEXT")
fi

echo "Target context: $("${KUBECTL[@]}" config current-context)"
echo "Target API server: $("${KUBECTL[@]}" config view --minify -o jsonpath='{.clusters[0].cluster.server}')"

"${KUBECTL[@]}" get crd -o json >"$tmpdir/crds.json" || {
  echo "Cannot inspect CRDs in the target context; refusing deployment." >&2
  exit 1
}
"${KUBECTL[@]}" get apiservices.apiregistration.k8s.io -o json >"$tmpdir/apiservices.json" || {
  echo "Cannot inspect existing APIServices in the target context; refusing deployment." >&2
  exit 1
}

# Classify every configured group/version as create (no APIService yet),
# takeover (CRD-backed local APIService to shadow), or adopt (already ours).
# Refuses unsafe combinations in both modes.
python3 - "$tmpdir/crds.json" "$tmpdir/apiservices.json" "$takeover" "$namespace" "$service" "${api_versions[@]}" >"$tmpdir/apiservice-actions.txt" <<'PY'
import json
import sys

crds_path, apiservices_path, takeover, namespace, service = sys.argv[1:6]
catalog = sys.argv[6:]

with open(crds_path, encoding="utf-8") as stream:
    crds = json.load(stream)["items"]
with open(apiservices_path, encoding="utf-8") as stream:
    apiservices = {item["metadata"]["name"]: item for item in json.load(stream)["items"]}

shadowed = {}
for crd in crds:
    spec = crd["spec"]
    for version in spec.get("versions", []):
        group_version = f'{spec["group"]}/{version["name"]}'
        if version.get("served") and group_version in catalog:
            shadowed.setdefault(group_version, []).append(crd["metadata"]["name"])

if takeover != "yes" and shadowed:
    print("Refusing deployment: selected APIService group/versions overlap installed CRDs:", file=sys.stderr)
    print("(Set OCM_API_POC_TAKEOVER=yes to shadow them on a disposable demo cluster.)", file=sys.stderr)
    for group_version, names in sorted(shadowed.items()):
        print(f"  {group_version}: {', '.join(names)}", file=sys.stderr)
    sys.exit(1)

errors = []
actions = {}
for group_version in catalog:
    group, version = group_version.split("/", 1)
    name = f"{version}.{group}"
    item = apiservices.get(name)
    if item is None:
        actions[group_version] = "create"
        continue
    labels = item.get("metadata", {}).get("labels", {})
    spec_service = item.get("spec", {}).get("service")
    ours = labels.get("app.kubernetes.io/managed-by") == "ocm-aggregate-api-poc"
    if spec_service is None:
        if ours:
            actions[group_version] = "adopt"
        elif takeover == "yes":
            actions[group_version] = "takeover"
        else:
            errors.append(
                f"APIService {name} already exists as a local (CRD-backed) service; "
                "set OCM_API_POC_TAKEOVER=yes to shadow it."
            )
    elif spec_service.get("name") == service and spec_service.get("namespace") == namespace:
        if ours:
            actions[group_version] = "adopt"
        else:
            errors.append(f"APIService {name} points at {namespace}/{service} but lacks the POC ownership label.")
    else:
        errors.append(
            f"APIService {name} is served by {spec_service.get('namespace')}/{spec_service.get('name')}; refusing."
        )

if errors:
    for error in errors:
        print("Refusing deployment: " + error, file=sys.stderr)
    sys.exit(1)

if takeover == "yes" and shadowed:
    print("# shadowed CRDs (left installed but no longer served by the CRD handler):")
    for group_version, names in sorted(shadowed.items()):
        print(f"#   {group_version}: {', '.join(names)}")
for group_version in sorted(actions):
    print(f"{actions[group_version]} {group_version}")
PY

declare -A apiservice_actions
while read -r action group_version; do
  [[ "$action" == \#* ]] && continue
  apiservice_actions["$group_version"]="$action"
done <"$tmpdir/apiservice-actions.txt"

"${KUBECTL[@]}" get namespaces -o json >"$tmpdir/namespaces.json" || {
  echo "Cannot inspect namespaces; refusing deployment." >&2
  exit 1
}
namespace_exists=$(python3 - "$tmpdir/namespaces.json" "$namespace" <<'PY'
import json, sys
with open(sys.argv[1], encoding="utf-8") as stream:
    items = json.load(stream)["items"]
target = next((item for item in items if item["metadata"]["name"] == sys.argv[2]), None)
if target is None:
    print("no")
elif target.get("metadata", {}).get("labels", {}).get("app.kubernetes.io/managed-by") == "ocm-aggregate-api-poc":
    print("owned")
else:
    print("unowned")
PY
)
if [[ "$namespace_exists" == unowned ]]; then
  echo "Refusing to reuse namespace $namespace without the POC ownership label." >&2
  exit 1
elif [[ "$namespace_exists" == no ]]; then
  cat <<EOF | "${KUBECTL[@]}" apply -f -
apiVersion: v1
kind: Namespace
metadata:
  name: ${namespace}
  labels:
    app.kubernetes.io/managed-by: ocm-aggregate-api-poc
EOF
elif [[ "$namespace_exists" != owned ]]; then
  echo "Could not establish ownership of namespace $namespace; refusing deployment." >&2
  exit 1
fi

"${KUBECTL[@]}" get configmap extension-apiserver-authentication -n kube-system \
  -o jsonpath='{.data.requestheader-client-ca-file}' >"$tmpdir/requestheader-client-ca.crt"
if [ ! -s "$tmpdir/requestheader-client-ca.crt" ]; then
  echo "The cluster does not provide requestheader-client-ca-file in extension-apiserver-authentication." >&2
  exit 1
fi
proxy_names=$("${KUBECTL[@]}" get configmap extension-apiserver-authentication -n kube-system \
  -o jsonpath='{.data.requestheader-allowed-names}' | tr -d '[]" ')
if [ -z "$proxy_names" ]; then
  echo "The cluster does not provide requestheader-allowed-names; refusing to trust proxy headers." >&2
  exit 1
fi

openssl req -x509 -nodes -newkey rsa:2048 -days 30 \
  -keyout "$tmpdir/tls.key" -out "$tmpdir/tls.crt" \
  -subj "/CN=${service}.${namespace}.svc" \
  -addext "subjectAltName=DNS:${service},DNS:${service}.${namespace},DNS:${service}.${namespace}.svc"

# Reuse an existing PostgreSQL secret so a partial redeploy keeps the role
# password provisioned by the initdb script. Changing the password requires
# deleting the namespace (make undeploy), which removes the database volume.
if "${KUBECTL[@]}" get secret ocm-api-poc-postgres -n "$namespace" >/dev/null 2>&1; then
  echo "Reusing existing secret ${namespace}/ocm-api-poc-postgres"
else
  admin_password=$(openssl rand -hex 24)
  "${KUBECTL[@]}" create secret generic ocm-api-poc-postgres -n "$namespace" \
    --from-literal=password="$OCM_API_POC_DB_PASSWORD" \
    --from-literal=admin-password="$admin_password" \
    --from-literal=dsn="postgres://manifestwork_poc:${OCM_API_POC_DB_PASSWORD}@ocm-api-poc-postgres.${namespace}.svc:5432/manifestwork_poc?sslmode=disable"
fi
"${KUBECTL[@]}" create configmap ocm-api-poc-postgres-init -n "$namespace" \
  --from-file=initdb.sh="$script_dir/postgres-init.sh" \
  --dry-run=client -o yaml | "${KUBECTL[@]}" apply -f -
"${KUBECTL[@]}" create secret generic ocm-api-poc-proxy -n "$namespace" \
  --from-literal=allowed-names="$proxy_names" \
  --dry-run=client -o yaml | "${KUBECTL[@]}" apply -f -
"${KUBECTL[@]}" create secret generic ocm-aggregate-api-tls -n "$namespace" \
  --from-file=tls.crt="$tmpdir/tls.crt" \
  --from-file=tls.key="$tmpdir/tls.key" \
  --from-file=requestheader-client-ca.crt="$tmpdir/requestheader-client-ca.crt" \
  --dry-run=client -o yaml | "${KUBECTL[@]}" apply -f -

# The YAML sources use the default namespace, image, and pull policy; render
# the actual deployment values into a temp copy before applying.
sed -e "s|namespace: ocm-api-poc|namespace: ${namespace}|g" \
  "$script_dir/postgres.yaml" >"$tmpdir/postgres.yaml"
"${KUBECTL[@]}" apply -f "$tmpdir/postgres.yaml"
"${KUBECTL[@]}" -n "$namespace" rollout status deployment/ocm-api-poc-postgres --timeout=180s

sed -e "s|namespace: ocm-api-poc|namespace: ${namespace}|g" \
  -e "s|image: .*|image: ${OCM_API_POC_IMAGE}|" \
  -e "s|imagePullPolicy: .*|imagePullPolicy: ${OCM_API_POC_PULL_POLICY}|" \
  "$script_dir/apiserver.yaml" >"$tmpdir/apiserver.yaml"
"${KUBECTL[@]}" apply -f "$tmpdir/apiserver.yaml"
"${KUBECTL[@]}" -n "$namespace" rollout status deployment/ocm-aggregate-api --timeout=180s

ca_bundle=$(base64 <"$tmpdir/tls.crt" | tr -d '\n')
for group_version in "${api_versions[@]}"; do
  group="${group_version%/*}"
  api_version="${group_version##*/}"
  apiservice="${api_version}.${group}"
  action="${apiservice_actions[$group_version]}"
  if [[ "$action" == create ]]; then
    cat <<EOF | "${KUBECTL[@]}" apply -f -
apiVersion: apiregistration.k8s.io/v1
kind: APIService
metadata:
  name: ${apiservice}
  labels:
    app.kubernetes.io/managed-by: ocm-aggregate-api-poc
spec:
  group: ${group}
  version: ${api_version}
  groupPriorityMinimum: 2000
  versionPriority: 10
  caBundle: ${ca_bundle}
  service:
    name: ${service}
    namespace: ${namespace}
    port: 443
EOF
  else
    # Takeover or adopt: re-point the existing (CRD-backed local) APIService
    # at this server. Removing the automanaged label is what stops the
    # kube-aggregator autoregistration controller from reverting it to local.
    if [[ "$action" == takeover ]]; then
      echo "Taking over CRD-backed APIService ${apiservice}"
    fi
    "${KUBECTL[@]}" patch apiservice "$apiservice" --type=merge -p "
{
  \"metadata\": {
    \"labels\": {
      \"kube-aggregator.kubernetes.io/automanaged\": null,
      \"app.kubernetes.io/managed-by\": \"ocm-aggregate-api-poc\"
    }
  },
  \"spec\": {
    \"service\": {\"name\": \"${service}\", \"namespace\": \"${namespace}\", \"port\": 443},
    \"caBundle\": \"${ca_bundle}\",
    \"groupPriorityMinimum\": 2000,
    \"versionPriority\": 10
  }
}"
  fi
done
for group_version in "${api_versions[@]}"; do
  group="${group_version%/*}"
  api_version="${group_version##*/}"
  apiservice="${api_version}.${group}"
  "${KUBECTL[@]}" wait --for=condition=Available apiservice/"$apiservice" --timeout=180s
done
echo "OCM APIService group/versions are available: ${api_versions[*]}"
if [[ "$takeover" == yes ]]; then
  echo "Takeover mode: CRDs for these group/versions remain installed but shadowed;"
  echo "their objects are served from PostgreSQL by the aggregate API server."
  echo "Run deploy/uninstall.sh to restore local CRD serving."
else
  echo "Do not install CRDs for these group/versions in this isolated cluster."
fi
