# Changelog since v0.5.0
All notable changes to this project will be documented in this file.

## v0.6.0

### New Features 
* Support running work agent outside of managed cluster. ([#104](https://github.com/open-cluster-management-io/work/pull/104) [@zhujian7](https://github.com/zhujian7))
* Support to report back the status of applied resources in ManifestWork to hub cluster. ([#107](https://github.com/open-cluster-management-io/work/pull/107) [@qiujian16](https://github.com/qiujian16))

### Added
* Add flag to work agent to disable leader election. ([#109](https://github.com/open-cluster-management-io/work/pull/109) [@qiujian16](https://github.com/qiujian16))

### Changes
* Set the default propagation policy to background when deleting applied resources. ([#101](https://github.com/open-cluster-management-io/work/pull/101) [@qiujian16](https://github.com/qiujian16))
* Update SECURITY.md with reference to community doc. ([#105](https://github.com/open-cluster-management-io/work/pull/105) [@mikeshng](https://github.com/mikeshng))
* Upgrade go to 1.17. ([#108](https://github.com/open-cluster-management-io/work/pull/108) [@qiujian16](https://github.com/qiujian16))

### Bug Fixes
* Fix flaky test failure. ([#106](https://github.com/open-cluster-management-io/work/pull/106) [@zhujian7](https://github.com/zhujian7))

### Removed & Deprecated
N/C
