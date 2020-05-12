#!/bin/bash

source "$(dirname "${BASH_SOURCE}")/init.sh"

for f in $CRD_FILES
do
    diff -N $f ./manifests/hub/$(basename $f) || ( echo 'crd content is incorrect' && false )
done
