# Changelog since v0.9.0
All notable changes to this project will be documented in this file.

## v0.10.0

### New Features
N/C

### Added
* Add inital skeleton for placeManifestWork controller. ([#171](https://github.com/open-cluster-management-io/work/pull/171) [@serngawy](https://github.com/serngawy))
* Add informer setup for placemanifestwork. ([#175](https://github.com/open-cluster-management-io/work/pull/175) [@qiujian16](https://github.com/qiujian16))

### Changes
* Upgrade to golang1.19. ([#163](https://github.com/open-cluster-management-io/work/pull/163) [@elgnay](https://github.com/elgnay))
* Delete unmanaged AppliedManifestWork. ([#161](https://github.com/open-cluster-management-io/work/pull/161) [@qiujian16](https://github.com/qiujian16))
* Use controller runtime to implement webhook. ([#164](https://github.com/open-cluster-management-io/work/pull/164) [@ldpliu](https://github.com/ldpliu))
* Set min tls version. ([#166](https://github.com/open-cluster-management-io/work/pull/166) [@ldpliu](https://github.com/ldpliu))
* Update ginkgo to v2. ([#168](https://github.com/open-cluster-management-io/work/pull/168) [@zhujian7](https://github.com/zhujian7))
* Cache the executor validation results. ([#165](https://github.com/open-cluster-management-io/work/pull/165) [@zhujian7](https://github.com/zhujian7))
* Delete resource even if it has non-appliedmanifestwork owners. ([#170](https://github.com/open-cluster-management-io/work/pull/170) [@zhujian7](https://github.com/zhujian7))
* Reduce default sync interval to 10s. ([#167](https://github.com/open-cluster-management-io/work/pull/167) [@qiujian16](https://github.com/qiujian16))
* Clear the comments for deleting unmanaged appliedmanifestworks. ([#174](https://github.com/open-cluster-management-io/work/pull/174) [@skeeey](https://github.com/skeeey))
* Split logic of controller to multiple reconcilers. ([#176](https://github.com/open-cluster-management-io/work/pull/176) [@qiujian16](https://github.com/qiujian16))


### Bug Fixes
* Delete old applied work after its new work is applied. ([#172](https://github.com/open-cluster-management-io/work/pull/172) [@skeeey](https://github.com/skeeey))

### Removed & Deprecated
N/C
