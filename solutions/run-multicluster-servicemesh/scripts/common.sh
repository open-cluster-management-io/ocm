#!/usr/bin/env bash

# Shared configuration for the multicluster service mesh solution.
# Override any value via environment variables before running the scripts.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SOLUTION_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
MANIFESTS_DIR="${SOLUTION_DIR}/manifests"

HUB="${HUB:-hub}"
CLUSTER1="${CLUSTER1:-cluster1}"
CLUSTER2="${CLUSTER2:-cluster2}"

CTX_HUB="${CTX_HUB:-kind-${HUB}}"
CTX_CLUSTER1="${CTX_CLUSTER1:-kind-${CLUSTER1}}"
CTX_CLUSTER2="${CTX_CLUSTER2:-kind-${CLUSTER2}}"

ISTIO_VERSION="${ISTIO_VERSION:-1.16.7}"
ISTIO_REVISION="${ISTIO_REVISION:-1-16-7}"
BOOKINFO_MANIFEST_URL="${BOOKINFO_MANIFEST_URL:-https://raw.githubusercontent.com/istio/istio/release-1.16/samples/bookinfo/platform/kube/bookinfo.yaml}"

MULTICLUSTER_MESH_REPO="${MULTICLUSTER_MESH_REPO:-https://github.com/open-cluster-management-io/multicluster-mesh.git}"
MULTICLUSTER_MESH_REF="${MULTICLUSTER_MESH_REF:-main}"
MULTICLUSTER_MESH_DIR="${MULTICLUSTER_MESH_DIR:-/tmp/multicluster-mesh}"

ADDON_NAMESPACE="${ADDON_NAMESPACE:-open-cluster-management-addon}"
ADDON_RELEASE="${ADDON_RELEASE:-multicluster-mesh}"

# KinD nodes use the host Docker platform. Multi-arch image tags (e.g. istio/*:1.16.7)
# break `kind load` on Apple Silicon unless we pull/tag a single-platform digest first.
KIND_PLATFORM_OS="${KIND_PLATFORM_OS:-linux}"
KIND_PLATFORM_ARCH="${KIND_PLATFORM_ARCH:-$(uname -m)}"
PRELOAD_IMAGES="${PRELOAD_IMAGES:-true}"

require_command() {
  local cmd="$1"
  if ! command -v "${cmd}" >/dev/null 2>&1; then
    echo "error: required command '${cmd}' not found in PATH" >&2
    exit 1
  fi
}

require_context() {
  local ctx="$1"
  if ! kubectl config get-contexts -o name | grep -qx "${ctx}"; then
    echo "error: kube context '${ctx}' not found. Run ../setup-dev-environment/local-up.sh first." >&2
    exit 1
  fi
}

wait_for_managedclusteraddons() {
  local timeout="${1:-10m}"
  local cluster
  local deadline
  deadline=$((SECONDS + $(parse_timeout_seconds "${timeout}")))

  # ManagedClusterAddon is created asynchronously by the addon manager after Helm install.
  # kubectl wait fails immediately with NotFound if the object does not exist yet.
  for cluster in "${CLUSTER1}" "${CLUSTER2}"; do
    echo "   waiting for managedclusteraddon/multicluster-mesh in namespace ${cluster}..."
    until kubectl --context "${CTX_HUB}" get managedclusteraddon/multicluster-mesh \
      -n "${cluster}" >/dev/null 2>&1; do
      if (( SECONDS >= deadline )); then
        echo "error: timed out waiting for managedclusteraddon/multicluster-mesh in ${cluster}." >&2
        exit 1
      fi
      sleep 2
    done
    kubectl --context "${CTX_HUB}" wait --for=condition=Available \
      managedclusteraddon/multicluster-mesh -n "${cluster}" --timeout="${timeout}"
  done
}

parse_timeout_seconds() {
  local t="$1"
  case "${t}" in
    *s) echo "${t%s}" ;;
    *m) echo $(( ${t%m} * 60 )) ;;
    *h) echo $(( ${t%h} * 3600 )) ;;
    *)  echo "${t}" ;;
  esac
}

