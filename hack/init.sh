#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail

CRD_FILES="./vendor/github.com/open-cluster-management/api/cluster/v1/*.crd.yaml
./vendor/github.com/open-cluster-management/api/work/v1/*.crd.yaml
"
