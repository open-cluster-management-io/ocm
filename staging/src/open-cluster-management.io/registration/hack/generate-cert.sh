#!/bin/bash
BASE_DIR=$(dirname $(readlink -f $0))

DOMAIN=managedcluster-admission.open-cluster-management-hub.svc
CERTDIR=$BASE_DIR/cert
DATE=3650

rm -rf $CERTDIR

mkdir $CERTDIR
echo subjectAltName = DNS:${DOMAIN} > ${CERTDIR}/server.conf

openssl genrsa -out $CERTDIR/tls.key 4096
openssl req -new -subj "/CN=${DOMAIN}" -sha256 -out $CERTDIR/tls.csr -key $CERTDIR/tls.key 
openssl x509 -req -days ${DATE} -in $CERTDIR/tls.csr -signkey $CERTDIR/tls.key -out $CERTDIR/tls.crt -extfile ${CERTDIR}/server.conf
