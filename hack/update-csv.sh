#!/bin/bash

set -e

# The steps to update the operator bundle manifests:
# Run: RELEASED_CSV_VERSION=<last released csv version> CSV_VERSION=<will release csv version> make update-csv
# Example: RELEASED_CSV_VERSION=0.15.0 CSV_VERSION=0.16.0 make update-csv
# Move the olm-catalog/latest manifests to github.com/k8s-operatorhub/community-operators and open PRs to update the operator.
# https://github.com/k8s-operatorhub/community-operators/tree/main/operators/cluster-manager
# https://github.com/k8s-operatorhub/community-operators/tree/main/operators/klusterlet
# Can find the operators from https://operatorhub.io/operator/cluster-manager and https://operatorhub.io/operator/klusterlet

REPO_ROOT=$(realpath "$(dirname "${BASH_SOURCE[0]}")"/..)
BINDIR=$REPO_ROOT/_output/tools/bin

DEPLOY_DIR=$REPO_ROOT/deploy
CLUSTER_MANAGER_DIR=$DEPLOY_DIR/cluster-manager
KLUSTERLET_DIR=$DEPLOY_DIR/klusterlet

SED_CMD="sed"
if [[ "$(uname)" == "Darwin" ]]; then
    if command -v gsed >/dev/null 2>&1; then
        SED_CMD="gsed"
    else
        echo "Error: gsed not found. Please install it with 'brew install gnu-sed'" >&2
        exit 1
    fi
fi

NAMESPACE='open-cluster-management'

# update config from helm charts
$BINDIR/helm template $CLUSTER_MANAGER_DIR/chart/cluster-manager --namespace=$NAMESPACE -s templates/operator.yaml > $CLUSTER_MANAGER_DIR/config/operator/operator.yaml
$BINDIR/helm template $CLUSTER_MANAGER_DIR/chart/cluster-manager --namespace=$NAMESPACE -s templates/service_account.yaml > $CLUSTER_MANAGER_DIR/config/operator/service_account.yaml
$BINDIR/helm template $CLUSTER_MANAGER_DIR/chart/cluster-manager --namespace=$NAMESPACE -s templates/cluster_role.yaml > $CLUSTER_MANAGER_DIR/config/rbac/cluster_role.yaml
$BINDIR/helm template $CLUSTER_MANAGER_DIR/chart/cluster-manager --namespace=$NAMESPACE -s templates/cluster_role_binding.yaml > $CLUSTER_MANAGER_DIR/config/rbac/cluster_role_binding.yaml

$BINDIR/helm template $KLUSTERLET_DIR/chart/klusterlet --namespace=$NAMESPACE -s templates/operator.yaml > $KLUSTERLET_DIR/config/operator/operator.yaml
$BINDIR/helm template $KLUSTERLET_DIR/chart/klusterlet --namespace=$NAMESPACE -s templates/service_account.yaml > $KLUSTERLET_DIR/config/operator/service_account.yaml
$BINDIR/helm template $KLUSTERLET_DIR/chart/klusterlet --namespace=$NAMESPACE -s templates/cluster_role.yaml > $KLUSTERLET_DIR/config/rbac/cluster_role.yaml
$BINDIR/helm template $KLUSTERLET_DIR/chart/klusterlet --namespace=$NAMESPACE -s templates/cluster_role_binding.yaml > $KLUSTERLET_DIR/config/rbac/cluster_role_binding.yaml


# update the replaces to released version in csv
$SED_CMD -i "s/cluster-manager\.v[0-9]\+\.[0-9]\+\.[0-9]\+/cluster-manager\.v$RELEASED_CSV_VERSION/g" $DEPLOY_DIR/cluster-manager/config/manifests/bases/cluster-manager.clusterserviceversion.yaml
$SED_CMD -i "s/klusterlet\.v[0-9]\+\.[0-9]\+\.[0-9]\+/klusterlet\.v$RELEASED_CSV_VERSION/g" $DEPLOY_DIR/klusterlet/config/manifests/bases/klusterlet.clusterserviceversion.yaml

# generate current csv bundle files
cd $DEPLOY_DIR/cluster-manager && $BINDIR/operator-sdk generate bundle --version $CSV_VERSION --package cluster-manager --channels stable --default-channel stable --input-dir config --output-dir olm-catalog/latest
cd $DEPLOY_DIR/klusterlet && $BINDIR/operator-sdk generate bundle --version $CSV_VERSION --package klusterlet --channels stable --default-channel stable --input-dir config --output-dir olm-catalog/latest

cd $REPO_ROOT

# delete bundle.Dockerfile since we do not use it to build image.
rm $DEPLOY_DIR/cluster-manager/bundle.Dockerfile
rm $DEPLOY_DIR/klusterlet/bundle.Dockerfile

if [[ "$CSV_VERSION" == "9.9.9" ]]; then
  exit 0
fi

# replace the image tags with the released version.
$SED_CMD -i "s/latest/v$CSV_VERSION/g" $DEPLOY_DIR/cluster-manager/olm-catalog/latest/manifests/cluster-manager.clusterserviceversion.yaml
$SED_CMD -i "s/latest/v$CSV_VERSION/g" $DEPLOY_DIR/klusterlet/olm-catalog/latest/manifests/klusterlet.clusterserviceversion.yaml
