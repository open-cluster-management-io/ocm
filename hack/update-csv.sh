#!/bin/bash

set -e

REPO_ROOT=$(realpath "$(dirname "${BASH_SOURCE[0]}")"/..)
BINDIR=$REPO_ROOT/_output/tools/bin

DEPLOY_DIR=$REPO_ROOT/deploy
CLUSTER_MANAGER_DIR=$DEPLOY_DIR/cluster-manager
KLUSTERLET_DIR=$DEPLOY_DIR/klusterlet

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

