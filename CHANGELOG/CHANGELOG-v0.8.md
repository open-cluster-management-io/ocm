# Changelog since v0.7.0
All notable changes to this project will be documented in this file.

## v0.8.0

### New Features 
* Support global clusterset. ([#251](https://github.com/open-cluster-management-io/registration/pull/251) [@ldpliu](https://github.com/ldpliu))

### Added
* Support v1beta1 CSR API for hub registration-controller. ([#259](https://github.com/open-cluster-management-io/registration/pull/259) [@Promacanthus](https://github.com/Promacanthus))
* Enable log flags. ([#254](https://github.com/open-cluster-management-io/registration/pull/254) [@skeeey](https://github.com/skeeey))
* Support to update qps and burst for webhook. ([#249](https://github.com/open-cluster-management-io/registration/pull/249) [@ldpliu](https://github.com/ldpliu))
* Add `bound` condition to ClusterSetBinding when a ClusterSetBinding is bound to a ClusterSet. ([#248](https://github.com/open-cluster-management-io/registration/pull/248) [@qiujian16](https://github.com/qiujian16))
* Add client cert condition when cert is rotated. ([#246](https://github.com/open-cluster-management-io/registration/pull/246) [@qiujian16](https://github.com/qiujian16))
* Add lint check in make verify. ([#243](https://github.com/open-cluster-management-io/registration/pull/243) [@qiujian16](https://github.com/qiujian16))
* Add join permission on defatult clusterset. ([#238](https://github.com/open-cluster-management-io/registration/pull/238) [@elgnay](https://github.com/elgnay))
* Publish release artifact after image are built. ([#231](https://github.com/open-cluster-management-io/registration/pull/231) [@yue9944882](https://github.com/yue9944882))
* Support multi arch image. ([#227](https://github.com/open-cluster-management-io/registration/pull/227) [@qiujian16](https://github.com/qiujian16))
* Allow ManagedCluster creation with taint.timeAdded specified ([#224](https://github.com/open-cluster-management-io/registration/pull/224) [@elgnay](https://github.com/elgnay))

### Changes
* Use fine-grained permissions for registration. ([#255](https://github.com/open-cluster-management-io/registration/pull/255) [@haoqing0110](https://github.com/haoqing0110))
* Use patch method to update ManagedCluster status and finalizers to reduce conflict errors ([#244](https://github.com/open-cluster-management-io/registration/pull/244) [@qiujian16](https://github.com/qiujian16))
* Update golang to `1.18`. ([#247](https://github.com/open-cluster-management-io/registration/pull/247) [@qiujian16](https://github.com/qiujian16))
* Update dependent libraries ([#229](https://github.com/open-cluster-management-io/registration/pull/229) [@skeeey](https://github.com/skeeey))
* Reduce the get request for ManagedCluster when checking the lease of managed clusters on hub cluster. ([#226](https://github.com/open-cluster-management-io/registration/pull/226) [@skeeey](https://github.com/skeeey))
* Improve the performance of hub client certification creation/rotation by reducing the informer queue size and adding label filter for CSR informer. ([#223](https://github.com/open-cluster-management-io/registration/pull/223) [@qiujian16](https://github.com/qiujian16))


### Bug Fixes
* Fix sometimes clusterset status doesn't update after removing a managedcluster from the clusterset. ([#234](https://github.com/open-cluster-management-io/registration/pull/234) [@elgnay](https://github.com/elgnay))
* Using the default clusterset when the value of the clusterset label is empty. ([#222](https://github.com/open-cluster-management-io/registration/pull/222) [@ldpliu](https://github.com/ldpliu))


### Removed & Deprecated
* Remvoe add default set label controller from `managedclusterset` package. ([#250](https://github.com/open-cluster-management-io/registration/pull/250) [@ldpliu](https://github.com/ldpliu))
