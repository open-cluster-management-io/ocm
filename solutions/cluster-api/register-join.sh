#!/bin/bash

cd $(dirname ${BASH_SOURCE})

set -e

CLUSTER_NAME=$1

echo "get bootstrap token from ocm hub cluster"
joincmd=$(clusteradm get token --use-bootstrap-token | grep clusteradm)

echo "Join $1 to hub"
$(echo ${joincmd} --dry-run --output-file join.yaml  | sed "s/<cluster_name>/$CLUSTER_NAME/g")
kubectl create secret generic import-secret-$1 --from-file=join.yaml --type=addons.cluster.x-k8s.io/resource-set

cat << EOF | kubectl apply -f -
apiVersion: addons.cluster.x-k8s.io/v1alpha3
kind: ClusterResourceSet
metadata:
 name: import-$CLUSTER_NAME
spec:
 strategy: "ApplyOnce"
 clusterSelector:
   matchLabels:
     cluster.x-k8s.io/cluster-name: $CLUSTER_NAME
 resources:
   - name: import-secret
     kind: Secret
EOF
