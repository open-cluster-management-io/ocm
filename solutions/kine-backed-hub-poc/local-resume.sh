#!/usr/bin/env bash
# Copyright Contributors to the Open Cluster Management project

set -Eeuo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
hub=${HUB:-sql-hub}
spokes=("${CLUSTER1:-sql-spoke1}" "${CLUSTER2:-sql-spoke2}")
runtime_dir=${RUNTIME_DIR:-/tmp/opencode/ocm-sql-hub-poc-${hub}}
admin_kubeconfig="${runtime_dir}/ocm-api-host.kubeconfig"
internal_kubeconfig="${runtime_dir}/ocm-api-internal.kubeconfig"
ca_file="${runtime_dir}/pki/server-ca.crt"

fail() { echo "ERROR: $*" >&2; exit 1; }
for tool in kind kubectl clusteradm python3; do
    command -v "${tool}" >/dev/null 2>&1 || fail "'${tool}' is required"
done
if [[ -r /proc/sys/kernel/keys/maxkeys ]] && (( $(cat /proc/sys/kernel/keys/maxkeys) < 1000 )); then
    fail "kernel.keys.maxkeys is too low; set kernel.keys.maxkeys=10000 and kernel.keys.maxbytes=1000000, then resume"
fi
if [[ -r /proc/sys/kernel/keys/maxbytes ]] && (( $(cat /proc/sys/kernel/keys/maxbytes) < 1000000 )); then
    fail "kernel.keys.maxbytes is too low; set kernel.keys.maxkeys=10000 and kernel.keys.maxbytes=1000000, then resume"
fi
[[ -f "${runtime_dir}/.ocm-sql-hosted-poc" && -s "${admin_kubeconfig}" && -s "${internal_kubeconfig}" ]] || \
    fail "no interrupted POC runtime found at ${runtime_dir}"

internal_api=$(kubectl config view --kubeconfig "${internal_kubeconfig}" --minify -o jsonpath='{.clusters[0].cluster.server}')
host_api=$(kubectl config view --kubeconfig "${admin_kubeconfig}" --minify -o jsonpath='{.clusters[0].cluster.server}')
[[ -n "${internal_api}" && -n "${host_api}" ]] || fail "OCM API kubeconfig is incomplete"
kubectl --kubeconfig "${admin_kubeconfig}" get --raw=/readyz >/dev/null

# OCM registration approves client CSRs; the standalone Kubernetes controller
# manager signs them with the CA trusted by the OCM API server.
kubectl --context "kind-${hub}" -n ocm-sql-api create secret generic ocm-sql-api-pki \
    --from-file=apiserver.crt="${runtime_dir}/pki/apiserver.crt" \
    --from-file=apiserver.key="${runtime_dir}/pki/apiserver.key" \
    --from-file=apiserver-ca.crt="${runtime_dir}/pki/server-ca.crt" \
    --from-file=client-ca.crt="${runtime_dir}/pki/client-ca.crt" \
    --from-file=ocm-client-ca.crt="${runtime_dir}/pki/ocm-client-ca.crt" \
    --from-file=ocm-client-ca.key="${runtime_dir}/pki/ocm-client-ca.key" \
    --from-file=sa.key="${runtime_dir}/pki/sa.key" \
    --from-file=sa.pub="${runtime_dir}/pki/sa.pub" \
    --from-file=admin.kubeconfig="${internal_kubeconfig}" --dry-run=client -o yaml |
    kubectl --context "kind-${hub}" -n ocm-sql-api apply -f -
kubectl --context "kind-${hub}" -n ocm-sql-api patch deployment ocm-sql-controller-manager --type=strategic -p \
    '{"spec":{"template":{"spec":{"containers":[{"name":"kube-controller-manager","args":["--kubeconfig=/etc/ocm-api/admin.kubeconfig","--authentication-kubeconfig=/etc/ocm-api/admin.kubeconfig","--authorization-kubeconfig=/etc/ocm-api/admin.kubeconfig","--controllers=namespace,serviceaccount,serviceaccount-token,garbagecollector,bootstrapsigner,tokencleaner,csrsigning","--leader-elect=false","--secure-port=10258","--root-ca-file=/etc/ocm-api/apiserver-ca.crt","--service-account-private-key-file=/etc/ocm-api/sa.key","--cluster-signing-cert-file=/etc/ocm-api/ocm-client-ca.crt","--cluster-signing-key-file=/etc/ocm-api/ocm-client-ca.key","--cluster-signing-duration=8760h","--v=2"]}]}}}}'
