#!/bin/bash

source "$(dirname "${BASH_SOURCE}")/init.sh"

for f in $HUB_CRD_FILES
do
    diff -N $f ./deploy/hub/$(basename $f) || ( echo 'crd content is incorrect' && false )
done
