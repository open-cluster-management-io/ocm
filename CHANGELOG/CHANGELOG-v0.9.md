# Changelog since v0.8.0
All notable changes to this project will be documented in this file.

## v0.9.0

### New Features
N/C

### Added
* Add new placement condition type `PlacementConditionMisconfigured` for configuration error and schedule failure. ([#72](https://github.com/open-cluster-management-io/placement/pull/72) [@haoqing0110](https://github.com/haoqing0110))
* Introduce `cluster.open-cluster-management.io/experimental-scheduling-disable` annotation to allow disabling the scheduling. ([#80](https://github.com/open-cluster-management-io/placement/pull/80) [@clyang82](https://github.com/clyang82))

### Changes
* Upgrade golang modules. ([#78](https://github.com/open-cluster-management-io/placement/pull/78) [@haoqing0110](https://github.com/haoqing0110))

### Bug Fixes
* Fix the issue that placement doesn't trigger scheduling when cluster belongs to LabelSelector type clusterset. ([#81](https://github.com/open-cluster-management-io/placement/pull/81) [@haoqing0110](https://github.com/haoqing0110))

### Removed & Deprecated
* The `Placement` and `PlacementDecision` API v1alpha1 version will no longer be served in OCM v0.9.0. ([#290](https://github.com/open-cluster-management-io/open-cluster-management-io.github.io/pull/290) [@haoqing0110](https://github.com/haoqing0110))