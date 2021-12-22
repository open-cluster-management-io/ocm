#!/bin/bash

cd $(dirname ${BASH_SOURCE})

set -e

hubctx="kind-hub"

kubectl config use ${hubctx}

echo "Setup provision LoadBalance typed service support"
kubectl apply -f supports/ns 

kubectl create secret generic -n metallb-system memberlist --from-literal=secretkey="$(openssl rand -base64 128)" 

kubectl apply -f supports/metallb 