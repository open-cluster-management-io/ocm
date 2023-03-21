# Changelog since v0.6.0
All notable changes to this project will be documented in this file.

## v0.7.0

### New Features
N/C

### Added
* Enable DefaultClusterSet feature-gate for registration and registration-webhook. ([#209](https://github.com/open-cluster-management-io/registration-operator/pull/209) [@ycyaoxdu](https://github.com/ycyaoxdu), [#210](https://github.com/open-cluster-management-io/registration-operator/pull/210) [@ldpliu](https://github.com/ldpliu))
* Support AddonPlacementScores in placement controller.  ([#203](https://github.com/open-cluster-management-io/registration-operator/pull/203) [@haoqing0110](https://github.com/haoqing0110))
* Add disable-leader-election flag for Klusterlet. ([#221](https://github.com/open-cluster-management-io/registration-operator/pull/221))

### Changes
* Upgrade the Placement and PlacementDecision APIs to v1Beta1. ([#198](https://github.com/open-cluster-management-io/registration-operator/pull/198) [@haoqing0110](https://github.com/haoqing0110))
* Upgrade the API and library version.([#217](https://github.com/open-cluster-management-io/registration-operator/pull/217) [@qiujian16](https://github.com/qiujian16), [#208](https://github.com/open-cluster-management-io/registration-operator/pull/208) [@ldpliu](https://github.com/ldpliu))
* Change Detached mode to Hosted mode in ClusterManager and Klusterlet. ([#219](https://github.com/open-cluster-management-io/registration-operator/pull/219), [#220](https://github.com/open-cluster-management-io/registration-operator/pull/220) [@xuezhaojun](https://github.com/xuezhaojun))
* Make the installMode as an option for the klusterlet. ([#207](https://github.com/open-cluster-management-io/registration-operator/pull/207) [@zhujian7](https://github.com/zhujian7))
* Set the replica of work-agent to 0 when hub-kubeconfig-secret is missing. ([#213](https://github.com/open-cluster-management-io/registration-operator/pull/213) [@zhiweiyin318](https://github.com/zhiweiyin318))
* Reduce the resource request for the pods.([#218](https://github.com/open-cluster-management-io/registration-operator/pull/218) [@zhujian7](https://github.com/zhujian7))
* Change to use a community builder image. ([#199](https://github.com/open-cluster-management-io/registration-operator/pull/199) [@zhiweiyin318](https://github.com/zhiweiyin318))
* Code refactor. ([#202](https://github.com/open-cluster-management-io/registration-operator/pull/202), [#204](https://github.com/open-cluster-management-io/registration-operator/pull/204), [#222](https://github.com/open-cluster-management-io/registration-operator/pull/222) [@qiujian16](https://github.com/qiujian16), [#216](https://github.com/open-cluster-management-io/registration-operator/pull/216) [@zhujian7](https://github.com/zhujian7), [#206](https://github.com/open-cluster-management-io/registration-operator/pull/206) [@xuezhaojun](https://github.com/xuezhaojun))


### Bug Fixes
* Fix the issue that cannot get SA token secret when the secret name is long. ([#197](https://github.com/open-cluster-management-io/registration-operator/pull/197) [@xuezhaojun](https://github.com/xuezhaojun))
* Fix the issue that has wrong replica in condition message. ([#201](https://github.com/open-cluster-management-io/registration-operator/pull/201) [@qiujian16](https://github.com/qiujian16))

### Removed & Deprecated
N/C