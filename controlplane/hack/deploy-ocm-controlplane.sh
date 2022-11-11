#!/usr/bin/env bash
# This script starts ocm control plane.
#     Example 1: hack/start-ocm-controlplane.sh
#     Example 2: hack/start-ocm-controlplane.sh false

KUBECTL=oc
KUSTOMIZE=kustomize
if [ ! $KUBECTL >& /dev/null ] ; then
      echo "Failed to run $KUBECTL. Please ensure $KUBECTL is installed"
  exit 1
fi
if [ ! $KUSTOMIZE >& /dev/null ] ; then
      echo "Failed to run $KUSTOMIZE. Please ensure $KUSTOMIZE is installed"
  exit 1
fi

KUBE_ROOT=$(pwd)
if [ ! $API_HOST ] ; then
    echo "API_HOST should be set"
    exit 1
fi

export OCM_CONFIG_DIRECTORY="$(pwd)/hack/deploy"
#  set port
SERVING_PORT=9443
# use embedded etcd if set to true
ENABLE_EMBEDDED_ETCD=${1:-true}
#
GO_OUT=${GO_OUT:-"${KUBE_ROOT}/bin"}
LOG_LEVEL=${LOG_LEVEL:-7}
# This is the default dir and filename where the apiserver will generate a self-signed cert
# which should be able to be used as the CA to verify itself
CERT_DIR=${CERT_DIR:-"${OCM_CONFIG_DIRECTORY}/cert"}

SERVICE_CLUSTER_IP_RANGE=${SERVICE_CLUSTER_IP_RANGE:-10.0.0.0/24}
FIRST_SERVICE_CLUSTER_IP=${FIRST_SERVICE_CLUSTER_IP:-10.0.0.1}
# owner of client certs, default to current user if not specified
USER=${USER:-$(whoami)}

WAIT_FOR_URL_API_SERVER=${WAIT_FOR_URL_API_SERVER:-60}
MAX_TIME_FOR_URL_API_SERVER=${MAX_TIME_FOR_URL_API_SERVER:-1}
ENABLE_DAEMON=${ENABLE_DAEMON:-false}

KUBELET_PROVIDER_ID=${KUBELET_PROVIDER_ID:-"$(hostname)"}
FEATURE_GATES=${FEATURE_GATES:-"DefaultClusterSet=true"}
STORAGE_BACKEND=${STORAGE_BACKEND:-"etcd3"}
STORAGE_MEDIA_TYPE=${STORAGE_MEDIA_TYPE:-"application/vnd.kubernetes.protobuf"}
# preserve etcd data. you also need to set ETCD_DIR.
PRESERVE_ETCD="${PRESERVE_ETCD:-false}"

# WebHook Authentication and Authorization
AUTHORIZATION_WEBHOOK_CONFIG_FILE=${AUTHORIZATION_WEBHOOK_CONFIG_FILE:-""}
AUTHENTICATION_WEBHOOK_CONFIG_FILE=${AUTHENTICATION_WEBHOOK_CONFIG_FILE:-""}

# Do not run the mutation detector by default on a local cluster.
# It is intended for a specific type of testing and inherently leaks memory.
KUBE_CACHE_MUTATION_DETECTOR="${KUBE_CACHE_MUTATION_DETECTOR:-false}"
export KUBE_CACHE_MUTATION_DETECTOR

# panic the server on watch decode errors since they are considered coder mistakes
KUBE_PANIC_WATCH_DECODE_ERROR="${KUBE_PANIC_WATCH_DECODE_ERROR:-true}"
export KUBE_PANIC_WATCH_DECODE_ERROR

# Default list of admission Controllers to invoke prior to persisting objects in cluster
# The order defined here does not matter.
ENABLE_ADMISSION_PLUGINS=${ENABLE_ADMISSION_PLUGINS:-"NamespaceLifecycle,LimitRanger,ServiceAccount,MutatingAdmissionWebhook,ValidatingAdmissionWebhook,ResourceQuota"}
DISABLE_ADMISSION_PLUGINS=${DISABLE_ADMISSION_PLUGINS:-"TaintNodesByCondition,Priority,DefaultTolerationSeconds,DefaultStorageClass,PodSecurity,PersistentVolumeClaimResize,RuntimeClass,DefaultIngressClass"}


# Stop right away if the build fails
set -e

source "${KUBE_ROOT}/hack/lib/init.sh"
kube::util::ensure-gnu-sed

# Shut down anyway if there's an error.
set +e

API_PORT=${API_PORT:-0}
API_SECURE_PORT=${API_SECURE_PORT:-$SERVING_PORT}
API_HOST=${API_HOST:-""}
API_HOST_IP=${API_HOST_IP:-$SERVING_IP}
ADVERTISE_ADDRESS=${ADVERTISE_ADDRESS:-""}
NODE_PORT_RANGE=${NODE_PORT_RANGE:-""}
API_BIND_ADDR=${API_BIND_ADDR:-"0.0.0.0"}
EXTERNAL_HOSTNAME=${EXTERNAL_HOSTNAME:-""}
# TODO(ycyaoxdu): should allowe all origins?
API_CORS_ALLOWED_ORIGINS=${API_CORS_ALLOWED_ORIGINS:-/(.*)+$}

