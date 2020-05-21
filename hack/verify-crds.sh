#!/bin/bash

source "$(dirname "${BASH_SOURCE}")/init.sh"

for f in $CRD_FILES
do
    diff -N $f ./manifests/hub/$(basename $f) || ( echo 'crd content is incorrect' && false )
done

diff -N $NUCLEUS_HUB_CRD_FILE ./deploy/nucleus-hub/crds/$(basename $NUCLEUS_HUB_CRD_FILES) || ( echo 'crd content is incorrect' && false )
diff -N $NUCLEUS_SPOKE_CRD_FILE ./deploy/nucleus-spoke/crds/$(basename $NUCLEUS_SPOKE_CRD_FILES) || ( echo 'crd content is incorrect' && false )

