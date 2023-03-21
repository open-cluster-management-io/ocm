#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail

HUB_CRD_FILES="./vendor/open-cluster-management.io/api/cluster/v1/*.crd.yaml
./vendor/open-cluster-management.io/api/addon/v1alpha1/*.crd.yaml
./vendor/open-cluster-management.io/api/cluster/v1beta2/0000_01_clusters.open-cluster-management.io_managedclustersetbindings.crd.yaml
./vendor/open-cluster-management.io/api/cluster/v1beta2/0000_00_clusters.open-cluster-management.io_managedclustersets.crd.yaml
./vendor/open-cluster-management.io/api/cluster/v1beta1/0000_02_clusters.open-cluster-management.io_placements.crd.yaml
./vendor/open-cluster-management.io/api/cluster/v1beta1/0000_03_clusters.open-cluster-management.io_placementdecisions.crd.yaml
./vendor/open-cluster-management.io/api/cluster/v1alpha1/0000_05_clusters.open-cluster-management.io_addonplacementscores.crd.yaml
./vendor/open-cluster-management.io/api/work/v1/0000_00_work.open-cluster-management.io_manifestworks.crd.yaml
"

PATCHED_DIR="./_output/"

SPOKE_CRD_FILES="./vendor/open-cluster-management.io/api/work/v1/0000_01_work.open-cluster-management.io_appliedmanifestworks.crd.yaml
./vendor/open-cluster-management.io/api/cluster/v1alpha1/0000_02_clusters.open-cluster-management.io_clusterclaims.crd.yaml
./vendor/open-cluster-management.io/api/work/v1/0001_01_work.open-cluster-management.io_appliedmanifestworks.crd.yaml
./vendor/open-cluster-management.io/api/cluster/v1alpha1/0001_02_clusters.open-cluster-management.io_clusterclaims.crd.yaml
"

CLUSTER_MANAGER_CRD_FILE="./vendor/open-cluster-management.io/api/operator/v1/0000_01_operator.open-cluster-management.io_clustermanagers.crd.yaml"
KLUSTERLET_CRD_FILE="./vendor/open-cluster-management.io/api/operator/v1/0000_00_operator.open-cluster-management.io_klusterlets.crd.yaml"
