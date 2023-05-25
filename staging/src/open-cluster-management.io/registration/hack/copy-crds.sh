#!/bin/bash

source "$(dirname "${BASH_SOURCE}")/init.sh"

for f in $HUB_CRD_FILES
do
    cp $f ./deploy/hub/
done

for f in $SPOKE_CRD_FILES
do
    cp $f ./deploy/spoke/
done
