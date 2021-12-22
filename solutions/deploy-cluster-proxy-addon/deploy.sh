#!/bin/bash

cd $(dirname ${BASH_SOURCE})

set -e

hubctx="kind-hub"

kubectl config use ${hubctx}

echo "Deploy cluster=proxy addon"
helm repo add ocm https://open-cluster-management.oss-us-west-1.aliyuncs.com
helm repo update
helm install \
    -n open-cluster-management-addon --create-namespace \
    cluster-proxy ocm/cluster-proxy 

