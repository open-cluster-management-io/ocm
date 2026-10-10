#!/bin/bash

cd -- "$(dirname -- "${BASH_SOURCE[0]}")" || exit 1

set -e

hubctx="${1:-kind-hub}"
shift || true
clusters=("$@")

msa_name="headlamp"
namespace="headlamp"
clusterset="headlamp"
cluster_proxy_version="0.12.0"
msa_version="0.11.0"
headlamp_version="0.45.0"

kubectl config use-context "${hubctx}"

echo "Enabling the ClusterProfile feature gate on the hub"
if kubectl get clustermanager cluster-manager \
    -o jsonpath='{.spec.registrationConfiguration.featureGates[*].feature}' | grep -qw ClusterProfile; then
    echo "  already enabled"
else
    kubectl patch clustermanager cluster-manager --type=json \
        -p='[{"op":"add","path":"/spec/registrationConfiguration/featureGates/-","value":{"feature":"ClusterProfile","mode":"Enable"}}]'
fi

echo "Waiting for the ClusterProfile CRD (the operator has to roll the registration controller first)"
for _ in $(seq 1 60); do
    if kubectl get crd clusterprofiles.multicluster.x-k8s.io >/dev/null 2>&1; then
        break
    fi
    sleep 5
done
kubectl wait --for=condition=Established crd/clusterprofiles.multicluster.x-k8s.io --timeout=60s

echo "Creating the ${namespace} namespace, ManagedClusterSet and binding"
kubectl apply -f manifests/clusterset.yaml

