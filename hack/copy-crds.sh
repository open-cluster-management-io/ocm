#!/bin/bash

source "$(dirname "${BASH_SOURCE}")/init.sh"

BASE_DIR=$(dirname $(readlink -f $0))

for f in $HUB_CRD_FILES
do
    if [ -f "$BASE_DIR/$(basename $f).yaml-patch" ];
    then
        "$1" -o "$BASE_DIR/$(basename "$f").yaml-patch" < "$f" > "./manifests/cluster-manager/hub/crds/$(basename "$f")"
    else 
        cp $f ./manifests/cluster-manager/hub/crds
    fi
done

for f in $SPOKE_CRD_FILES
do
    cp $f ./manifests/klusterlet/managed/
done

cp $CLUSTER_MANAGER_CRD_FILE ./deploy/cluster-manager/config/crds/
cp $CLUSTER_MANAGER_CRD_FILE ./deploy/cluster-manager/chart/cluster-manager/crds/
cp $KLUSTERLET_CRD_FILE ./deploy/klusterlet/config/crds/
cp $KLUSTERLET_CRD_FILE ./deploy/klusterlet/chart/klusterlet/crds/
