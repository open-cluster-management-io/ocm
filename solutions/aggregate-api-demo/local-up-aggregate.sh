#!/bin/bash
# Copyright Contributors to the Open Cluster Management project
#
# Demo variant of solutions/setup-dev-environment/local-up.sh that runs the
# same three-cluster OCM flow with the hub's OCM APIs served by the SQL-backed
# aggregate API server POC (poc/aggregate-api-server) instead of CRDs+etcd.
#
# The hub is initialized normally with clusteradm (CRDs and controllers are
# installed), then the POC takes over the CRD-backed local APIServices so the
# selected OCM group/versions are served from PostgreSQL. CRDs remain
# installed but shadowed. Kubernetes-native resources (Secrets, ConfigMaps,
# Leases, CSRs, RBAC, ...) stay in the hub's etcd.
#
# Requires: kind, kubectl, clusteradm, openssl, docker or podman.
# All clusters are disposable; never run this against a real hub.

set -e
cd "$(dirname "${BASH_SOURCE[0]}")"

hub=${HUB:-agg-hub}
c1=${CLUSTER1:-agg-cluster1}
c2=${CLUSTER2:-agg-cluster2}
local_cluster=local-cluster
poc_dir="$(cd ../../poc/aggregate-api-server && pwd)"
archive=/tmp/ocm-api-poc-image.tar

hubctx="kind-${hub}"
c1ctx="kind-${c1}"
c2ctx="kind-${c2}"
original_context=$(kubectl config current-context 2>/dev/null || true)

for tool in kind kubectl clusteradm openssl; do
  command -v "$tool" >/dev/null || {
    echo "'${tool}' is required. See solutions/setup-dev-environment/README.md for clusteradm." >&2
    exit 1
  }
done

engine=$(command -v docker 2>/dev/null || command -v podman 2>/dev/null || true)
[[ -n "$engine" ]] || {
  echo "docker or podman is required to build the POC image" >&2
  exit 1
}
image=ocm-aggregate-api-poc:dev
if [[ "$engine" == *podman* ]]; then
  export KIND_EXPERIMENTAL_PROVIDER=podman
  # containerd imports archive tags verbatim and CRI resolves short names to
  # docker.io/library/<name>, so load a docker-qualified tag under podman.
  image=docker.io/library/ocm-aggregate-api-poc:dev
fi

restore_context() {
  if [[ -n "$original_context" ]]; then
    kubectl config use-context "$original_context" >/dev/null
    echo "Restored kubectl context to '$original_context'."
  fi
}
trap restore_context EXIT

echo "=== 1/9 creating kind clusters ${hub}, ${c1}, ${c2} ==="
# Reuse existing clusters so the demo can resume after a partial failure.
# If creation fails with "could not find a log line that matches ... cgroup v1",
# the host has run out of inotify instances; the upstream dev-environment
# README suggests raising fs.inotify.max_user_instances.
for cluster in "${hub}" "${c1}" "${c2}"; do
  if kind get clusters 2>/dev/null | grep -qx "$cluster"; then
    echo "Reusing kind cluster ${cluster}"
  else
    kind create cluster --name "$cluster"
  fi
done

echo "=== 2/9 initializing the OCM hub on ${hubctx} (CRDs + controllers) ==="
clusteradm init --wait --context "${hubctx}"

echo "=== 3/9 waiting for CRD-backed local APIServices ==="
for group_version in $(grep -vE '^[[:space:]]*(#|$)' "${poc_dir}/internal/server/apiservices.txt"); do
  group="${group_version%/*}"
  version="${group_version##*/}"
  # internal.open-cluster-management.io is not installed by clusteradm; the
  # POC creates its APIService directly.
  [[ "$group" == internal.open-cluster-management.io ]] && continue
  apiservice="${version}.${group}"
  found=0
  for _ in $(seq 1 60); do
    if kubectl --context "${hubctx}" get apiservice "$apiservice" >/dev/null 2>&1; then
      found=1
      break
    fi
    sleep 2
  done
  if [[ "$found" != 1 ]]; then
    echo "Local APIService ${apiservice} never appeared after clusteradm init." >&2
    exit 1
  fi
done
echo "All CRD-backed APIServices are present."

echo "=== 4/9 building and loading the POC image into ${hub} ==="
make -C "${poc_dir}" image >/dev/null
if [[ "$engine" == *podman* ]]; then
  rm -f "$archive"
  "$engine" save -o "$archive" "$image"
  kind load image-archive "$archive" --name "${hub}"