# Every cluster the set selects gets a ClusterProfile, and every ClusterProfile
# needs a ManagedServiceAccount or Headlamp will list the cluster and then fail
# to reach it. Default to covering the whole set rather than a fixed list.
if [ ${#clusters[@]} -eq 0 ]; then
    # Resolve the set's membership the way the registration controller does:
    # either the exclusive clusterset label, or the set's own label selector
    # rendered into kubectl selector syntax.
    # shellcheck disable=SC2016 # $first/$k/$v belong to the Go template, not the shell
    selector=$(kubectl get managedclusterset "${clusterset}" -o go-template='
{{- if eq .spec.clusterSelector.selectorType "ExclusiveClusterSetLabel" -}}
cluster.open-cluster-management.io/clusterset={{.metadata.name}}
{{- else if .spec.clusterSelector.labelSelector -}}
{{- $first := true -}}
{{- range $k, $v := .spec.clusterSelector.labelSelector.matchLabels -}}
{{- if not $first}},{{end}}{{$k}}={{$v}}{{$first = false}}
{{- end -}}
{{- range .spec.clusterSelector.labelSelector.matchExpressions -}}
{{- if not $first}},{{end}}{{$first = false}}
{{- if eq .operator "Exists"}}{{.key}}
{{- else if eq .operator "DoesNotExist"}}!{{.key}}
{{- else}}{{.key}} {{if eq .operator "NotIn"}}notin{{else}}in{{end}} ({{range $i, $v := .values}}{{if $i}},{{end}}{{$v}}{{end}})
{{- end -}}
{{- end -}}
{{- end -}}')

    expected=$(kubectl get managedclusters -l "${selector}" \
        -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' | sort)
    if [ -z "${expected}" ]; then
        echo "The ${clusterset} ManagedClusterSet selects no managed clusters." >&2
        echo "Widen its selector, or pass cluster names: $0 ${hubctx} cluster1 cluster2" >&2
        exit 1
    fi
    echo "Waiting for a ClusterProfile in ${namespace} for each of:" \
        "$(tr '\n' ' ' <<<"${expected}")"

    # One profile is created at a time, so a non-empty -- or even an unchanged --
    # read can still be a partial list. Wait until every selected cluster has one
    # instead of waiting for the list to settle.
    missing="${expected}"
    for _ in $(seq 1 60); do
        # A transient API error must not abort the script under set -e; treat it
        # as an empty read and let the loop try again.
        profiles=$(kubectl -n "${namespace}" get clusterprofiles \
            -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' 2>/dev/null | sort) || profiles=""
        missing=$(comm -23 <(echo "${expected}") <(echo "${profiles}"))
        [ -z "${missing}" ] && break
        sleep 2
    done
    if [ -n "${missing}" ]; then
        echo "Timed out waiting for ClusterProfiles in ${namespace} for:" \
            "$(tr '\n' ' ' <<<"${missing}")" >&2
        exit 1
    fi

    while IFS= read -r profile; do
        [ -n "${profile}" ] && clusters+=("${profile}")
    done <<<"${expected}"
fi

echo "Clusters: ${clusters[*]}"

echo "Installing the cluster-proxy addon (${cluster_proxy_version})"
helm upgrade --install cluster-proxy ocm/cluster-proxy --version "${cluster_proxy_version}" \
    -n open-cluster-management-addon --create-namespace \
    --set userServer.enabled=true \
    --set enableServiceProxy=true \
    --set featureGates.clusterProfile=true \
    --kube-context "${hubctx}"

echo "Installing the managed-serviceaccount addon (${msa_version})"
helm upgrade --install managed-serviceaccount ocm/managed-serviceaccount --version "${msa_version}" \
    -n open-cluster-management-addon --create-namespace \
    --set featureGates.clusterProfile=true \
    --take-ownership \
    --kube-context "${hubctx}"

for cluster in "${clusters[@]}"; do
    echo "Waiting for the addons on ${cluster}"
    kubectl wait managedclusteraddon/cluster-proxy -n "${cluster}" --for=condition=Available --timeout=300s
    kubectl wait managedclusteraddon/managed-serviceaccount -n "${cluster}" --for=condition=Available --timeout=300s
done

for cluster in "${clusters[@]}"; do
    echo "Creating the ManagedServiceAccount and spoke RBAC for ${cluster}"
    kubectl apply -f - <<EOF
apiVersion: authentication.open-cluster-management.io/v1beta1
kind: ManagedServiceAccount
metadata:
  name: ${msa_name}
  namespace: ${cluster}
  labels:
    authentication.open-cluster-management.io/sync-to-clusterprofile: "true"
spec:
  rotation:
    enabled: true
    validity: 8640h0m0s
EOF
    clusteradm create work headlamp-rbac -f manifests/spoke-rbac --cluster "${cluster}" \
        --context "${hubctx}" --overwrite
done

for cluster in "${clusters[@]}"; do
    echo "Waiting for credentials to reach the ClusterProfile for ${cluster}"
    server=""
    for _ in $(seq 1 60); do
        server=$(kubectl -n "${namespace}" get clusterprofile "${cluster}" \
            -o jsonpath='{.status.accessProviders[?(@.name=="open-cluster-management")].cluster.server}' 2>/dev/null) || server=""
        [ -n "${server}" ] && break
        sleep 5
    done
    # Going on without an endpoint would install a Headlamp that lists the
    # cluster in its chooser and then fails the moment it is selected.
    if [ -z "${server}" ]; then
        echo "No open-cluster-management access provider on clusterprofile/${cluster} after 300s." >&2
        echo "Check the cluster-proxy and managed-serviceaccount addons for ${cluster}." >&2
        exit 1
    fi
    echo "  ${server}"
done

echo "Installing Headlamp (${headlamp_version})"
helm repo add headlamp https://kubernetes-sigs.github.io/headlamp/ >/dev/null
helm repo update headlamp >/dev/null
helm upgrade --install headlamp headlamp/headlamp --version "${headlamp_version}" \
    -n "${namespace}" -f headlamp-values.yaml \
    --kube-context "${hubctx}"
kubectl -n "${namespace}" rollout status deployment/headlamp --timeout=300s

echo "Done. Open the UI with:"
echo "  kubectl -n ${namespace} port-forward deploy/headlamp 8080:4466"
echo "  kubectl -n ${namespace} create token headlamp"
echo "then browse to http://127.0.0.1:8080 and pick a cluster."
