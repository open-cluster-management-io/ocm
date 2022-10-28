#!/usr/bin/env bash
# This script starts ocm control plane.
#     Example 1: hack/start-ocm-controlplane.sh
#     Example 2: hack/start-ocm-controlplane.sh false

KUBE_ROOT=$(pwd)
# ensure ifconfig is installed 
if ! ifconfig >& /dev/null; then
  echo "Failed to run ifconfig. Please ensure ifconfig is installed"
  exit 1
fi
# use ifconfig to get local IP address here
SERVING_IP=$(ifconfig eth0 | grep -Eo 'inet (addr:)?([0-9]*\.){3}[0-9]*' | grep -Eo '([0-9]*\.){3}[0-9]*')
#  set port
SERVING_PORT=44444
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
CERT_DIR=${CERT_DIR:-"${KUBE_ROOT}/.ocmconfig/cert"}
# set runtime config to disable some apis 
RUNTIME_CONFIG=${RUNTIME_CONFIG:-""}


ALLOW_PRIVILEGED=${ALLOW_PRIVILEGED:-""}
DENY_SECURITY_CONTEXT_ADMISSION=${DENY_SECURITY_CONTEXT_ADMISSION:-""}
SERVICE_CLUSTER_IP_RANGE=${SERVICE_CLUSTER_IP_RANGE:-10.0.0.0/24}
FIRST_SERVICE_CLUSTER_IP=${FIRST_SERVICE_CLUSTER_IP:-10.0.0.1}
# owner of client certs, default to current user if not specified
USER=${USER:-$(whoami)}

# required for cni installation
CNI_CONFIG_DIR=${CNI_CONFIG_DIR:-/etc/cni/net.d}
CNI_PLUGINS_VERSION=${CNI_PLUGINS_VERSION:-"v1.0.1"}
CNI_TARGETARCH=${CNI_TARGETARCH:-amd64}
CNI_PLUGINS_TARBALL="${CNI_PLUGINS_VERSION}/cni-plugins-linux-${CNI_TARGETARCH}-${CNI_PLUGINS_VERSION}.tgz"
CNI_PLUGINS_URL="https://github.com/containernetworking/plugins/releases/download/${CNI_PLUGINS_TARBALL}"
CNI_PLUGINS_AMD64_SHA256SUM=${CNI_PLUGINS_AMD64_SHA256SUM:-"5238fbb2767cbf6aae736ad97a7aa29167525dcd405196dfbc064672a730d3cf"}
CNI_PLUGINS_ARM64_SHA256SUM=${CNI_PLUGINS_ARM64_SHA256SUM:-"2d4528c45bdd0a8875f849a75082bc4eafe95cb61f9bcc10a6db38a031f67226"}
CNI_PLUGINS_PPC64LE_SHA256SUM=${CNI_PLUGINS_PPC64LE_SHA256SUM:-"f078e33067e6daaef3a3a5010d6440f2464b7973dec3ca0b5d5be22fdcb1fd96"}
CNI_PLUGINS_S390X_SHA256SUM=${CNI_PLUGINS_S390X_SHA256SUM:-"468d33e16440d9ca4395c6bb2d5b71b35ae4a4df26301e4da85ac70c5ce56822"}

WAIT_FOR_URL_API_SERVER=${WAIT_FOR_URL_API_SERVER:-60}
MAX_TIME_FOR_URL_API_SERVER=${MAX_TIME_FOR_URL_API_SERVER:-1}
ENABLE_DAEMON=${ENABLE_DAEMON:-false}

EXTERNAL_CLOUD_PROVIDER=${EXTERNAL_CLOUD_PROVIDER:-false}
EXTERNAL_CLOUD_PROVIDER_BINARY=${EXTERNAL_CLOUD_PROVIDER_BINARY:-""}
EXTERNAL_CLOUD_VOLUME_PLUGIN=${EXTERNAL_CLOUD_VOLUME_PLUGIN:-""}
CONFIGURE_CLOUD_ROUTES=${CONFIGURE_CLOUD_ROUTES:-true}
CLOUD_CTLRMGR_FLAGS=${CLOUD_CTLRMGR_FLAGS:-""}
CLOUD_PROVIDER=${CLOUD_PROVIDER:-""}
CLOUD_CONFIG=${CLOUD_CONFIG:-""}
KUBELET_PROVIDER_ID=${KUBELET_PROVIDER_ID:-"$(hostname)"}
FEATURE_GATES=${FEATURE_GATES:-"DefaultClusterSet=true"}
STORAGE_BACKEND=${STORAGE_BACKEND:-"etcd3"}
STORAGE_MEDIA_TYPE=${STORAGE_MEDIA_TYPE:-"application/vnd.kubernetes.protobuf"}
# preserve etcd data. you also need to set ETCD_DIR.
PRESERVE_ETCD="${PRESERVE_ETCD:-false}"

