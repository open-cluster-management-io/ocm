#!/bin/bash

source "$(dirname "${BASH_SOURCE}")/init.sh"

BASE_DIR=$(dirname $(readlink -f $0))

for f in $HUB_CRD_FILES
do
    if [ -f "$BASE_DIR/$(basename $f).yaml-patch" ];
    then
       "$1" -o "$BASE_DIR/$(basename "$f").yaml-patch" < "$f" > "./manifests/cluster-manager/hub/crds/$(basename "$f").tmp"
       diff -N "./manifests/cluster-manager/hub/crds/$(basename "$f").tmp" "./manifests/cluster-manager/hub/crds/$(basename "$f")" || ( echo 'crd content is incorrect' && false )
       rm "./manifests/cluster-manager/hub/crds/$(basename "$f").tmp"
    else 
        diff -N "$f" "./manifests/cluster-manager/hub/crds/$(basename "$f")" || ( echo 'crd content is incorrect' && false )
    fi
done

for f in $SPOKE_CRD_FILES
do
    diff -N $f ./manifests/klusterlet/managed/$(basename $f) || ( echo 'crd content is incorrect' && false )
done

diff -N $CLUSTER_MANAGER_CRD_FILE ./deploy/cluster-manager/config/crds/$(basename $CLUSTER_MANAGER_CRD_FILE) || ( echo 'crd content is incorrect' && false )
diff -N $KLUSTERLET_CRD_FILE ./deploy/klusterlet/config/crds/$(basename $KLUSTERLET_CRD_FILE) || ( echo 'crd content is incorrect' && false )

