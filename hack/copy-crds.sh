#!/bin/bash

source "$(dirname "${BASH_SOURCE}")/init.sh"

for f in $CRD_FILES
do
    cp $f ./manifests/hub/
done

cp $NUCLEUS_HUB_CRD_FILE ./deploy/nucleus-hub/crds/
cp $NUCLEUS_SPOKE_CRD_FILE ./deploy/nucleus-spoke/crds/