# RBAC Mode options
AUTHORIZATION_MODE=${AUTHORIZATION_MODE:-"Node,RBAC"}

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
ADMISSION_CONTROL_CONFIG_FILE=${ADMISSION_CONTROL_CONFIG_FILE:-""}

# Audit policy
AUDIT_POLICY_FILE=${AUDIT_POLICY_FILE:-""}

# sanity check for OpenStack provider
if [ "${CLOUD_PROVIDER}" == "openstack" ]; then
    if [ "${CLOUD_CONFIG}" == "" ]; then
        echo "Missing CLOUD_CONFIG env for OpenStack provider!"
        exit 1
    fi
    if [ ! -f "${CLOUD_CONFIG}" ]; then
        echo "Cloud config ${CLOUD_CONFIG} doesn't exist"
        exit 1
    fi
fi

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
ADVERTISE_ADDRESS=${ADVERTISE_ADDRESS:-""}
NODE_PORT_RANGE=${NODE_PORT_RANGE:-""}
API_BIND_ADDR=${API_BIND_ADDR:-"0.0.0.0"}
EXTERNAL_HOSTNAME=${EXTERNAL_HOSTNAME:-""}
# TODO(ycyaoxdu): should allowe all origins?
API_CORS_ALLOWED_ORIGINS=${API_CORS_ALLOWED_ORIGINS:-/(.*)+$}

# Use to increase verbosity on particular files, e.g. LOG_SPEC=token_controller*=5,other_controller*=4
LOG_SPEC=${LOG_SPEC:-""}
LOG_DIR=${LOG_DIR:-"/tmp"}
ROOT_CA_FILE=${CERT_DIR}/server-ca.crt
CLUSTER_SIGNING_CERT_FILE=${CLUSTER_SIGNING_CERT_FILE:-"${CERT_DIR}/client-ca.crt"}
CLUSTER_SIGNING_KEY_FILE=${CLUSTER_SIGNING_KEY_FILE:-"${CERT_DIR}/client-ca.key"}
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
    SERVICE_ACCOUNT_KEY="${KUBE_ROOT}/.ocmconfig/sak/kube-serviceaccount.key"
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
    priv_arg=""
    if [[ -n "${ALLOW_PRIVILEGED}" ]]; then
      priv_arg="--allow-privileged=${ALLOW_PRIVILEGED}"
    fi

    runtime_config=""
    if [[ -n "${RUNTIME_CONFIG}" ]]; then
      runtime_config="--runtime-config=${RUNTIME_CONFIG}"
    fi

    # Let the API server pick a default address when API_HOST_IP
    # is set to 127.0.0.1
    advertise_address=""
    if [[ "${API_HOST_IP}" != "127.0.0.1" ]]; then
        advertise_address="--advertise-address=${API_HOST_IP}"
    fi
    if [[ "${ADVERTISE_ADDRESS}" != "" ]] ; then
        advertise_address="--advertise-address=${ADVERTISE_ADDRESS}"
    fi
    node_port_range=""
    if [[ "${NODE_PORT_RANGE}" != "" ]] ; then
        node_port_range="--service-node-port-range=${NODE_PORT_RANGE}"
    fi

    if [[ "${REUSE_CERTS}" != true ]]; then
      # Create Certs
      generate_certs
    fi

    cloud_config_arg="--cloud-provider=${CLOUD_PROVIDER} --cloud-config=${CLOUD_CONFIG}"
    if [[ "${EXTERNAL_CLOUD_PROVIDER:-}" == "true" ]]; then
      cloud_config_arg="--cloud-provider=external"
    fi

    if [[ -z "${EGRESS_SELECTOR_CONFIG_FILE:-}" ]]; then
      cat <<EOF > /tmp/kube_egress_selector_configuration.yaml
apiVersion: apiserver.k8s.io/v1beta1
kind: EgressSelectorConfiguration
egressSelections:
- name: cluster
  connection:
    proxyProtocol: Direct
- name: controlplane
  connection:
    proxyProtocol: Direct
- name: etcd
  connection:
    proxyProtocol: Direct