else
  kind load docker-image "$image" --name "${hub}"
fi

echo "=== 5/9 deploying the POC aggregate API server (takeover mode) ==="
OCM_API_POC_IMAGE="$image" OCM_API_POC_PULL_POLICY=Never \
  OCM_API_POC_NAMESPACE=ocm-api-poc OCM_API_POC_DB_PASSWORD="$(openssl rand -hex 16)" \
  OCM_API_POC_KUBECONTEXT="${hubctx}" OCM_API_POC_TAKEOVER=yes \
  bash "${poc_dir}/deploy/install.sh"

echo "=== 6/9 restarting hub controllers to re-sync against PostgreSQL ==="
# Informers established before the takeover keep streaming from the CRD
# handler; a rollout restart forces a fresh LIST/WATCH through the aggregate
# server. The hub controllers run in open-cluster-management-hub; the operator
# and the local-cluster klusterlet run in open-cluster-management.
for ns in open-cluster-management-hub open-cluster-management; do
  if kubectl --context "${hubctx}" get namespace "$ns" >/dev/null 2>&1; then
    kubectl --context "${hubctx}" -n "$ns" rollout restart deployment
    kubectl --context "${hubctx}" -n "$ns" rollout status deployment --timeout=300s
  fi
done

echo "=== 7/9 joining ${c1}, ${c2}, and ${local_cluster} to the hub ==="
joincmd=$(clusteradm get token --context "${hubctx}" | grep "clusteradm join")
$(echo "${joincmd}" --force-internal-endpoint-lookup --wait --context "${c1ctx}" | sed "s/<cluster_name>/${c1}/g")
$(echo "${joincmd}" --force-internal-endpoint-lookup --wait --context "${c2ctx}" | sed "s/<cluster_name>/${c2}/g")
$(echo "${joincmd}" --force-internal-endpoint-lookup --wait --context "${hubctx}" | sed "s/<cluster_name>/${local_cluster}/g")

echo "=== 8/9 accepting the managed clusters ==="
clusteradm accept --context "${hubctx}" --clusters "${c1},${c2},${local_cluster}" --wait

echo "=== 9/9 verifying registration and work delivery ==="
for cluster in "${c1}" "${c2}" "${local_cluster}"; do
  kubectl --context "${hubctx}" wait --for=condition=ManagedClusterJoined "managedcluster/${cluster}" --timeout=600s
  kubectl --context "${hubctx}" wait --for=condition=ManagedClusterConditionAvailable "managedcluster/${cluster}" --timeout=600s
done
kubectl label managedcluster "${local_cluster}" local-cluster=true --overwrite --context "${hubctx}" --validate=false

# End-to-end ManifestWork: create on the hub (stored in PostgreSQL), applied
# on the spoke by the work agent watching through the aggregate server.
cat <<EOF | kubectl --context "${hubctx}" apply --validate=false -f -
apiVersion: work.open-cluster-management.io/v1
kind: ManifestWork
metadata:
  name: aggregate-demo-work
  namespace: ${c1}
spec:
  workload:
    manifests:
    - apiVersion: v1
      kind: ConfigMap
      metadata:
        name: aggregate-demo-configmap
        namespace: default
      data:
        served-by: postgresql-backed-aggregate-api
EOF
applied=0
for _ in $(seq 1 60); do
  if kubectl --context "${c1ctx}" -n default get configmap aggregate-demo-configmap >/dev/null 2>&1; then
    applied=1
    break
  fi
  sleep 2
done
if [[ "$applied" != 1 ]]; then
  echo "ManifestWork was not applied on ${c1}: aggregate-demo-configmap is missing." >&2
  exit 1
fi
echo "ManifestWork applied on ${c1}: ConfigMap aggregate-demo-configmap exists."

kubectl get managedclusters --context "${hubctx}"

echo
echo "Demo is ready. Object counts:"
./count-resources.sh "${hubctx}"

echo
echo "Inspect with:  kubectl --context ${hubctx} get managedclusters,manifestworks"
echo "PostgreSQL:    kubectl --context ${hubctx} -n ocm-api-poc exec deploy/ocm-api-poc-postgres -- psql -U manifestwork_poc -d manifestwork_poc"
echo "Tear down:     ./teardown.sh   (or kind delete clusters ${hub} ${c1} ${c2})"
