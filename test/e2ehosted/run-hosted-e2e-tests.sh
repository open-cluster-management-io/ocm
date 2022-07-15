#!/bin/bash
###############################################################################
# Copyright Contributors to the Open Cluster Management project
###############################################################################

set -o errexit
set -o nounset

function wait_deployment() {
  set +e
  for((i=0;i<30;i++));
  do
    ${KUBECTL} -n $1 get deploy $2
    if [ 0 -eq $? ]; then
      break
    fi
    echo "sleep 1 second to wait deployment $1/$2 to exist: $i"
    sleep 1
  done
  set -e
}

function hub_approve_cluster(){
  local cluster_name=$1
  echo "approve ${cluster_name}"
  ${KUBECTL} get csr -l open-cluster-management.io/cluster-name=${cluster_name} | \
    grep -v NAME | awk '{print $1}' | xargs ${KUBECTL} certificate approve
  ${KUBECTL} patch managedcluster "${cluster_name}" -p='{"spec":{"hubAcceptsClient":true}}' --type=merge
  ${KUBECTL} get managedcluster "${cluster_name}"
}


BUILD_DIR="$( cd "$(dirname "$0")" >/dev/null 2>&1 ; pwd -P )"
E2E_DIR="$(dirname "$BUILD_DIR")"
REPO_DIR="$(dirname "$E2E_DIR")"
WORK_DIR="${REPO_DIR}/_output"

KIND_VERSION="v0.11.1"
KIND="${WORK_DIR}/bin/kind"

KUBE_VERSION="v1.20.2"
KUBECTL="${WORK_DIR}/bin/kubectl"

export MANAGED_CLUSTER_NAME="hub"
export HOSTED_MANAGED_CLUSTER_NAME="managed"

mkdir -p "${WORK_DIR}/bin"
mkdir -p "${WORK_DIR}/config"

echo "###### installing kind"
curl -s -f \
  -L "https://github.com/kubernetes-sigs/kind/releases/download/${KIND_VERSION}/kind-${GOHOSTOS}-${GOHOSTARCH}" \
  -o "${KIND}"
chmod +x "${KIND}"

CLEAN_ARG=${1:-unclean}
if [ "$CLEAN_ARG"x = "clean"x ]; then
    ${KIND} delete cluster --name ${MANAGED_CLUSTER_NAME}
    ${KIND} delete cluster --name ${HOSTED_MANAGED_CLUSTER_NAME}
    exit 0
fi

echo "###### installing kubectl"
curl -s -f \
  -L "https://storage.googleapis.com/kubernetes-release/release/${KUBE_VERSION}/bin/${GOHOSTOS}/${GOHOSTARCH}/kubectl" \
  -o "${KUBECTL}"
chmod +x "${KUBECTL}"


echo "###### installing e2e test cluster : ${WORK_DIR}/kubeconfig"
export KUBECONFIG="${WORK_DIR}/kubeconfig"
${KIND} delete cluster --name ${MANAGED_CLUSTER_NAME}
${KIND} create cluster --image kindest/node:${KUBE_VERSION} --name ${MANAGED_CLUSTER_NAME}
cluster_ip=$(${KUBECTL} get svc kubernetes -n default -o jsonpath="{.spec.clusterIP}")
cluster_context=$(${KUBECTL} config current-context)
# scale replicas to 1 to save resources
${KUBECTL} --context="${cluster_context}" -n kube-system scale --replicas=1 deployment/coredns

echo "###### loading image: ${EXAMPLE_IMAGE_NAME}"
${KIND} load docker-image ${EXAMPLE_IMAGE_NAME} --name ${MANAGED_CLUSTER_NAME}

echo "###### deploy registration-operator"
rm -rf "$WORK_DIR/registration-operator"
git clone https://github.com/open-cluster-management-io/registration-operator.git "$WORK_DIR/registration-operator"
${KUBECTL} apply -k "$WORK_DIR/registration-operator/deploy/cluster-manager/config/manifests"
${KUBECTL} apply -k "$WORK_DIR/registration-operator/deploy/cluster-manager/config/samples"
${KUBECTL} apply -k "$WORK_DIR/registration-operator/deploy/klusterlet/config/manifests"
rm -rf "$WORK_DIR/registration-operator"

cat << EOF | ${KUBECTL} apply -f -
apiVersion: operator.open-cluster-management.io/v1
kind: Klusterlet
metadata:
  name: klusterlet
