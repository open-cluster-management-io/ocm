#!/usr/bin/env bash
# This script starts ocm control plane on local.
#     Example 1: hack/start-ocm-controlplane.sh
#     Example 2: hack/start-ocm-controlplane.sh false

KUBE_ROOT=$(pwd)
# export SERVING_IP=$(ifconfig en0 | grep -Eo 'inet (addr:)?([0-9]*\.){3}[0-9]*' | grep -Eo '([0-9]*\.){3}[0-9]*') # ***en0 for macOS***
# export SERVING_IP=$(ifconfig eth0 | grep -Eo 'inet (addr:)?([0-9]*\.){3}[0-9]*' | grep -Eo '([0-9]*\.){3}[0-9]*') # ***eth0 for Linux***
if [ ! $SERVING_IP ] ; then
    echo "SERVING_IP should be set"
    exit 1
fi

# set root dir
OCM_CONFIG_DIRECTORY=${OCM_CONFIG_DIRECTORY:-".ocmconfig"}
#  set port
SERVING_PORT=9443
# use embedded etcd if set to true
ENABLE_EMBEDDED_ETCD=${1:-true}
# must be set if ENABLE_EMBEDDED_ETCD is set to false
ETCD_HOST=${ETCD_HOST:-"localhost"}
ETCD_PORT=${ETCD_PORT:-"2379"}
#
GO_OUT=${GO_OUT:-"${KUBE_ROOT}/bin"}
LOG_LEVEL=${LOG_LEVEL:-7}
# This is the default dir and filename where the apiserver will generate a self-signed cert
# which should be able to be used as the CA to verify itself
CERT_DIR=${CERT_DIR:-"${OCM_CONFIG_DIRECTORY}/cert"}

DENY_SECURITY_CONTEXT_ADMISSION=${DENY_SECURITY_CONTEXT_ADMISSION:-""}
SERVICE_CLUSTER_IP_RANGE=${SERVICE_CLUSTER_IP_RANGE:-10.0.0.0/24}
FIRST_SERVICE_CLUSTER_IP=${FIRST_SERVICE_CLUSTER_IP:-10.0.0.1}
# owner of client certs, default to current user if not specified
USER=${USER:-$(whoami)}

WAIT_FOR_URL_API_SERVER=${WAIT_FOR_URL_API_SERVER:-60}
MAX_TIME_FOR_URL_API_SERVER=${MAX_TIME_FOR_URL_API_SERVER:-1}

FEATURE_GATES=${FEATURE_GATES:-"DefaultClusterSet=true"}
STORAGE_BACKEND=${STORAGE_BACKEND:-"etcd3"}
# preserve etcd data. you also need to set ETCD_DIR.
PRESERVE_ETCD="${PRESERVE_ETCD:-false}"

# RBAC Mode options
AUTHORIZATION_MODE=${AUTHORIZATION_MODE:-"RBAC"}
# Default list of admission Controllers to invoke prior to persisting objects in cluster
# The order defined here does not matter.
ENABLE_ADMISSION_PLUGINS=${ENABLE_ADMISSION_PLUGINS:-"NamespaceLifecycle,ServiceAccount,MutatingAdmissionWebhook,ValidatingAdmissionWebhook,ResourceQuota"}
DISABLE_ADMISSION_PLUGINS=${DISABLE_ADMISSION_PLUGINS:-""}

# Stop right away if the build fails
set -e

source "${KUBE_ROOT}/hack/lib/init.sh"
kube::util::ensure-gnu-sed

# Shut down anyway if there's an error.
set +e

API_PORT=${API_PORT:-0}
API_SECURE_PORT=${API_SECURE_PORT:-$SERVING_PORT}
API_HOST=${API_HOST:-$SERVING_IP}
API_HOST_IP=${API_HOST_IP:-$SERVING_IP}
API_BIND_ADDR=${API_BIND_ADDR:-"0.0.0.0"}

LOG_DIR=${LOG_DIR:-"/tmp"}
ROOT_CA_FILE="server-ca.crt"
# Reuse certs will skip generate new ca/cert files under CERT_DIR
# it's useful with PRESERVE_ETCD=true because new ca will make existed service account secrets invalided
REUSE_CERTS=${REUSE_CERTS:-false}


# Ensure CERT_DIR is created for auto-generated crt/key and kubeconfig
mkdir -p "${CERT_DIR}" &>/dev/null || sudo mkdir -p "${CERT_DIR}"
CONTROLPLANE_SUDO=$(test -w "${CERT_DIR}" || echo "sudo -E")

function test_apiserver_off {
    # For the common local scenario, fail fast if server is already running.
    # this can happen if you run start-ocm-controlplane.sh twice and kill etcd in between.
    if [[ "${API_PORT}" -gt "0" ]]; then
        if ! curl --silent -g "${API_HOST}:${API_PORT}" ; then
            echo "API SERVER insecure port is free, proceeding..."
        else
            echo "ERROR starting API SERVER, exiting. Some process on ${API_HOST} is serving already on ${API_PORT}"
            exit 1
        fi
    fi
    
    if ! curl --silent -k -g "${API_HOST}:${API_SECURE_PORT}" ; then
        echo "API SERVER secure port is free, proceeding..."
    else
        echo "ERROR starting API SERVER, exiting. Some process on ${API_HOST} is serving already on ${API_SECURE_PORT}"
        exit 1
    fi
}

cleanup()
{
    echo "Cleaning up..."
    # Check if the API server is still running
    [[ -n "${APISERVER_PID-}" ]] && kube::util::read-array APISERVER_PIDS < <(pgrep -P "${APISERVER_PID}" ; ps -o pid= -p "${APISERVER_PID}")
    [[ -n "${APISERVER_PIDS-}" ]] && sudo kill "${APISERVER_PIDS[@]}" 2>/dev/null
    exit 0
}

