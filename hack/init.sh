#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail

HUB_CRD_FILES="./vendor/github.com/open-cluster-management/api/cluster/v1/*.crd.yaml
./vendor/github.com/open-cluster-management/api/work/v1/0000_00_work.open-cluster-management.io_manifestworks.crd.yaml
"

SPOKE_CRD_FILES="./vendor/github.com/open-cluster-management/api/work/v1/0000_01_work.open-cluster-management.io_appliedmanifestworks.crd.yaml"

CLUSTER_MANAGER_CRD_FILE="./vendor/github.com/open-cluster-management/api/operator/v1/0000_01_operator.open-cluster-management.io_clustermanagers.crd.yaml"
KLUSTERLET_CRD_FILE="./vendor/github.com/open-cluster-management/api/operator/v1/0000_00_operator.open-cluster-management.io_klusterlets.crd.yaml"
