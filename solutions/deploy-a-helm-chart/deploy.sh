#!/bin/bash

cd $(dirname ${BASH_SOURCE})

set -e

hubctx="kind-hub"

kubectl config use ${hubctx}

echo "Bind clusterset to default namespace"

clusteradm clusterset bind default --namespace default

echo "Label cluster1 so placement will select cluster1 only"

kubectl label managedcluster cluster1 purpose=test --overwrite

echo "Deploy the application with placement"

kubectl apply -f manifests/