spec:
  deployOption:
    mode: Default
  registrationImagePullSpec: quay.io/open-cluster-management/registration
  workImagePullSpec: quay.io/open-cluster-management/work
  clusterName: ${MANAGED_CLUSTER_NAME}
  namespace: open-cluster-management-agent
  externalServerURLs:
  - url: https://localhost
  registrationConfiguration:
    featureGates:
    - feature: AddonManagement
      mode: Enable
EOF

wait_deployment open-cluster-management cluster-manager
${KUBECTL} -n open-cluster-management rollout status deploy cluster-manager --timeout=120s

wait_deployment open-cluster-management klusterlet
${KUBECTL} -n open-cluster-management rollout status deploy klusterlet --timeout=120s

wait_deployment open-cluster-management-hub cluster-manager-registration-controller
${KUBECTL} -n open-cluster-management-hub rollout status deploy cluster-manager-registration-controller --timeout=120s
${KUBECTL} -n open-cluster-management-hub rollout status deploy cluster-manager-registration-webhook --timeout=120s
${KUBECTL} -n open-cluster-management-hub rollout status deploy cluster-manager-work-webhook --timeout=120s


# scale replicas to save resources, after the hub are installed, we don't need
# the cluster-manager and placement-controller for the e2e test
${KUBECTL} -n open-cluster-management scale --replicas=0 deployment/cluster-manager
${KUBECTL} -n open-cluster-management-hub scale --replicas=0 deployment/cluster-manager-placement-controller
# scale replicas to save resources
${KUBECTL} -n open-cluster-management scale --replicas=1 deployment/klusterlet

wait_deployment open-cluster-management-agent klusterlet-registration-agent
echo "###### prepare bootstrap hub secret"
cp "${KUBECONFIG}" "${WORK_DIR}"/e2e-kubeconfig
${KUBECTL} config set "clusters.${cluster_context}.server" "https://${cluster_ip}" \
  --kubeconfig "${WORK_DIR}"/e2e-kubeconfig
${KUBECTL} delete secret bootstrap-hub-kubeconfig -n open-cluster-management-agent --ignore-not-found
${KUBECTL} create secret generic bootstrap-hub-kubeconfig \
  --from-file=kubeconfig="${WORK_DIR}"/e2e-kubeconfig \
  -n open-cluster-management-agent
${KUBECTL} -n open-cluster-management-agent rollout status deploy klusterlet-registration-agent --timeout=120s
${KUBECTL} -n open-cluster-management-agent rollout status deploy klusterlet-work-agent --timeout=120s

hub_approve_cluster ${MANAGED_CLUSTER_NAME}

# prepare another managed cluster for hosted mode testing
echo "###### installing e2e test managed cluster"
export KUBECONFIG="${WORK_DIR}/kubeconfig"
${KIND} delete cluster --name ${HOSTED_MANAGED_CLUSTER_NAME}
${KIND} create cluster --image kindest/node:${KUBE_VERSION} --name ${HOSTED_MANAGED_CLUSTER_NAME}
cluster_context_managed=$(${KUBECTL} config current-context)
echo "managed cluster context is: ${cluster_context_managed}"
# scale replicas to 1 to save resources
${KUBECTL} --context="${cluster_context_managed}" -n kube-system scale --replicas=1 deployment/coredns

echo "###### loading image: ${EXAMPLE_IMAGE_NAME}"
${KIND} load docker-image ${EXAMPLE_IMAGE_NAME} --name ${HOSTED_MANAGED_CLUSTER_NAME}

echo "###### prepare bootstrap hub and external managed kubeconfig for hosted cluster"
${KIND} get kubeconfig --name=${HOSTED_MANAGED_CLUSTER_NAME} --internal > "${WORK_DIR}"/e2e-managed-kubeconfig
${KIND} get kubeconfig --name=${HOSTED_MANAGED_CLUSTER_NAME} > "${WORK_DIR}"/e2e-managed-kubeconfig-public
${KUBECTL} config use-context "${cluster_context}"

export HOSTED_MANAGED_KLUSTERLET_NAME="managed"
${KUBECTL} create ns ${HOSTED_MANAGED_KLUSTERLET_NAME}
cat << EOF | ${KUBECTL} apply -f -
apiVersion: operator.open-cluster-management.io/v1
kind: Klusterlet
metadata:
  name: ${HOSTED_MANAGED_KLUSTERLET_NAME}
