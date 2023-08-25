#!/bin/bash
#
# Example patch file 0000_00_clusters.open-cluster-management.io_managedclustersets.crd.yaml.yaml-patch
# - op: add
#   path: /spec/conversion
#   value:
#     strategy: Webhook
#     webhook:
#       clientConfig:
#         service:
#           namespace: {{ .ClusterManagerNamespace }}
#           name: cluster-manager-registration-webhook
#           path: /convert
#           port: {{.RegistrationWebhook.Port}}
#         caBundle: {{ .RegistrationAPIServiceCABundle }}
#       conversionReviewVersions:
#       - v1beta1
#       - v1beta2

BASE_DIR=$(dirname $(readlink -f $0))

source "$BASE_DIR/../init.sh"

for f in $HUB_CRD_FILES
do
   if [ -f "$BASE_DIR/$(basename $f).yaml-patch" ]; then
     $1 -o $BASE_DIR/$(basename $f).yaml-patch < $f > $PATCHED_DIR/$(basename $f)   
   fi
done
