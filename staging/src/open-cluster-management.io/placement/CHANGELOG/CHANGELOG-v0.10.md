# Changelog since v0.9.0
All notable changes to this project will be documented in this file.

## v0.10.0

### New Features
N/C

### Added
N/C

### Changes
* Watch the `AddOnPlacementScore` during scheduling. This enable `PlacementDecision` to reflect `AddOnPlacementScore` more frequently than every 5 minutes [#87](https://github.com/open-cluster-management-io/placement/issues/87). ([#89](https://github.com/open-cluster-management-io/placement/pull/89) [@qiujian16](https://github.com/qiujian16))
* Create empty `PlacementDecision` when no cluster selected. ([#95](https://github.com/open-cluster-management-io/placement/pull/95) [@haoqing0110](https://github.com/haoqing0110))
* Enhance the integration testing. ([#90](https://github.com/open-cluster-management-io/placement/pull/90) [@qiujian16](https://github.com/qiujian16)) ([#91](https://github.com/open-cluster-management-io/placement/pull/91) [@zhujian7](https://github.com/zhujian7)) ([#93](https://github.com/open-cluster-management-io/placement/pull/93) [@haoqing0110](https://github.com/haoqing0110))
* Upgrade `ManagedClusterSet` and `ManagedClusterSetBinding` API to v1beta2. ([#82](https://github.com/open-cluster-management-io/placement/pull/82) [@ldpliu](https://github.com/ldpliu))
* Upgrade golang to 1.19. ([#84](https://github.com/open-cluster-management-io/placement/pull/84) [@haoqing0110](https://github.com/haoqing0110))
* Upgrade ginkgo to v2. ([#88](https://github.com/open-cluster-management-io/placement/pull/88) [@zhujian7](https://github.com/zhujian7))
* Upgrade github action. ([#94](https://github.com/open-cluster-management-io/placement/pull/94) [@ycyaoxdu](https://github.com/ycyaoxdu))

### Bug Fixes
N/C

### Removed & Deprecated
N/C