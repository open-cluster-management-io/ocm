#!/usr/bin/env bash

#################################
# include the -=magic=-
# you can pass command line args
#
# example:
# to disable simulated typing
# . ../demo-magic.sh -d
#
# pass -h to see all options
#################################
. ./demo-magic.sh


########################
# Configure the options
########################

#
# speed at which to simulate typing. bigger num = faster
#
TYPE_SPEED=40

#
# custom prompt
#
# see http://www.tldp.org/HOWTO/Bash-Prompt-HOWTO/bash-prompt-escape-sequences.html for escape sequences
#
DEMO_PROMPT="${GREEN}➜ ${CYAN}\W ${COLOR_RESET}"
ROOT_DIR="$(pwd)"
number=${1:-$1}
export IMAGE_NAME="quay.io/clyang82/controlplane:latest"

# this is needed for the controlplane deploy
echo "* Testing connection"
HOST_URL=$(oc -n openshift-console get routes console -o jsonpath='{.status.ingress[0].routerCanonicalHostname}')
if [ $? -ne 0 ]; then
    echo "ERROR: Make sure you are logged into an OpenShift Container Platform before running this script"
    exit
fi

# shorten to the basedomain
DEFAULT_HOST_POSTFIX=${HOST_URL/#router-default./}
HOST_POSTFIX=${HOST_POSTFIX:-$DEFAULT_HOST_POSTFIX}

if [[ "$2" == "clean" ]]; then
  for i in $(seq 1 "${number}"); do
    namespace=ocm-controlplane-$i
    oc delete ns $namespace
    kind delete cluster --name $namespace-mc1
    rm -rf ${ROOT_DIR}/../deploy/cert-${namespace}
  done
  oc delete -k multicluster-global-hub-lite/deploy/server -n default
  rm -rf multicluster-global-hub-lite
  exit
fi

# text color
# DEMO_CMD_COLOR=$BLACK

# hide the evidence
clear

for i in $(seq 1 "${number}"); do

  # put your demo awesomeness here
  namespace=ocm-controlplane-$i
  p "deploy standalone controlplane and addons(workmgr and managedserviceaccount) in namespace ${namespace}"
  export HUB_NAME="${namespace}"
  API_HOST="ocm-controlplane-${HUB_NAME}.${HOST_POSTFIX}"
  pei "cd ../.. && make deploy-all"
  cd ${ROOT_DIR}
  pei "oc get pod -n ${namespace}"

  CERTS_DIR=${ROOT_DIR}/../deploy/cert-${namespace}
  p "create a KinD cluster as a managedcluster"
  pei "kind create cluster --name $namespace-mc1 --kubeconfig ${CERTS_DIR}/mc1-kubeconfig"

  output=$(clusteradm --kubeconfig=${CERTS_DIR}/kubeconfig get token --use-bootstrap-token)
  token=$(echo $output | awk -F ' ' '{print $1}' | awk -F '=' '{print $2}')
  p "join to the control plane"
  pei "clusteradm --kubeconfig=${CERTS_DIR}/mc1-kubeconfig join --hub-token $token --hub-apiserver https://$API_HOST --cluster-name $namespace-mc1"
  PROMPT_TIMEOUT=10
  wait
  pei "clusteradm --kubeconfig=${CERTS_DIR}/kubeconfig accept --clusters $namespace-mc1"

  pei "oc --kubeconfig=${CERTS_DIR}/kubeconfig get managedcluster"
  
  PROMPT_TIMEOUT=10
  wait
  pei "oc --kubeconfig=${CERTS_DIR}/kubeconfig get managedclusteraddon -n $namespace-mc1"

done

# show a prompt so as not to reveal our true nature after
# the demo has concluded

p "deploy the global hub in default namespace"
rm -rf multicluster-global-hub-lite
git clone git@github.com:clyang82/multicluster-global-hub-lite.git
pei "oc apply -k multicluster-global-hub-lite/deploy/server -n default"

for i in $(seq 1 "${number}"); do

  namespace=ocm-controlplane-$i
  p "deploy syncer into namespace ${namespace}"
  SECRETNAME=`oc get sa multicluster-global-hub-apiserver-sa -ojsonpath="{.secrets[0].name}" -n default`
  TOKEN=`oc get secret $SECRETNAME -n default -ojsonpath="{.data.token}" | base64 -d`
  APISERVER=`oc get route multicluster-global-hub-apiserver -ojsonpath="{.spec.host}" -n default`
  oc --kubeconfig /tmp/kubeconfig config set-credentials apiserver-user --token=$TOKEN
  oc --kubeconfig /tmp/kubeconfig config set-cluster multicluster-global-hub-apiserver --server=https://$APISERVER --insecure-skip-tls-verify=true
  oc --kubeconfig /tmp/kubeconfig config set-context global-hub-apiserver --user=apiserver-user --cluster=multicluster-global-hub-apiserver
  oc --kubeconfig /tmp/kubeconfig config use-context global-hub-apiserver
  oc create secret generic multicluster-global-hub-kubeconfig --from-file=kubeconfig=/tmp/kubeconfig -n ${namespace}
  # temporarily to apply policy crds into standalone controlplane
  CERTS_DIR=${ROOT_DIR}/../deploy/cert-${namespace}
  oc --kubeconfig ${CERTS_DIR}/kubeconfig apply -f multicluster-global-hub-lite/server/manifests/0000_00_policy.open-cluster-management.io_policies.crd.yaml

  pei "oc apply -n ${namespace} -k multicluster-global-hub-lite/deploy/syncer"

done

oc get secret -oyaml multicluster-global-hub-kubeconfig -ojsonpath="{.data.kubeconfig}" | base64 -d > /tmp/global-hub-kubeconfig
p "Use oc --kubeconfig /tmp/global-hub-kubeconfig to access the global hub"

p ""