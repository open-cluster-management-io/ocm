# Overview

This repo describes the API implemented by addon controllers in the open-cluster-management.io project to report status of addons installed on the managed cluster.

# API

## ClusterManagementAddon and ManagedClusterAddon

API Group: `addon.open-cluster-management.io/v1alpha1`

Kinds:

- `ClusterManagementAddOn`: represents the registration of an add-on to the cluster manager
- `ManagedClusterAddOn`: represents the current state of an add-on.

