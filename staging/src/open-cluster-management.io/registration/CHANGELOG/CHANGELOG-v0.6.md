# Changelog since v0.5.0
All notable changes to this project will be documented in this file.

## v0.6.0

### New Features 
* Support to run spoke agent outside of managed cluster.([#175](https://github.com/open-cluster-management-io/registration/pull/175) [@zhujian7](https://github.com/zhujian7))

### Added
* Recreate the managed cluster addon client cert once managed cluster bootstrap kubeconfig changes. ([#172](https://github.com/open-cluster-management-io/registration/pull/172) [@elgnay](https://github.com/elgnay))
* Exclude the cordoned nodes when calculating managed cluster allocatable nodes. ([#174](https://github.com/open-cluster-management-io/registration/pull/174) [@skeeey](https://github.com/skeeey))
* Support to deploy the control plane and agent in two clusters by Makefile ([#182](https://github.com/open-cluster-management-io/registration/pull/182) [@zhujian7](https://github.com/zhujian7))
* Set the value of ManagedCluster taint timeAdded by managed cluster mutating webhook. ([#186](https://github.com/open-cluster-management-io/registration/pull/186) [@elgnay](https://github.com/elgnay))
* Support customized health check mode for managed cluster addon. ([#187](https://github.com/open-cluster-management-io/registration/pull/187) [@yue9944882](https://github.com/yue9944882))
* Add leader election flag. ([#193](https://github.com/open-cluster-management-io/registration/pull/193) [@qiujian16](https://github.com/qiujian16))

### Changes
* Upgrade golang from `1.16` to `1.17`. ([#189](https://github.com/open-cluster-management-io/registration/pull/189) [@qiujian16](https://github.com/qiujian16))
* Change builder image to `quay.io/bitnami/golang`. ([#191](https://github.com/open-cluster-management-io/registration/pull/191) [@qiujian16](https://github.com/qiujian16))
* Update resouce cache implementation. ([#192](https://github.com/open-cluster-management-io/registration/pull/192) [@qiujian16](https://github.com/qiujian16))

### Bug Fixes
* Fix the managed cluster lease will be recreated when the managed cluster is deleting. ([#181](https://github.com/open-cluster-management-io/registration/pull/181) [@skeeey](https://github.com/skeeey))
* Fix the registration pod will non-stop print event info when ManagedCluster.Spec.LeaseDurationSeconds is zero. ([#184](https://github.com/open-cluster-management-io/registration/pull/184) [@champly](https://github.com/champly))

### Removed & Deprecated
N/C
