# Changelog since v0.4.0
All notable changes to this project will be documented in this file.

## v0.5.0

### New Features 
* Support delete option. User is able to specify the deletion propagation strategy for resources in a manifestwork by setting `DeleteOption` in the spec of the manifestwork. ([#90](https://github.com/open-cluster-management-io/work/pull/90) [@qiujian16](https://github.com/qiujian16))

### Added
* Add generation in manifestwork status. ([#95](https://github.com/open-cluster-management-io/work/pull/95) [@qiujian16](https://github.com/qiujian16))

### Changes
* Upgrade library-go and api. ([#96](https://github.com/open-cluster-management-io/work/pull/96) [@qiujian16](https://github.com/qiujian16))
* Reduce the numbe of GET request of manifestwork. ([#98](https://github.com/open-cluster-management-io/work/pull/98) [@elgnay](https://github.com/elgnay))

### Bug Fixes
* Resolve apply conflict. ([#89](https://github.com/open-cluster-management-io/work/pull/89) [@qiujian16](https://github.com/qiujian16))
* Fix owners merge in apply unstructured. ([#99](https://github.com/open-cluster-management-io/work/pull/99) [@qiujian16](https://github.com/qiujian16))

### Removed & Deprecated
N/C
