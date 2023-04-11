# Changelog since v0.3.0
All notable changes to this project will be documented in this file.

## v0.4.0

### New Features
* Add filter plugin taint toleration. Users are able to filter clusters via taints and tolerations. ([#63](https://github.com/open-cluster-management-io/placement/pull/63) [#64](https://github.com/open-cluster-management-io/placement/pull/64) [#66](https://github.com/open-cluster-management-io/placement/pull/66) [@haoqing0110](https://github.com/haoqing0110))

### Added
N/C

### Changes
* Update `Placement` and `PlacementDecision` api version to v1beta1. ([#60](https://github.com/open-cluster-management-io/placement/pull/60) [@haoqing0110](https://github.com/haoqing0110))
* Integrates placement with `ManagedClusterSet` [changes](https://github.com/open-cluster-management-io/api/pull/141) and supports `ManagedClusterSet` with `LegacyClusterSetLabel`. ([#65](https://github.com/open-cluster-management-io/placement/pull/65) [@haoqing0110](https://github.com/haoqing0110))
* Avoid building latest image in release branch. ([#67](https://github.com/open-cluster-management-io/placement/pull/67) [@qiujian16](https://github.com/qiujian16))

### Bug Fixes
N/C

### Removed & Deprecated
N/C