#!/bin/bash

cd -- "$(dirname -- "${BASH_SOURCE[0]}")" || exit 1

hubctx="${1:-kind-hub}"
shift || true
clusters=("$@")

namespace="headlamp"

kubectl config use-context "${hubctx}"

# Fall back to whichever clusters still have anything of ours. Both object types
# are checked because a previous run may have deleted one and failed on the other.
if [ ${#clusters[@]} -eq 0 ]; then
    found=$(
        kubectl get managedserviceaccount --all-namespaces \
            -o jsonpath="{range .items[?(@.metadata.name=='headlamp')]}{.metadata.namespace}{'\n'}{end}" 2>/dev/null
        kubectl get manifestwork --all-namespaces \
            -o jsonpath="{range .items[?(@.metadata.name=='headlamp-rbac')]}{.metadata.namespace}{'\n'}{end}" 2>/dev/null
    )
    while IFS= read -r ns; do
        [ -n "${ns}" ] && clusters+=("${ns}")
    done <<<"$(printf '%s\n' "${found}" | sort -u)"
fi

echo "Uninstalling Headlamp"
helm uninstall headlamp -n "${namespace}" --kube-context "${hubctx}" --ignore-not-found

for cluster in "${clusters[@]}"; do
    echo "Removing the spoke RBAC and ManagedServiceAccount for ${cluster}"
    kubectl delete manifestwork headlamp-rbac -n "${cluster}" --ignore-not-found
    kubectl delete managedserviceaccount headlamp -n "${cluster}" --ignore-not-found
done

echo "Removing the ManagedClusterSet, binding and namespace"
kubectl delete -f manifests/clusterset.yaml --ignore-not-found

# cluster-proxy and managed-serviceaccount are shared hub infrastructure -- other
# ClusterProfile consumers such as MultiKueue or Argo CD may be using them -- so
# they are kept by default. Reinstalling them also forces every managed cluster to
# re-register its addon agent, and repeated cycles hit the spoke registration
# agent's CSR rate limit ("too many csr created already on hub"), which leaves the
# proxy tunnels down until it clears.
if [ "${REMOVE_ADDONS:-false}" = "true" ]; then
    echo "Uninstalling the addons (REMOVE_ADDONS=true)"
    helm uninstall managed-serviceaccount -n open-cluster-management-addon --kube-context "${hubctx}" --ignore-not-found
    helm uninstall cluster-proxy -n open-cluster-management-addon --kube-context "${hubctx}" --ignore-not-found
else
    echo "Keeping the cluster-proxy and managed-serviceaccount addons (set REMOVE_ADDONS=true to remove them)"
fi

# The ClusterProfile feature gate and its CRD are deliberately left in place.
# Other hub applications may be consuming ClusterProfiles, and the operator does
# not remove the CRD when the gate is turned off. To disable it yourself:
#
#   kubectl patch clustermanager cluster-manager --type=json \
#     -p='[{"op":"remove","path":"/spec/registrationConfiguration/featureGates/<index>"}]'
echo "Done. The ClusterProfile feature gate and CRD were left enabled on purpose (see cleanup.sh)."
