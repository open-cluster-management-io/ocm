# Changelog since v0.5.0
All notable changes to this project will be documented in this file.

## v0.6.0

### New Features
* Support `Hosted` mode to deploy `Klusterlet` outside the managed cluster. ([#172](https://github.com/open-cluster-management-io/registration-operator/pull/172) [#180](https://github.com/open-cluster-management-io/registration-operator/pull/180) [@zhujian7](https://github.com/zhujian7), [#179](https://github.com/open-cluster-management-io/registration-operator/pull/179) [#186](https://github.com/open-cluster-management-io/registration-operator/pull/186) [#188](https://github.com/open-cluster-management-io/registration-operator/pull/188) [@xuezhaojun](https://github.com/xuezhaojun)
 )

### Added
* Add a new API `AddonPlacementScores`. ([#187](https://github.com/open-cluster-management-io/registration-operator/pull/187) [@haoqing0110](https://github.com/haoqing0110))

### Changes
* Disable the leader election of agent pods when the replica is 1. ([#193](https://github.com/open-cluster-management-io/registration-operator/pull/193) [@qiujian16](https://github.com/qiujian16))
* Update `ManagerCluster` and `Placement` APIs to support taint. ([#183](https://github.com/open-cluster-management-io/registration-operator/pull/183) [@haoqing0110](https://github.com/haoqing0110))
* The `relatedResources` field in the status of `ClusterManager` and `Klusterlet` includes all related resources.  ([#173](https://github.com/open-cluster-management-io/registration-operator/pull/173) [@zhiweiyin318](https://github.com/zhiweiyin318))
* Upgrade go to 1.17. ([#192](https://github.com/open-cluster-management-io/registration-operator/pull/192) [@zhiweiyin318](https://github.com/zhiweiyin318))

### Bug Fixes
* Fix the issue that apiService re-apply infinitely. ([#178](https://github.com/open-cluster-management-io/registration-operator/pull/178) [@xuezhaojun](https://github.com/xuezhaojun))
* Fix the issue that work agent works after 2min.   ([#184](https://github.com/open-cluster-management-io/registration-operator/pull/184) [@zhiweiyin318](https://github.com/zhiweiyin318))

### Removed & Deprecated
N/C