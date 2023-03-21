# Changelog since v0.9.1
All notable changes to this project will be documented in this file.

## v0.10.0

### New Features
N/A

### Added
* Make work agent feature gate configurable. ([#303](https://github.com/open-cluster-management-io/registration-operator/pull/303) [@zhujian7](https://github.com/zhujian7))
* Add test cases for hubConfigSecretMissing. ([#307](https://github.com/open-cluster-management-io/registration-operator/pull/307) [@xuezhaojun](https://github.com/xuezhaojun))
* Add OAuthClient permissions to klusterlet-work-clusterrole-execution. ([#310](https://github.com/open-cluster-management-io/registration-operator/pull/310) [@TheRealJon](https://github.com/TheRealJon))
* Allow customizing the klusterlet name when deploying in hosted mode. ([#311](https://github.com/open-cluster-management-io/registration-operator/pull/311) [@mprahl](https://github.com/mprahl))

### Changes
* Use CRD manager to update and clean CRDs. ([#297](https://github.com/open-cluster-management-io/registration-operator/pull/297) [@qiujian16](https://github.com/qiujian16))
* Upgrade appliedManifestWork API. ([#298](https://github.com/open-cluster-management-io/registration-operator/pull/298) [@qiujian16](https://github.com/qiujian16))
* Upgrade clusterManagementAddon API. ([#300](https://github.com/open-cluster-management-io/registration-operator/pull/300) [@skeeey](https://github.com/skeeey))
* Upgrade ginkgo to v2. ([#301](https://github.com/open-cluster-management-io/registration-operator/pull/301) [@xuezhaojun](https://github.com/xuezhaojun))
* Refactor clustermanager controller. ([#305](https://github.com/open-cluster-management-io/registration-operator/pull/305) [@qiujian16](https://github.com/qiujian16))
* Refactor klusterlet. ([#306](https://github.com/open-cluster-management-io/registration-operator/pull/306) [@qiujian16](https://github.com/qiujian16))
* Upgrade github action. ([#308](https://github.com/open-cluster-management-io/registration-operator/pull/308) [@ycyaoxdu](https://github.com/ycyaoxdu))

### Bug Fixes
* Fix the issue that cleanup is not completed if appliedmainfestWork is not found. ([#312](https://github.com/open-cluster-management-io/registration-operator/pull/312) [@zhiweiyin318](https://github.com/zhiweiyin318))

### Removed & Deprecated
N/A
