#!/bin/bash

cd $(dirname ${BASH_SOURCE})

set -e

hubctx="kind-hub"

kubectl config use ${hubctx}

helm repo add ocm https://open-cluster-management.oss-us-west-1.aliyuncs.com

helm repo update

echo "Start installing addon..."

helm install -n open-cluster-management-addon --create-namespace managed-serviceaccount ocm/managed-serviceaccount