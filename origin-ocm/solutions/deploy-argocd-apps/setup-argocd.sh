#!/bin/bash
set -e

# login to Argo CD
echo "Login to Argo CD"
admin_pass=$(kubectl -n argocd get secret argocd-initial-admin-secret -o=jsonpath='{.data.password}' | base64 -d)
argocd login localhost:8080 --username=admin --password="${admin_pass}" --insecure

# register the two OCM managed clusters to Argo CD
echo "Register cluster1 and cluster2 to Argo CD"
argocd cluster add kind-cluster1 --name=cluster1
argocd cluster add kind-cluster2 --name=cluster2

# grant permission to Argo CD to access OCM placement API
echo "Grant permission to Argo CD to access OCM placement API"
kubectl apply -f ./manifests/ocm-placement-consumer-role.yaml
kubectl apply -f ./manifests/ocm-placement-consumer-rolebinding.yaml

# bind the global clusterset which includes all OCM managed clusters to argocd namespace
echo "Bind the global clusterset to argocd namespace"
clusteradm clusterset bind global --namespace argocd

# create the configuration of the OCM Placement generator
echo "Create the OCM Placement generator"
kubectl apply -f ./manifests/ocm-placement-generator-cm.yaml

# list registered clusters
argocd cluster list