function healthcheck {
    if [[ -n "${APISERVER_PID-}" ]] && ! sudo kill -0 "${APISERVER_PID}" 2>/dev/null; then
        warning_log "API server terminated unexpectedly, see ${APISERVER_LOG}"
        APISERVER_PID=
    fi
}

function print_color {
    message=$1
    prefix=${2:+$2: } # add colon only if defined
    color=${3:-1}     # default is red
    echo -n "$(tput bold)$(tput setaf "${color}")"
    echo "${prefix}${message}"
    echo -n "$(tput sgr0)"
}

function warning_log {
    print_color "$1" "W$(date "+%m%d %H:%M:%S")]" 1
}

function start_etcd {
    echo "etcd starting..."
    export ETCD_LOGFILE=${LOG_DIR}/etcd.log
    kube::etcd::start
}

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
    # Create CA signers
    if [[ "${ENABLE_SINGLE_CA_SIGNER:-}" = true ]]; then
        kube::util::create_signing_certkey "${CONTROLPLANE_SUDO}" "${CERT_DIR}" server '"client auth","server auth"'
        sudo cp "${CERT_DIR}/server-ca.key" "${CERT_DIR}/client-ca.key"
        sudo cp "${CERT_DIR}/server-ca.crt" "${CERT_DIR}/client-ca.crt"
        sudo cp "${CERT_DIR}/server-ca-config.json" "${CERT_DIR}/client-ca-config.json"
    else
        kube::util::create_signing_certkey "${CONTROLPLANE_SUDO}" "${CERT_DIR}" server '"server auth"'
        kube::util::create_signing_certkey "${CONTROLPLANE_SUDO}" "${CERT_DIR}" client '"client auth"'
    fi
    
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
    kube::util::write_client_kubeconfig "${CONTROLPLANE_SUDO}" "${CERT_DIR}" "${ROOT_CA_FILE}" "${API_HOST}" "${API_SECURE_PORT}" kube-aggregator
}

function start_apiserver {
    security_admission=""
    if [[ -n "${DENY_SECURITY_CONTEXT_ADMISSION}" ]]; then
        security_admission=",SecurityContextDeny"
    fi
    
    # Append security_admission plugin
    ENABLE_ADMISSION_PLUGINS="${ENABLE_ADMISSION_PLUGINS}${security_admission}"
    
    authorizer_arg=""
    if [[ -n "${AUTHORIZATION_MODE}" ]]; then
        authorizer_arg="--authorization-mode=${AUTHORIZATION_MODE}"
    fi
 
    if [[ "${REUSE_CERTS}" != true ]]; then
        # Create Certs
        generate_certs
    fi

    APISERVER_LOG=${LOG_DIR}/kube-apiserver.log
    ${CONTROLPLANE_SUDO} "${GO_OUT}/ocm-controlplane" \
    "${authorizer_arg}"  \
    --v="${LOG_LEVEL}" \
    --enable-bootstrap-token-auth \
    --enable-priority-and-fairness="false" \
    --api-audiences="" \
    --external-hostname="${API_HOST}" \
    --client-ca-file="${CERT_DIR}/client-ca.crt" \
    --client-key-file="${CERT_DIR}/client-ca.key" \
    --service-account-key-file="${SERVICE_ACCOUNT_KEY}" \
    --service-account-lookup="${SERVICE_ACCOUNT_LOOKUP}" \
    --service-account-issuer="https://kubernetes.default.svc" \
    --service-account-signing-key-file="${SERVICE_ACCOUNT_KEY}" \
    --enable-admission-plugins="${ENABLE_ADMISSION_PLUGINS}" \
    --disable-admission-plugins="${DISABLE_ADMISSION_PLUGINS}" \
    --bind-address="${API_BIND_ADDR}" \
    --secure-port="${API_SECURE_PORT}" \
    --tls-cert-file="${CERT_DIR}/serving-kube-apiserver.crt" \
    --tls-private-key-file="${CERT_DIR}/serving-kube-apiserver.key" \
    --storage-backend="${STORAGE_BACKEND}" \
    --feature-gates="${FEATURE_GATES}" \
    --enable-embedded-etcd="${ENABLE_EMBEDDED_ETCD}" \
    --etcd-servers="http://${ETCD_HOST}:${ETCD_PORT}" \
    --service-cluster-ip-range="${SERVICE_CLUSTER_IP_RANGE}" >"${APISERVER_LOG}" 2>&1 &
    APISERVER_PID=$!
    
    echo "Waiting for apiserver to come up"
    kube::util::wait_for_url "https://${API_HOST_IP}:${API_SECURE_PORT}/healthz" "apiserver: " 1 "${WAIT_FOR_URL_API_SERVER}" "${MAX_TIME_FOR_URL_API_SERVER}" \
    || { echo "check apiserver logs: ${APISERVER_LOG}" ; exit 1 ; }
    
    echo "use 'kubectl --kubeconfig=${CERT_DIR}/kube-aggregator.kubeconfig' to use the aggregated API server"
    
}

echo "environment checking..."
# validate that etcd is: not running, in path, and has minimum required version.
if [[ "${ENABLE_EMBEDDED_ETCD}" = false ]]; then
    echo "etcd validate"
    kube::etcd::validate
fi
#
test_apiserver_off
kube::util::test_openssl_installed
kube::util::ensure-cfssl

trap cleanup EXIT


echo "Starting services now!"
if [[ "${ENABLE_EMBEDDED_ETCD}" = false ]]; then
    start_etcd
fi
set_service_accounts
start_apiserver

while true; do sleep 1; healthcheck; done
