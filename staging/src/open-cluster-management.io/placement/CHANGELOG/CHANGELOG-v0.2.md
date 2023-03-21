# Changelog since v0.1.0
All notable changes to this project will be documented in this file.

## v0.2.0

### New Features
* Allow user to weight prioritizers in a placement. ([#31](https://github.com/open-cluster-management-io/placement/pull/31) [@haoqing0110](https://github.com/haoqing0110))
* Support resource based placement scheduling. User is able to create a placement to select managed clusters according to cluster resource capacity and allocatable. ([#32](https://github.com/open-cluster-management-io/placement/pull/32) [@haoqing0110](https://github.com/haoqing0110))

### Added
N/C

### Changes
* Update the sort logic in Schedule. ([#29](https://github.com/open-cluster-management-io/placement/pull/29) [@suigh](https://github.com/suigh))
* Clean env after e2e test. ([#34](https://github.com/open-cluster-management-io/placement/pull/34) [@suigh](https://github.com/suigh))
* Upgrade the ClusterSet and ClusterSetBinding APIs to v1beta1. ([#35](https://github.com/open-cluster-management-io/placement/pull/35) [@elgnay](https://github.com/elgnay))

### Bug Fixes
* Avoid forbidden message in the log of controller. ([#27](https://github.com/open-cluster-management-io/placement/pull/27) [@vincent-pli](https://github.com/vincent-pli))
* Reschedule placment when clusterset added. ([#39](https://github.com/open-cluster-management-io/placement/pull/39) [@haoqing0110](https://github.com/haoqing0110))

### Removed & Deprecated
N/C
