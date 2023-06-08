# Changelog 
All notable changes to this project will be documented in this file.

## v0.4.0

### New Features
N/C

### Added
* Add goci lint. ([#95](https://github.com/open-cluster-management-io/addon-framework/pull/95) [@xuezhaojun](https://github.com/xuezhaojun))
* Add healthProber to the addonfactory interface. ([#106](https://github.com/open-cluster-management-io/addon-framework/pull/106) [@zhiweiyin318](https://github.com/zhiweiyin318))

### Changes
* Replace the go builder image in Dockerfile. ([#94](https://github.com/open-cluster-management-io/addon-framework/pull/94) [@xuezhaojun](https://github.com/xuezhaojun))
* Do not update addon when spec is changed. ([#100](https://github.com/open-cluster-management-io/addon-framework/pull/100) [@qiujian16](https://github.com/qiujian16))
* Make the chart dir support containing file path suffix. ([#99](https://github.com/open-cluster-management-io/addon-framework/pull/99) [@zhujian7](https://github.com/zhujian7))
* Update condition when call manifest failed. ([#101](https://github.com/open-cluster-management-io/addon-framework/pull/101) [@qiujian16](https://github.com/qiujian16))

### Bug Fixes
* Fix: no addon install when managed cluster is deleting. ([#93](https://github.com/open-cluster-management-io/addon-framework/pull/93) [@xuezhaojun](https://github.com/xuezhaojun))

### Removed & Deprecated
* Remove the openshift library-go dependency. ([#104](https://github.com/open-cluster-management-io/addon-framework/pull/104) [@qiujian16](https://github.com/qiujian16))