spec:
  deployOption:
    mode: Hosted
  registrationImagePullSpec: quay.io/open-cluster-management/registration
  workImagePullSpec: quay.io/open-cluster-management/work
  clusterName: ${HOSTED_MANAGED_CLUSTER_NAME}
  namespace: open-cluster-management-agent
  externalServerURLs:
  - url: https://localhost
  registrationConfiguration:
    featureGates:
    - feature: AddonManagement
      mode: Enable
EOF


${KUBECTL} delete secret bootstrap-hub-kubeconfig -n ${HOSTED_MANAGED_KLUSTERLET_NAME} --ignore-not-found
${KUBECTL} create secret generic bootstrap-hub-kubeconfig \
  --from-file=kubeconfig="${WORK_DIR}"/e2e-kubeconfig \
  -n ${HOSTED_MANAGED_KLUSTERLET_NAME}

${KUBECTL} delete secret external-managed-kubeconfig -n ${HOSTED_MANAGED_KLUSTERLET_NAME} --ignore-not-found
${KUBECTL} create secret generic external-managed-kubeconfig \
  --from-file=kubeconfig="${WORK_DIR}"/e2e-managed-kubeconfig \
  -n ${HOSTED_MANAGED_KLUSTERLET_NAME}

export HOSTED_MANAGED_KUBECONFIG_SECRET_NAME=e2e-hosted-managed-kubeconfig
${KUBECTL} delete secret ${HOSTED_MANAGED_KUBECONFIG_SECRET_NAME} \
  -n ${HOSTED_MANAGED_KLUSTERLET_NAME} --ignore-not-found
${KUBECTL} create secret generic ${HOSTED_MANAGED_KUBECONFIG_SECRET_NAME} \
  --from-file=kubeconfig="${WORK_DIR}"/e2e-managed-kubeconfig-public \
  -n ${HOSTED_MANAGED_KLUSTERLET_NAME}

wait_deployment ${HOSTED_MANAGED_KLUSTERLET_NAME} ${HOSTED_MANAGED_KLUSTERLET_NAME}-registration-agent
wait_deployment ${HOSTED_MANAGED_KLUSTERLET_NAME} ${HOSTED_MANAGED_KLUSTERLET_NAME}-work-agent

${KUBECTL} -n ${HOSTED_MANAGED_KLUSTERLET_NAME} rollout status deploy \
  ${HOSTED_MANAGED_KLUSTERLET_NAME}-registration-agent --timeout=120s
${KUBECTL} -n ${HOSTED_MANAGED_KLUSTERLET_NAME} rollout status deploy \
  ${HOSTED_MANAGED_KLUSTERLET_NAME}-work-agent --timeout=120s

hub_approve_cluster ${HOSTED_MANAGED_CLUSTER_NAME}

${KUBECTL} wait --for=condition=ManagedClusterConditionAvailable=true \
  managedcluster/${MANAGED_CLUSTER_NAME} --timeout=120s
${KUBECTL} wait --for=condition=ManagedClusterConditionAvailable=true \
  managedcluster/${HOSTED_MANAGED_CLUSTER_NAME} --timeout=120s
echo "######## clusters are prepared completed!"

# install hellowrold addon controller
cat << EOF | ${KUBECTL} apply -f -
apiVersion: v1
data:
  EXAMPLE_IMAGE_NAME: ${EXAMPLE_IMAGE_NAME}
kind: ConfigMap
metadata:
  name: image-config
  namespace: open-cluster-management
EOF

${KUBECTL} apply -f "${REPO_DIR}"/examples/deploy/addon/resources -n open-cluster-management
${KUBECTL} set image -n open-cluster-management deployment/helloworld-controller \
  helloworld-controller="${EXAMPLE_IMAGE_NAME}"
${KUBECTL} set image -n open-cluster-management deployment/helloworldhelm-controller \
  helloworldhelm-controller="${EXAMPLE_IMAGE_NAME}"
${KUBECTL} set image -n open-cluster-management deployment/helloworldhosted-controller \
  helloworldhosted-controller="${EXAMPLE_IMAGE_NAME}"

# start the e2e test
"${REPO_DIR}"/e2ehosted.test -test.v -ginkgo.v