wait_for_istio_control_plane() {
  local ctx="$1"
  local timeout="${2:-10m}"
  local deadline
  deadline=$((SECONDS + $(parse_timeout_seconds "${timeout}")))

  # MeshDeployment is reconciled asynchronously; istiod deployment may not exist yet.
  echo "   waiting for istiod deployment to appear on ${ctx}..."
  until kubectl --context "${ctx}" -n istio-system get deployment -l app=istiod \
    --no-headers 2>/dev/null | grep -q .; do
    if (( SECONDS >= deadline )); then
      echo "error: timed out waiting for istiod to appear on ${ctx}." >&2
      exit 1
    fi
    sleep 5
  done

  kubectl --context "${ctx}" -n istio-system wait --for=condition=Available \
    deployment -l app=istiod --timeout="${timeout}"

  echo "   waiting for istio-ingressgateway deployment to appear on ${ctx}..."
  until kubectl --context "${ctx}" -n istio-system get deployment/istio-ingressgateway \
    >/dev/null 2>&1; do
    if (( SECONDS >= deadline )); then
      echo "error: timed out waiting for istio-ingressgateway to appear on ${ctx}." >&2
      exit 1
    fi
    sleep 5
  done

  kubectl --context "${ctx}" -n istio-system wait --for=condition=Available \
    deployment/istio-ingressgateway --timeout="${timeout}"
}

wait_for_eastwest_gateway() {
  local ctx="$1"
  local timeout="${2:-10m}"
  local deadline
  deadline=$((SECONDS + $(parse_timeout_seconds "${timeout}")))

  # MeshFederation creates istio-eastwestgateway asynchronously; poll until it exists.
  echo "   waiting for istio-eastwestgateway deployment to appear on ${ctx}..."
  until kubectl --context "${ctx}" -n istio-system get deployment/istio-eastwestgateway \
    >/dev/null 2>&1; do
    if (( SECONDS >= deadline )); then
      echo "error: timed out waiting for istio-eastwestgateway to appear on ${ctx}." >&2
      exit 1
    fi
    sleep 5
  done

  kubectl --context "${ctx}" -n istio-system wait --for=condition=Available \
    deployment/istio-eastwestgateway --timeout="${timeout}"
}

require_istio_crds() {
  local ctx="$1"
  if ! kubectl --context "${ctx}" get crd serviceentries.networking.istio.io >/dev/null 2>&1; then
    echo "error: Istio CRDs are not installed on ${ctx}." >&2
    echo "hint: run ./setup-mesh-addon.sh first and wait for MeshDeployment to reconcile." >&2
    exit 1
  fi
}

get_reviews_v3_pod_ip() {
  # Get the IP of the Running reviews-v3 pod (not Terminating).
  # This must be refreshed after any pod restart.
  kubectl --context "${CTX_CLUSTER2}" -n bookinfo get pod \
    -l app=reviews,version=v3 \
    --field-selector=status.phase=Running \
    -o jsonpath='{.items[0].status.podIP}' 2>/dev/null
}

configure_cross_cluster_routing() {
  require_command envsubst

  echo "==> Configuring cross-cluster service discovery and traffic routing"

  # Get current reviews-v3 pod IP (must be refreshed after restarts for pure mesh routing)
  export REVIEW_V3_POD_IP
  REVIEW_V3_POD_IP="$(get_reviews_v3_pod_ip)"
  if [[ -z "${REVIEW_V3_POD_IP}" ]]; then
    echo "error: could not determine reviews-v3 pod IP on ${CTX_CLUSTER2}." >&2
    echo "hint: ensure reviews-v3 pod is Running in bookinfo namespace on cluster2." >&2
    exit 1
  fi
  echo "   reviews-v3 pod IP on cluster2: ${REVIEW_V3_POD_IP}"
  envsubst < "${MANIFESTS_DIR}/serviceentry-export-cluster2.yaml" | \
    kubectl --context "${CTX_CLUSTER2}" apply -f -

  export CLUSTER2_HOST_IP EASTWESTGW_NODEPORT
  CLUSTER2_HOST_IP="$(get_cluster_control_plane_ip "${CLUSTER2}")"
  EASTWESTGW_NODEPORT="$(kubectl --context "${CTX_CLUSTER2}" -n istio-system get svc istio-eastwestgateway \
    -o jsonpath='{.spec.ports[?(@.name=="tls")].nodePort}')"
  echo "   cluster2 east-west gateway: ${CLUSTER2_HOST_IP}:${EASTWESTGW_NODEPORT}"
  envsubst < "${MANIFESTS_DIR}/serviceentry-import-cluster1.yaml" | \
    kubectl --context "${CTX_CLUSTER1}" apply -f -

  kubectl --context "${CTX_CLUSTER2}" apply -f "${MANIFESTS_DIR}/destinationrule-cluster2.yaml"
  kubectl --context "${CTX_CLUSTER1}" apply -f "${MANIFESTS_DIR}/virtualservice-cluster1.yaml"

  # Allow Istio to propagate ServiceEntry updates before traffic checks.
  sleep 5
}

wait_for_bookinfo_pods() {
  local ctx="$1"
  local timeout="${2:-5m}"
  kubectl --context "${ctx}" -n bookinfo wait --for=condition=Ready pod -l app --timeout="${timeout}"
}

