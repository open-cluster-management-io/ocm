#!/bin/bash

set -o nounset
set -o pipefail

KUBECTL=${KUBECTL:-kubectl}

rm -rf registration-operator

echo "############  Cloning registration-operator"
git clone https://github.com/open-cluster-management-io/registration-operator.git

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

echo "############  ManagedCluster klusterlet is installed successfully!!"

echo "############  Cleanup"
cd ../ || exist
rm -rf registration-operator

echo "############  Finished installation!!!"
