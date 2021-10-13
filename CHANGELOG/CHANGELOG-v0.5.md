# Changelog since v0.4.0
All notable changes to this project will be documented in this file.

## v0.5.0

### New Features
* We can customize the `NodeSelector` and `Tolerations` to the pods deployed by the ClusterManager and Klusterlet Operators. ([#145](https://github.com/open-cluster-management-io/registration-operator/pull/145) [@zhiweiyin318](https://github.com/zhiweiyin318))

### Added
* Add a new status `Available` for the Klusterlet.([#151](https://github.com/open-cluster-management-io/registration-operator/pull/151) [@kim-fitness](https://github.com/kim-fitness))
* Create `open-cluster-management-xxx-addon` namespace and sync the image pull secret to the `open-cluster-management-xxx-addon` namespace on the managed clusters.  ([#147](https://github.com/open-cluster-management-io/registration-operator/pull/147) [@qiujian16](https://github.com/qiujian16))

### Changes
* Refine the permissions of Placement. ([#139](https://github.com/open-cluster-management-io/registration-operator/pull/139) [@elgnay](https://github.com/elgnay))
* Update the work and Placement APIs. ([#140](https://github.com/open-cluster-management-io/registration-operator/pull/140) [@qiujian16](https://github.com/qiujian16), [#153](https://github.com/open-cluster-management-io/registration-operator/pull/153) [@haoqing0110](https://github.com/haoqing0110))
* Upgrade the ClusterSet and ClusterSetBinding APIs to v1beta1. ([#148](https://github.com/open-cluster-management-io/registration-operator/pull/148), [#149](https://github.com/open-cluster-management-io/registration-operator/pull/149) [@elgnay](https://github.com/elgnay))

### Bug Fixes
* Fix the issue that too many SAR requests when lots of managed clusters registry once.([#152](https://github.com/open-cluster-management-io/registration-operator/pull/152) [@xuezhaojun](https://github.com/xuezhaojun))

### Removed & Deprecated
* Deprecated the ClusterSet and ClusterSetBinding v1alpha1 APIs. ([#148](https://github.com/open-cluster-management-io/registration-operator/pull/148), [#149](https://github.com/open-cluster-management-io/registration-operator/pull/149) [@elgnay](https://github.com/elgnay))
