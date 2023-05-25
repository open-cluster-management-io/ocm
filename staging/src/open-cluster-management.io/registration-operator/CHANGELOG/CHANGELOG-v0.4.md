# Changelog since v0.3.0
All notable changes to this project will be documented in this file.

## v0.4.0

### New Features
* Enable clusterManagementAddon and managedClusterAddon.
* Support to deploy placement controller.
* The replica of pods can be changed based on the number of master nodes.

### Added
* Add short names for mangedCluster and managedClusterSet.
* Support to check hub bootstrap secret expired.

### Changes
* Upgrade CRD to support placement API.
* Upgrade CRD to v1 and k8s lib to v0.21.0-rc.0.
* Upgrade Go to 1.16.
* Use kustomize to deploy by Makefile.

### Bug Fixes
* Fix some deploy issues about Makefile.

### Removed & Deprecated
N/C
