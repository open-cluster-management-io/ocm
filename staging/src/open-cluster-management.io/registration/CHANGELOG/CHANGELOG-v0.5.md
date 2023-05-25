# Changelog since v0.4.0
All notable changes to this project will be documented in this file.

## v0.5.0

### New Features 
* Support to generate feature labels on the managed cluster to allow user select managed clusters with feature
labels by placement API.
([#164](https://github.com/open-cluster-management-io/registration/pull/164) [@elgnay](https://github.com/elgnay))

### Added
* Support to report all resource information of all managed cluster nodes to the `managedcluster` status.
([#165](https://github.com/open-cluster-management-io/registration/pull/165) [@suigh](https://github.com/suigh))

### Changes
* Use golang `embed` package to replace `go-bindata` library.
([#166](https://github.com/open-cluster-management-io/registration/pull/166) [@qiujian16](https://github.com/qiujian16))
* Upgrade `clusterset` API to `v1beta1`.
([#169](https://github.com/open-cluster-management-io/registration/pull/169) [@elgnay](https://github.com/elgnay))

### Bug Fixes
* Fix insufficient events permission granted by hub clusterrole.
([#168](https://github.com/open-cluster-management-io/registration/pull/168) [@yue9944882](https://github.com/yue9944882))
* Fix when transfering one managed clusrter from one clusterset to another, the clusterset status update slow issue.
([#171](https://github.com/open-cluster-management-io/registration/pull/171) [@elgnay](https://github.com/elgnay))

### Removed & Deprecated
N/C
