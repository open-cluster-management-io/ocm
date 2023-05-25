# Changelog since v0.10.0
All notable changes to this project will be documented in this file.

## v0.11.0

### New Features
* Support to approve a managed cluster registraion request automatically. ([#301](https://github.com/open-cluster-management-io/registration/pull/301) [@skeeey](https://github.com/skeeey))

### Added
* Support to specify expiration seconds for managed cluster registraion CSR. ([#312](https://github.com/open-cluster-management-io/registration/pull/312) [@youhangwang](https://github.com/youhangwang))
* Support to configure the leader eclection flags for registration controller. ([#306](https://github.com/open-cluster-management-io/registration/pull/306) [@ldpliu](https://github.com/ldpliu))
* Validate the name of managed cluster. ([#304](https://github.com/open-cluster-management-io/registration/pull/304) [@qiujian16](https://github.com/qiujian16))

### Changes
* Upgrade webhook server TLS min version to 1.3. ([#314](https://github.com/open-cluster-management-io/registration/pull/314) [@ldpliu](https://github.com/ldpliu))
* Upgrade kube libraries to 1.26 ([#302](https://github.com/open-cluster-management-io/registration/pull/302) [@skeeey](https://github.com/skeeey))
* Read the add-on installation mamespace from addo-on status at first. ([#308](https://github.com/open-cluster-management-io/registration/pull/308) [@qiujian16](https://github.com/qiujian16))
* Remove anonymous check from managedClusterCreatingController sync. ([#299](https://github.com/open-cluster-management-io/registration/pull/299) [@aii-nozomu-oki](https://github.com/aii-nozomu-oki))

### Bug Fixes
* Keep the addon hub-kubeocnfig during addon is deleting. ([#315](https://github.com/open-cluster-management-io/registration/pull/315) [@zhiweiyin318](https://github.com/zhiweiyin318))
* Deny the managed cluster registraion request when managed cluster namespace is terminating. ([#310](https://github.com/open-cluster-management-io/registration/pull/310) [@xuezhaojun](https://github.com/xuezhaojun))

### Removed & Deprecated
N/C
