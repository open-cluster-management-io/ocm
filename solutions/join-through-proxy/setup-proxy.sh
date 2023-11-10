#!/bin/bash
set -e

proxy=${HUB_NAME:-proxy}
proxyctx="kind-${proxy}"

# create server certificate for proxy server
rm -rf tls
mkdir tls
openssl genrsa -out tls/root-ca.key 2048
openssl req -key tls/root-ca.key -subj "/CN=example.com" -new -x509 -days 3650 -out tls/root-ca.crt
openssl genrsa -out tls/squid.key 2048
openssl req -new -key tls/squid.key -subj "/CN=${proxy}-control-plane" -out tls/squid.csr
cat config/server-cert.conf | sed "s/<host_name>/$proxy-control-plane/g" > tls/server-cert.conf
openssl x509 -req -in tls/squid.csr -CA tls/root-ca.crt -CAkey tls/root-ca.key -CAcreateserial -out tls/squid.crt -days 3600 -extensions v3_ext -extfile tls/server-cert.conf
cat tls/squid.crt tls/squid.key >> tls/squid-cert-key.pem

kind create cluster --name "${proxy}"

kubectl config use ${proxyctx}
echo "setup proxy server - squid"
kubectl create ns squid
kubectl -n squid create secret generic squid-cert-key --from-file=squid-cert-key.pem=tls/squid-cert-key.pem
kubectl -n squid create configmap squid-config --from-file=squid=config/squid.conf
kubectl apply -f manifests/squid-deploy.yaml
kubectl apply -f manifests/squid-service.yaml

kubectl -n squid get pods
