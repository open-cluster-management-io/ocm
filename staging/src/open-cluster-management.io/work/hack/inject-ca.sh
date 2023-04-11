#!/bin/bash

BASE_DIR=$(dirname $(readlink -f $0))

sh -x $BASE_DIR/generate-cert.sh

CA=`cat $BASE_DIR/cert/tls.crt |base64 -w 0`

sed -i "s/CA_PLACE_HOLDER/${CA}/g" $BASE_DIR/../deploy/hub/webhook.yaml

rm -rf $BASE_DIR/../deploy/hub/cert

mv -f $BASE_DIR/cert $BASE_DIR/../deploy/hub/cert
