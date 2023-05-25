#!/bin/bash

BASE_DIR=$(dirname $(readlink -f $0))

source "$BASE_DIR/../init.sh"

for f in $HUB_CRD_FILES
do
   if [ -f "$BASE_DIR/$(basename $f).yaml-patch" ]; then
     $1 -o $BASE_DIR/$(basename $f).yaml-patch < $f > $PATCHED_DIR/$(basename $f)   
   fi
done