kubectl --context "kind-${hub}" -n ocm-sql-api rollout status deployment/ocm-sql-controller-manager --timeout=180s

rewrite_bootstrap_endpoint() {
    local context=$1 secret_file cluster_name patch_file
    secret_file=$(mktemp "${runtime_dir}/bootstrap.XXXXXX.kubeconfig")
    for _ in $(seq 1 60); do
        if kubectl --context "${context}" -n open-cluster-management-agent get secret bootstrap-hub-kubeconfig >/dev/null 2>&1; then
            break
        fi
        sleep 1
    done
    kubectl --context "${context}" -n open-cluster-management-agent get secret bootstrap-hub-kubeconfig \
        -o jsonpath='{.data.kubeconfig}' | base64 -d >"${secret_file}"
    cluster_name=$(kubectl config view --kubeconfig "${secret_file}" -o jsonpath='{.clusters[0].name}')
    [[ -n "${cluster_name}" ]] || fail "bootstrap kubeconfig for ${context} has no cluster entry"
    current_server=$(kubectl config view --kubeconfig "${secret_file}" --minify -o jsonpath='{.clusters[0].cluster.server}')
    if [[ "${current_server}" == "${internal_api}" ]]; then
        rm -f "${secret_file}"
        return 0
    fi
    kubectl config set-cluster "${cluster_name}" --server="${internal_api}" --kubeconfig="${secret_file}" >/dev/null
    patch_file=$(mktemp "${runtime_dir}/bootstrap-patch.XXXXXX")
    python3 - "${secret_file}" "${patch_file}" <<'PY'
import base64, json, sys
with open(sys.argv[1], "rb") as source:
    data = base64.b64encode(source.read()).decode("ascii")
with open(sys.argv[2], "w", encoding="utf-8") as target:
    json.dump({"data": {"kubeconfig": data}}, target)
PY
    kubectl --context "${context}" -n open-cluster-management-agent patch secret bootstrap-hub-kubeconfig \
        --type=merge --patch-file="${patch_file}" >/dev/null
    rm -f "${secret_file}" "${patch_file}"
}

for spoke in "${spokes[@]}"; do
    context="kind-${spoke}"
    if ! kubectl --context "${context}" get klusterlet klusterlet >/dev/null 2>&1; then
        token=$(kubectl --kubeconfig "${admin_kubeconfig}" -n open-cluster-management \
            create token agent-registration-bootstrap --duration=24h)
        clusteradm join --hub-token "${token}" --hub-apiserver "${host_api}" --ca-file "${ca_file}" \
            --cluster-name "${spoke}" --context "${context}"
    fi
    rewrite_bootstrap_endpoint "${context}"
done

cat <<'EOF' | kubectl --context "kind-${hub}" apply -f -
apiVersion: v1
kind: Service
metadata:
  name: cluster-manager-registration-webhook
  namespace: cluster-manager
spec:
  ports:
  - name: https
    port: 30443
    targetPort: 30443
  selector:
    app: cluster-manager-registration-webhook
---
apiVersion: v1
kind: Service
metadata:
  name: cluster-manager-work-webhook
  namespace: cluster-manager
spec:
  ports:
  - name: https
    port: 31443
    targetPort: 31443
  selector:
    app: cluster-manager-work-webhook
---
apiVersion: v1
kind: Service
metadata:
  name: cluster-manager-addon-webhook
  namespace: cluster-manager
spec:
  ports:
  - name: https
    port: 9443
    targetPort: 9443
  selector:
    app: cluster-manager-addon-webhook
EOF
cat <<'EOF' | kubectl --kubeconfig "${admin_kubeconfig}" apply -f -
apiVersion: v1
kind: Service
metadata:
  name: cluster-manager-addon-webhook
  namespace: cluster-manager
