# Changelog since v0.8.0
All notable changes to this project will be documented in this file.

## v0.9.0

### New Features 
N/C

### Added
* Support to convert clusterset api between v1beta1 and v1beta1 by conversion webhook. ([#272](https://github.com/open-cluster-management-io/registration/pull/272) [@ldpliu](https://github.com/ldpliu))
* Support to update ManagedCluster serverURL and CABundle if ManagedCluster is created before registration-agent running. ([#270](https://github.com/open-cluster-management-io/registration/pull/270) [@ivan-cai](https://github.com/ivan-cai))

### Changes
* Decouple bootstrap informers to distinguish each lifecycle. ([#271](https://github.com/open-cluster-management-io/registration/pull/271) [@yue9944882](https://github.com/yue9944882))
* Update dependent libraries. ([#266](https://github.com/open-cluster-management-io/registration/pull/266) [@skeeey](https://github.com/skeeey))


### Bug Fixes
* Fix the e2e errors when running e2e before mutating webhook deployment is ready. ([#267](https://github.com/open-cluster-management-io/registration/pull/267) [@haoqing0110](https://github.com/haoqing0110))
* Recreating the add-on agent hub kubeconfig when add-on deploy options is changed. ([#265](https://github.com/open-cluster-management-io/registration/pull/265) [@zhujian7](https://github.com/zhujian7))


### Removed & Deprecated
* Remove the support for checking add-on hub lease. ([#275](https://github.com/open-cluster-management-io/registration/pull/275) [@skeeey](https://github.com/skeeey))