EOF
      EGRESS_SELECTOR_CONFIG_FILE="/tmp/kube_egress_selector_configuration.yaml"
    fi

    if [[ -z "${AUDIT_POLICY_FILE}" ]]; then
      cat <<EOF > /tmp/kube-audit-policy-file
# Log all requests at the Metadata level.
apiVersion: audit.k8s.io/v1
kind: Policy
rules:
- level: Metadata
EOF
      AUDIT_POLICY_FILE="/tmp/kube-audit-policy-file"
    fi

    APISERVER_LOG=${LOG_DIR}/kube-apiserver.log
    ${CONTROLPLANE_SUDO} "${GO_OUT}/ocm-controlplane" "${authorizer_arg}" "${priv_arg}" ${runtime_config} \
      ${cloud_config_arg} \
      "${advertise_address}" \
      "${node_port_range}" \
      --v="${LOG_LEVEL}" \
      --enable-bootstrap-token-auth \
      --enable-priority-and-fairness="false" \
      --vmodule="${LOG_SPEC}" \
      --audit-policy-file="${AUDIT_POLICY_FILE}" \
      --audit-log-path="${LOG_DIR}/kube-apiserver-audit.log" \
      --authorization-webhook-config-file="${AUTHORIZATION_WEBHOOK_CONFIG_FILE}" \
      --authentication-token-webhook-config-file="${AUTHENTICATION_WEBHOOK_CONFIG_FILE}" \
      --api-audiences="" \
      --cert-dir="${CERT_DIR}" \
      --egress-selector-config-file="${EGRESS_SELECTOR_CONFIG_FILE:-}" \
      --client-ca-file="${CERT_DIR}/client-ca.crt" \
      --kubelet-client-certificate="${CERT_DIR}/client-kube-apiserver.crt" \
      --kubelet-client-key="${CERT_DIR}/client-kube-apiserver.key" \
      --service-account-key-file="${SERVICE_ACCOUNT_KEY}" \
      --service-account-lookup="${SERVICE_ACCOUNT_LOOKUP}" \
      --service-account-issuer="https://kubernetes.default.svc" \
      --service-account-jwks-uri="https://kubernetes.default.svc/openid/v1/jwks" \
      --service-account-signing-key-file="${SERVICE_ACCOUNT_KEY}" \
      --enable-admission-plugins="${ENABLE_ADMISSION_PLUGINS}" \
      --disable-admission-plugins="${DISABLE_ADMISSION_PLUGINS}" \
      --admission-control-config-file="${ADMISSION_CONTROL_CONFIG_FILE}" \
      --bind-address="${API_BIND_ADDR}" \
      --secure-port="${API_SECURE_PORT}" \
      --tls-cert-file="${CERT_DIR}/serving-kube-apiserver.crt" \
      --tls-private-key-file="${CERT_DIR}/serving-kube-apiserver.key" \
      --storage-backend="${STORAGE_BACKEND}" \
      --storage-media-type="${STORAGE_MEDIA_TYPE}" \
      --feature-gates="${FEATURE_GATES}" \
      --enable-embedded-etcd="${ENABLE_EMBEDDED_ETCD}" \
      --etcd-servers="http://${ETCD_HOST}:${ETCD_PORT}" \
      --service-cluster-ip-range="${SERVICE_CLUSTER_IP_RANGE}" \
      --external-hostname="${EXTERNAL_HOSTNAME}" \
      --requestheader-username-headers=X-Remote-User \
      --requestheader-group-headers=X-Remote-Group \
      --requestheader-extra-headers-prefix=X-Remote-Extra- \
      --requestheader-client-ca-file="${CERT_DIR}/request-header-ca.crt" \
      --requestheader-allowed-names=system:auth-proxy \
      --proxy-client-cert-file="${CERT_DIR}/client-auth-proxy.crt" \
      --proxy-client-key-file="${CERT_DIR}/client-auth-proxy.key" \
      --cors-allowed-origins="${API_CORS_ALLOWED_ORIGINS}" >"${APISERVER_LOG}" 2>&1 &
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
if [[ "${ENABLE_DAEMON}" = false ]]; then
  trap cleanup EXIT
fi

echo "Starting services now!"
if [[ "${ENABLE_EMBEDDED_ETCD}" = false ]]; then 
  start_etcd
fi
set_service_accounts
start_apiserver

if [[ "${ENABLE_DAEMON}" = false ]]; then
  while true; do sleep 1; healthcheck; done
fi