ensure_bookinfo_sidecars() {
  local ctx="$1"
  local timeout="${2:-5m}"

  # Only restart if any running pod is missing the Istio sidecar (container count < 2).
  # Avoids unnecessary pod churn on re-runs where sidecars are already injected.
  local missing_sidecar=false
  local pod_names
  pod_names="$(kubectl --context "${ctx}" -n bookinfo get pod -l app \
    --field-selector=status.phase=Running \
    -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || true)"

  for pod in ${pod_names}; do
    local containers
    containers="$(kubectl --context "${ctx}" -n bookinfo get pod "${pod}" \
      -o jsonpath='{.spec.containers[*].name}' | wc -w | tr -d ' ')"
    if [[ "${containers}" -lt 2 ]]; then
      missing_sidecar=true
      break
    fi
  done

  local deployments
  deployments="$(kubectl --context "${ctx}" -n bookinfo get deployment -o name 2>/dev/null || true)"

  if [[ "${missing_sidecar}" == "true" && -n "${deployments}" ]]; then
    echo "   restarting bookinfo deployments on ${ctx} to pick up Istio sidecar injection"
    while IFS= read -r deployment; do
      kubectl --context "${ctx}" -n bookinfo rollout restart "${deployment}"
    done <<< "${deployments}"
    while IFS= read -r deployment; do
      kubectl --context "${ctx}" -n bookinfo rollout status "${deployment}" --timeout="${timeout}"
    done <<< "${deployments}"
  else
    echo "   all bookinfo pods on ${ctx} already have Istio sidecars injected"
  fi

  local pod_names
  pod_names="$(kubectl --context "${ctx}" -n bookinfo get pod -l app \
    -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || true)"
  if [[ -z "${pod_names}" ]]; then
    echo "error: no bookinfo pods found on ${ctx}." >&2
    exit 1
  fi

  for pod in ${pod_names}; do
    local container_count
    container_count="$(kubectl --context "${ctx}" -n bookinfo get pod "${pod}" \
      -o jsonpath='{.spec.containers[*].name}' | wc -w | tr -d ' ')"
    if [[ "${container_count}" -lt 2 ]]; then
      echo "error: pod ${pod} on ${ctx} is missing Istio sidecar (${container_count} container(s))." >&2
      echo "hint: confirm namespace bookinfo has label istio.io/rev=${ISTIO_REVISION} and istiod is running." >&2
      exit 1
    fi
  done

  kubectl --context "${ctx}" -n bookinfo wait --for=condition=Ready pod -l app --timeout="${timeout}"
}

get_cluster_control_plane_ip() {
  local cluster_name="$1"
  docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "${cluster_name}-control-plane"
}

resolve_platform_digest() {
  local image="$1"
  local media_type

  media_type="$(docker buildx imagetools inspect "${image}" 2>/dev/null \
    --format '{{.Manifest.MediaType}}' || true)"
  if [[ "${media_type}" != *"manifest.list"* ]]; then
    # Single-arch image; plain docker pull is sufficient for kind load.
    return 0
  fi

  docker buildx imagetools inspect "${image}" 2>/dev/null \
    --format "{{range .Manifest.Manifests}}{{if and (eq .Platform.OS \"${KIND_PLATFORM_OS}\") (eq .Platform.Architecture \"${KIND_PLATFORM_ARCH}\")}}{{.Digest}}{{end}}{{end}}"
}

pull_single_platform_image() {
  local image="$1"
  local digest

  digest="$(resolve_platform_digest "${image}" || true)"
  if [[ -z "${digest}" ]]; then
    echo "   pulling ${image} (single-arch)"
    docker pull "${image}"
    return
  fi

  echo "   pulling ${image}@${digest} (${KIND_PLATFORM_OS}/${KIND_PLATFORM_ARCH})"
  docker pull "${image}@${digest}"
  docker tag "${image}@${digest}" "${image}"
}

load_image_into_kind() {
  local image="$1"
  local cluster="$2"
  kind load docker-image "${image}" --name "${cluster}"
}

preload_images_into_kind() {
  local cluster="$1"
  shift
  local images=("$@")

  if [[ "${PRELOAD_IMAGES}" != "true" ]]; then
    echo "==> Skipping image preload (PRELOAD_IMAGES=${PRELOAD_IMAGES})"
    return
  fi

  echo "==> Preloading images into KinD cluster '${cluster}' for ${KIND_PLATFORM_OS}/${KIND_PLATFORM_ARCH}"
  for image in "${images[@]}"; do
    pull_single_platform_image "${image}"
    load_image_into_kind "${image}" "${cluster}"
  done
}
