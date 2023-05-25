# Changelog since v0.10.0
All notable changes to this project will be documented in this file.

## v0.11.0

### New Features
* Support the installation of addon-manager. ([#324](https://github.com/open-cluster-management-io/registration-operator/pull/324) [@qiujian16](https://github.com/qiujian16), [#325](https://github.com/open-cluster-management-io/registration-operator/pull/325) [#336](https://github.com/open-cluster-management-io/registration-operator/pull/336) [#341](https://github.com/open-cluster-management-io/registration-operator/pull/341) [#348](https://github.com/open-cluster-management-io/registration-operator/pull/348) [@haoqing0110](https://github.com/haoqing0110))
* Support the installation of work-controller. ([#331](https://github.com/open-cluster-management-io/registration-operator/pull/331) [#340](https://github.com/open-cluster-management-io/registration-operator/pull/340) [#345](https://github.com/open-cluster-management-io/registration-operator/pull/345) [@serngawy](https://github.com/serngawy))
* Support setting autoApprovedUser and certDurationSeconds. ([#351](https://github.com/open-cluster-management-io/registration-operator/pull/351) [#353](https://github.com/open-cluster-management-io/registration-operator/pull/353) [@qiujian16](https://github.com/qiujian16))

### Added
* Add e2e for deleting klusterlet when the managed cluster was destroyed. ([#339](https://github.com/open-cluster-management-io/registration-operator/pull/339) [@zhujian7](https://github.com/zhujian7))
* Enable addon management and workreplicaset featuregates in e2e. ([#346](https://github.com/open-cluster-management-io/registration-operator/pull/346) [@zhiweiyin318](https://github.com/zhiweiyin318))

### Changes
* Upgrade kube lib to 0.26. ([#329](https://github.com/open-cluster-management-io/registration-operator/pull/329) [@zhiweiyin318](https://github.com/zhiweiyin318), [#333](https://github.com/open-cluster-management-io/registration-operator/pull/333) [@xuezhaojun](https://github.com/xuezhaojun))
* Refactor migration and storedversion update. ([#332](https://github.com/open-cluster-management-io/registration-operator/pull/332) [@ldpliu](https://github.com/ldpliu))
* Update RBAC. ([#341](https://github.com/open-cluster-management-io/registration-operator/pull/341) [@xuezhaojun](https://github.com/xuezhaojun))
* Upgrade API for jsonRaw field in work. ([#352](https://github.com/open-cluster-management-io/registration-operator/pull/352) [@qiujian16](https://github.com/qiujian16))

### Bug Fixes
* Do not filter applied manifest work by hub host when deleting klusterlet. ([#321](https://github.com/open-cluster-management-io/registration-operator/pull/321) [@zhujian7](https://github.com/zhujian7))
* Fix migration issue. ([#328](https://github.com/open-cluster-management-io/registration-operator/pull/328) [@ldpliu](https://github.com/ldpliu))
* Fix Implicit memory aliasing in for loop. ([#335](https://github.com/open-cluster-management-io/registration-operator/pull/335) [@ldpliu](https://github.com/ldpliu))
* Check managed cluster connectivity when deleting klusterlet. ([#337](https://github.com/open-cluster-management-io/registration-operator/pull/337) [@zhujian7](https://github.com/zhujian7))
* Fix vulnerability issue. ([#344](https://github.com/open-cluster-management-io/registration-operator/pull/344) [#350](https://github.com/open-cluster-management-io/registration-operator/pull/350) [@zhiweiyin318](https://github.com/zhiweiyin318))
* Reduce error logs when cluster is deleting in hosted mode. ([#354](https://github.com/open-cluster-management-io/registration-operator/pull/354) [@zhiweiyin318](https://github.com/zhiweiyin318)）

### Removed & Deprecated
* Remove old webhook. ([#330](https://github.com/open-cluster-management-io/registration-operator/pull/330) [@ldpliu](https://github.com/ldpliu))
* Remove addon enable field in clustermanager API. ([#338](https://github.com/open-cluster-management-io/registration-operator/pull/338) [@qiujian16](https://github.com/qiujian16))
