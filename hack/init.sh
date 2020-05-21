#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail

CRD_FILES="./vendor/github.com/open-cluster-management/api/cluster/v1/*.crd.yaml
./vendor/github.com/open-cluster-management/api/work/v1/*.crd.yaml
"

NUCLEUS_HUB_CRD_FILE="./vendor/github.com/open-cluster-management/api/nucleus/v1/0000_01_nucleus.open-cluster-management.io_hubcores.crd.yaml"
NUCLEUS_SPOKE_CRD_FILE="./vendor/github.com/open-cluster-management/api/nucleus/v1/0000_00_nucleus.open-cluster-management.io_agentcores.crd.yaml"