# Use to increase verbosity on particular files, e.g. LOG_SPEC=token_controller*=5,other_controller*=4
LOG_SPEC=${LOG_SPEC:-""}
LOG_DIR=${LOG_DIR:-"/tmp"}
ROOT_CA_FILE=${CERT_DIR}/serving-kube-apiserver.crt
CLUSTER_SIGNING_CERT_FILE=${CLUSTER_SIGNING_CERT_FILE:-"${CERT_DIR}/client-ca.crt"}
CLUSTER_SIGNING_KEY_FILE=${CLUSTER_SIGNING_KEY_FILE:-"${CERT_DIR}/client-ca.key"}
# Reuse certs will skip generate new ca/cert files under CERT_DIR
# it's useful with PRESERVE_ETCD=true because new ca will make existed service account secrets invalided
REUSE_CERTS=${REUSE_CERTS:-false}


# Ensure CERT_DIR is created for auto-generated crt/key and kubeconfig
mkdir -p "${CERT_DIR}" &>/dev/null || sudo mkdir -p "${CERT_DIR}"
CONTROLPLANE_SUDO=$(test -w "${CERT_DIR}" || echo "sudo -E")


function set_service_accounts {
    SERVICE_ACCOUNT_LOOKUP=${SERVICE_ACCOUNT_LOOKUP:-true}
    SERVICE_ACCOUNT_KEY="${CERT_DIR}/kube-serviceaccount.key"
    # Generate ServiceAccount key if needed
    if [[ ! -f "${SERVICE_ACCOUNT_KEY}" ]]; then
      mkdir -p "$(dirname "${SERVICE_ACCOUNT_KEY}")"
      openssl genrsa -out "${SERVICE_ACCOUNT_KEY}" 2048 2>/dev/null
    fi
}

function generate_certs {
    kube::util::create_signing_certkey "${CONTROLPLANE_SUDO}" "${CERT_DIR}" server '"server auth"'
    kube::util::create_signing_certkey "${CONTROLPLANE_SUDO}" "${CERT_DIR}" client '"client auth"'
        
    # Create auth proxy client ca
    kube::util::create_signing_certkey "${CONTROLPLANE_SUDO}" "${CERT_DIR}" request-header '"client auth"'
    
    # serving cert for kube-apiserver
    kube::util::create_serving_certkey "${CONTROLPLANE_SUDO}" "${CERT_DIR}" "server-ca" kube-apiserver kubernetes.default kubernetes.default.svc "localhost" "${API_HOST_IP}" "${API_HOST}" "${FIRST_SERVICE_CLUSTER_IP}"
    
    # Create client certs signed with client-ca, given id, given CN and a number of groups
    kube::util::create_client_certkey "${CONTROLPLANE_SUDO}" "${CERT_DIR}" 'client-ca' admin system:admin system:masters
    kube::util::create_client_certkey "${CONTROLPLANE_SUDO}" "${CERT_DIR}" 'client-ca' kube-apiserver kube-apiserver
    
    # Create matching certificates for kube-aggregator
    kube::util::create_serving_certkey "${CONTROLPLANE_SUDO}" "${CERT_DIR}" "server-ca" kube-aggregator api.kube-public.svc "localhost" "${API_HOST_IP}"
    kube::util::create_client_certkey "${CONTROLPLANE_SUDO}" "${CERT_DIR}" request-header-ca auth-proxy system:auth-proxy
    
    # TODO remove masters and add rolebinding
    kube::util::create_client_certkey "${CONTROLPLANE_SUDO}" "${CERT_DIR}" 'client-ca' kube-aggregator system:kube-aggregator system:masters
    # TODO(ycyaoxdu): should write data( rather than) file in kubeconfig
    kube::util::write_client_kubeconfig "${CONTROLPLANE_SUDO}" "${CERT_DIR}" "${ROOT_CA_FILE}" "${API_HOST}" "${API_SECURE_PORT}" kube-aggregator
}

function start_apiserver {
    if [[ "${REUSE_CERTS}" != true ]]; then
      # Create Certs
      generate_certs
    fi

    cp ${CERT_DIR}/kube-aggregator.kubeconfig ${CERT_DIR}/kubeconfig
    sed -i "s@$OCM_CONFIG_DIRECTORY@/controlplane@g" ${CERT_DIR}/kube-aggregator.kubeconfig
    sed -i 's/9443/443/g' ${CERT_DIR}/kubeconfig
    ${KUSTOMIZE} build hack/deploy | ${KUBECTL} apply -f -

    echo "Use '${KUBECTL} --kubeconfig=${CERT_DIR}/kubeconfig' to use the aggregated API server" 
}


kube::util::test_openssl_installed
kube::util::ensure-cfssl

set_service_accounts
start_apiserver
