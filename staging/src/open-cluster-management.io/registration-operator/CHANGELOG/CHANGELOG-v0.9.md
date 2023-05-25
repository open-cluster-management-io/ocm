# Changelog since v0.8.0
All notable changes to this project will be documented in this file.

## v0.9.0

### New Features
N/A

### Added
* Add skip-remove-crds option for cluster-manager. ([#274](https://github.com/open-cluster-management-io/registration-operator/pull/274) [@ivanscai](https://github.com/ivan-cai))
* Add conversion webhook. ([#279](https://github.com/open-cluster-management-io/registration-operator/pull/279) [@ldpliu](https://github.com/ldpliu))

### Changes
* Allow OCM addons to set up metrics collection with Prometheus. ([#262](https://github.com/open-cluster-management-io/registration-operator/pull/262)[@mprahl](https://github.com/mprahl))
* Upgrade k8s lib to v0.24.3. ([#265](https://github.com/open-cluster-management-io/registration-operator/pull/265) [@zhiweiyin318](https://github.com/zhiweiyin318))
* Refactor to split two controllers to handle klusterlet deployment and cleanup. ([#269](https://github.com/open-cluster-management-io/registration-operator/pull/269) [@zhujian7](https://github.com/zhujian7))
* Apply Klusterlet only when having finalizer. ([#270](https://github.com/open-cluster-management-io/registration-operator/pull/270) [@qiujian16](https://github.com/qiujian16))
* Update AddOn configuration API. ([#272](https://github.com/open-cluster-management-io/registration-operator/pull/272) [@skeeey](https://github.com/skeeey))
* Allow work agent to impersonate serviceaccount. ([#275](https://github.com/open-cluster-management-io/registration-operator/pull/275) [@zhujian7](https://github.com/zhujian7))
* Make work webhook feature gate configurable. ([#276](https://github.com/open-cluster-management-io/registration-operator/pull/276) [@zhujian7](https://github.com/zhujian7))

### Bug Fixes
* Fix release yaml issue. ([#263](https://github.com/open-cluster-management-io/registration-operator/pull/263) [@qiujian16](https://github.com/qiujian16))
* Fix the managedCluster name in the apply-spoke-cr-hosted target. ([#266](https://github.com/open-cluster-management-io/registration-operator/pull/266)[@mprahl](https://github.com/mprahl))
* Fix to allow work agent to create subjectaccessreviews. ([#273](https://github.com/open-cluster-management-io/registration-operator/pull/273) [@zhujian7](https://github.com/zhujian7))
* Fix to delete addon crd at first. ([#277](https://github.com/open-cluster-management-io/registration-operator/pull/277) [@qiujian16](https://github.com/qiujian16))
* Fix token path in hosted mode. ([#284](https://github.com/open-cluster-management-io/registration-operator/pull/284) [@qiujian16](https://github.com/qiujian16))

### Removed & Deprecated
* Remove API Placement PlacementDecision ClusterSet ClusterSetBinding API version v1alpha. ([#278](https://github.com/open-cluster-management-io/registration-operator/pull/278) [@haoqing0110](https://github.com/haoqing0110))
* Remove install mode Detached. ([#282](https://github.com/open-cluster-management-io/registration-operator/pull/282) [@zhujian7](https://github.com/zhujian7))
* Remove clusterrole/role cleanBeforeApply code added in ocm 0.8.0. ([#283](https://github.com/open-cluster-management-io/registration-operator/pull/283) [@haoqing0110](https://github.com/haoqing0110))

## v0.9.1

### Bug Fixes

* Fix the incorrect managed cluster lease name. ([#288](https://github.com/open-cluster-management-io/registration-operator/pull/288) [@skeeey](https://github.com/skeeey))
* Fix the paradox description of the klusterlet condition([#294](https://github.com/open-cluster-management-io/registration-operator/pull/294) [@zhujian7](https://github.com/zhujian7))
