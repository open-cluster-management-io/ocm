# Changelog since v0.10.0
All notable changes to this project will be documented in this file.

## v0.11.0

### New Features
* Support of ManifestWorkReplicaSet
  * Add create, update and delete placeManifestWork. ([#177](https://github.com/open-cluster-management-io/work/pull/177) [@serngawy ](https://github.com/serngawy))
  * Add validate webhook. ([#182](https://github.com/open-cluster-management-io/work/pull/182) [@serngawy ](https://github.com/serngawy))
  * Add initial e2e/integration for placeManifestWork. ([#180](https://github.com/open-cluster-management-io/work/pull/180) [@qiujian16](https://github.com/qiujian16))
  * Rename placeManifestWork to manifestWorkReplicaSet. ([#187](https://github.com/open-cluster-management-io/work/pull/187) [@serngawy ](https://github.com/serngawy))
  * Add manifestWorkReplicaSet feature check. ([#192](https://github.com/open-cluster-management-io/work/pull/192) [@serngawy ](https://github.com/serngawy))
  * Add e2e/integration test case for manifestworkreplicaset. ([#191](https://github.com/open-cluster-management-io/work/pull/191) [@qiujian16](https://github.com/qiujian16))
* AppliedManifestWork eviction. ([#190](https://github.com/open-cluster-management-io/work/pull/190) [@skeeey](https://github.com/skeeey))
* Allow return rawjson when featuregate is enabled. ([#202](https://github.com/open-cluster-management-io/work/pull/202) [@qiujian16](https://github.com/qiujian16))

### Added
* Make e2e test cases ran as canary test cases. ([#178](https://github.com/open-cluster-management-io/work/pull/178) [@elgnay](https://github.com/elgnay))
* The manifest size limit can be configured. ([#186](https://github.com/open-cluster-management-io/work/pull/186) [@zhiweiyin318](https://github.com/zhiweiyin318))
* Make the eventually timeout of e2e test configurable. ([#197](https://github.com/open-cluster-management-io/work/pull/197) [@elgnay](https://github.com/elgnay))
* Add gosec in verify. ([#199](https://github.com/open-cluster-management-io/work/pull/199) [@xuezhaojun](https://github.com/xuezhaojun))

### Changes
* Upgrade imagebuilder. ([#181](https://github.com/open-cluster-management-io/work/pull/181) [@elgnay](https://github.com/elgnay))
* Upgrade deps. ([#194](https://github.com/open-cluster-management-io/work/pull/194) [@xuezhaojun](https://github.com/xuezhaojun))
* Increase controller workers for available and cleanup. ([#200](https://github.com/open-cluster-management-io/work/pull/200) [@skeeey](https://github.com/skeeey))
* Update min tls to 1.13. ([#201](https://github.com/open-cluster-management-io/work/pull/201) [@ldpliu](https://github.com/ldpliu))

### Bug Fixes
* Fix spelling in err message. ([#195](https://github.com/open-cluster-management-io/work/pull/195) [@maleck13](https://github.com/maleck13))
* Fix available controller to reduce conflict. ([#198](https://github.com/open-cluster-management-io/work/pull/198) [@qiujian16](https://github.com/qiujian16))

### Removed & Deprecated
* Upgrade kube lib to 1.26 and remove old webhook. ([#189](https://github.com/open-cluster-management-io/work/pull/189) [@qiujian16](https://github.com/qiujian16))