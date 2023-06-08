#!/bin/bash

source "$(dirname "${BASH_SOURCE}")/init.sh"

for f in $HUB_CRD_FILES
do
    if [ -f "$PATCHED_DIR/$(basename $f)" ]
    then
        cp $PATCHED_DIR/$(basename $f) ./manifests/cluster-manager/hub/
    else 
        cp $f ./manifests/cluster-manager/hub/
    fi
done

for f in $SPOKE_CRD_FILES
do
    cp $f ./manifests/klusterlet/managed/
done

cp $CLUSTER_MANAGER_CRD_FILE ./deploy/cluster-manager/config/crds/
cp $KLUSTERLET_CRD_FILE ./deploy/klusterlet/config/crds/