spec:
  type: ExternalName
  externalName: cluster-manager-addon-webhook.cluster-manager.svc
  ports:
  - name: https
    port: 9443
EOF
kubectl --context "kind-${hub}" patch clustermanager cluster-manager --type=merge -p \
    '{"spec":{"deployOption":{"hosted":{"registrationWebhookConfiguration":{"address":"cluster-manager-registration-webhook.cluster-manager.svc"},"workWebhookConfiguration":{"address":"cluster-manager-work-webhook.cluster-manager.svc"}}},"placementConfiguration":{"featureGates":[{"feature":"PlacementDebugServer","mode":"Disable"}]},"addOnManagerConfiguration":{"featureGates":[{"feature":"AddonManagement","mode":"Enable"}]}}}'
kubectl --context "kind-${hub}" -n cluster-manager rollout status deployment/cluster-manager-placement-controller --timeout=5m
kubectl --context "kind-${hub}" -n cluster-manager rollout status deployment/cluster-manager-addon-manager-controller --timeout=5m

for spoke in "${spokes[@]}"; do
    for _ in $(seq 1 300); do
        if kubectl --kubeconfig "${admin_kubeconfig}" get "managedcluster/${spoke}" >/dev/null 2>&1; then
            break
        fi
        sleep 1
    done
    kubectl --kubeconfig "${admin_kubeconfig}" get "managedcluster/${spoke}" >/dev/null || \
        fail "registration agent did not create ManagedCluster ${spoke}"
    accepted=$(kubectl --kubeconfig "${admin_kubeconfig}" get "managedcluster/${spoke}" -o jsonpath='{.spec.hubAcceptsClient}')
    if [[ "${accepted}" != true ]]; then
        clusteradm accept --kubeconfig "${admin_kubeconfig}" --clusters "${spoke}" --wait
    fi
    kubectl --kubeconfig "${admin_kubeconfig}" wait --for=condition=ManagedClusterJoined \
        "managedcluster/${spoke}" --timeout=300s
    kubectl --kubeconfig "${admin_kubeconfig}" wait --for=condition=ManagedClusterConditionAvailable \
        "managedcluster/${spoke}" --timeout=300s
    cat <<EOF | kubectl --kubeconfig "${admin_kubeconfig}" apply --validate=false -f -
apiVersion: work.open-cluster-management.io/v1
kind: ManifestWork
metadata:
  name: sql-hosted-smoke
  namespace: ${spoke}
spec:
  workload:
    manifests:
    - apiVersion: v1
      kind: ConfigMap
      metadata:
        name: sql-hosted-smoke
        namespace: default
      data:
        stored-by: kine-postgresql
EOF
done

addon_workload="${runtime_dir}/addon-workload.yaml"
[[ -s "${addon_workload}" ]] || cat >"${addon_workload}" <<'EOF'
apiVersion: v1
kind: ConfigMap
metadata:
  name: sql-poc-addon
  namespace: default
data:
  stored-by: kine-postgresql-addon-manager
EOF
clusteradm addon create sql-poc-addon --filename "${addon_workload}" --kubeconfig "${admin_kubeconfig}" --overwrite
clusteradm addon enable --names sql-poc-addon --clusters "${spokes[0]},${spokes[1]}" --kubeconfig "${admin_kubeconfig}"

for spoke in "${spokes[@]}"; do
    for resource in sql-hosted-smoke sql-poc-addon; do
        expected=kine-postgresql
        [[ "${resource}" == sql-poc-addon ]] && expected=kine-postgresql-addon-manager
        delivered=false
        for _ in $(seq 1 90); do
            value=$(kubectl --context "kind-${spoke}" -n default get configmap "${resource}" \
                -o jsonpath='{.data.stored-by}' 2>/dev/null || true)
            if [[ "${value}" == "${expected}" ]]; then delivered=true; break; fi
            sleep 2
        done
        [[ "${delivered}" == true ]] || fail "${resource} was not delivered to ${spoke}"
    done
done

echo "Hosted SQL-backed OCM POC resumed and verified."
echo "Host API remains etcd-backed; OCM API: ${internal_api}"
echo "Teardown: ${here}/local-down.sh"
