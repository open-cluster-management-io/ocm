#!/bin/bash

source "$(dirname "${BASH_SOURCE}")/init.sh"

for f in $HUB_CRD_FILES
do
    if [ -f "$PATCHED_DIR/$(basename $f)" ]
    then
        diff -N $PATCHED_DIR/$(basename $f) ./manifests/cluster-manager/hub/$(basename $f) || ( echo 'crd content is incorrect' && false )
    else 
        diff -N $f ./manifests/cluster-manager/hub/$(basename $f) || ( echo 'crd content is incorrect' && false )
    fi
done

for f in $SPOKE_CRD_FILES
do
    diff -N $f ./manifests/klusterlet/managed/$(basename $f) || ( echo 'crd content is incorrect' && false )
done

diff -N $CLUSTER_MANAGER_CRD_FILE ./deploy/cluster-manager/config/crds/$(basename $CLUSTER_MANAGER_CRD_FILE) || ( echo 'crd content is incorrect' && false )
diff -N $KLUSTERLET_CRD_FILE ./deploy/klusterlet/config/crds/$(basename $KLUSTERLET_CRD_FILE) || ( echo 'crd content is incorrect' && false )

