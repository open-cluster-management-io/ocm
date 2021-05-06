#!/bin/bash

set -o nounset
set -o pipefail

KUBECTL=${KUBECTL:-kubectl}

KLUSTERLET_KUBECONFIG_CONTEXT=$($KUBECTL config current-context)
KIND_CLUSTER=kind

# On openshift, OLM is installed into openshift-operator-lifecycle-manager
$KUBECTL get namespace openshift-operator-lifecycle-manager 1>/dev/null 2>&1
if [ $? -eq 0 ]; then
  export OLM_NAMESPACE=openshift-operator-lifecycle-manager
fi

rm -rf registration-operator

echo "############  Cloning registration-operator"
git clone https://github.com/open-cluster-management/registration-operator.git

cd registration-operator || {
  printf "cd failed, registration-operator does not exist"
  return 1
}

echo "############  Deploying klusterlet"
make deploy-spoke
if [ $? -ne 0 ]; then
 echo "############  Failed to deploy klusterlet"
 exit 1
fi

for i in {1..7}; do
  echo "############$i  Checking klusterlet-registration-agent"
  RUNNING_POD=$($KUBECTL -n open-cluster-management-agent get pods | grep klusterlet-registration-agent | grep -c "Running")
  if [ ${RUNNING_POD} -ge 1 ]; then
    break
  fi

  if [ $i -eq 7 ]; then
    echo "!!!!!!!!!!  the klusterlet-registration-agent is not ready within 3 minutes"
    $KUBECTL -n open-cluster-management-agent get pods
    exit 1
  fi
  sleep 30

done

for i in {1..7}; do
  echo "############$i  Checking managed cluster "
  $KUBECTL get managedclusters cluster1 1>/dev/null 2>&1
  if [ $? -eq 0 ]; then
    break
  fi

  if [ $i -eq 7 ]; then
    echo "!!!!!!!!!!  the managed cluster is not created within 3 minutes"
    $KUBECTL get ns
    exit 1
  fi
  sleep 30

done

CSR_NAME=$($KUBECTL get csr |grep cluster1 | grep Pending |awk '{print $1}')
$KUBECTL certificate approve "${CSR_NAME}"
$KUBECTL patch managedclusters cluster1  --type merge --patch '{"spec":{"hubAcceptsClient":true}}'

for i in {1..7}; do
  echo "############$i  Checking managed cluster namespace is created"
  $KUBECTL get ns cluster1 1>/dev/null 2>&1
  if [ $? -eq 0 ]; then
    break
  fi

  if [ $i -eq 7 ]; then
    echo "!!!!!!!!!!  the managed cluster namespace is not created within 3 minutes"
    $KUBECTL get ns
    exit 1
  fi
  sleep 30

done

echo "############  ManagedCluster klusterlet is installed successfully!!"

echo "############  Cleanup"
cd ../ || exist
rm -rf registration-operator

echo "############  Finished installation!!!"
