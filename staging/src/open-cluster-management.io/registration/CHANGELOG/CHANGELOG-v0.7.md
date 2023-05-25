# Changelog since v0.6.0
All notable changes to this project will be documented in this file.

## v0.7.0

### New Features 
* Support to to maintain a default cluster set for managed clusters. ([#204](https://github.com/open-cluster-management-io/registration/pull/204) [@elgnay](https://github.com/elgnay), [#198](https://github.com/open-cluster-management-io/registration/pull/198) [#202](https://github.com/open-cluster-management-io/registration/pull/202) [ycyaoxdu](https://github.com/ycyaoxdu), [#205](https://github.com/open-cluster-management-io/registration/pull/205) [ldpliu](https://github.com/ldpliu))

### Added
* Support v1beta1 CSR to be compatible with low versions of Kubernetes for hub cluster. ([#214](https://github.com/open-cluster-management-io/registration/pull/214) [@yue9944882](https://github.com/yue9944882))
* Support to run registration webhooks locally. ([#200](https://github.com/open-cluster-management-io/registration/pull/200) [xuezhaojun](https://github.com/xuezhaojun))
* Add managed cluster taints update e2e test. ([#212](https://github.com/open-cluster-management-io/registration/pull/212) [JiahaoWei-RH](https://github.com/JiahaoWei-RH))

### Changes
N/C

### Bug Fixes
* Avoid useless error log messages in registration webhooks. ([#215](https://github.com/open-cluster-management-io/registration/pull/215) [@qiujian16](https://github.com/qiujian16))

### Removed & Deprecated
* Stop to build latest image in release branch. ([#218](https://github.com/open-cluster-management-io/registration/pull/218) [@qiujian16](https://github.com/qiujian16))
