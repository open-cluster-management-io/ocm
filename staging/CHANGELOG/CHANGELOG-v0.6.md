# Changelog 
All notable changes to this project will be documented in this file.

## v0.6.1

### New Features
* Add addon manager for install strategy ([#150](https://github.com/open-cluster-management-io/addon-framework/pull/150) [@qiujian16](https://github.com/qiujian16))

### Added
* Add the Helm default variable of hostingClusterCapabilities ([#149](https://github.com/open-cluster-management-io/addon-framework/pull/149) [@mprahl](https://github.com/mprahl))

### Changes
* Upgrade imagebuilder to 1.2.3. ([#147](https://github.com/open-cluster-management-io/addon-framework/pull/147) [@zhujian7](https://github.com/zhujian7))
* Refactor for cmd factory. ([#148](https://github.com/open-cluster-management-io/addon-framework/pull/148) [@qiujian16](https://github.com/qiujian16))
* Use golang:1.19 in the build Dockerfile. ([#153](https://github.com/open-cluster-management-io/addon-framework/pull/153) [@skitt](https://github.com/skitt))
* Bump to controller-runtime 0.14.4. ([#141](https://github.com/open-cluster-management-io/addon-framework/pull/141) [@skitt](https://github.com/skitt))

### Bug Fixes
* Set the manifest limit size to 500k. ([#154](https://github.com/open-cluster-management-io/addon-framework/pull/154) [@zhiweiyin318](https://github.com/zhiweiyin318))

### Removed & Deprecated
* Helper func `GetAddOnDeloymentConfigValues` and `GetAddOnDeloymentConfigValues` are deprecated.  ([#152](https://github.com/open-cluster-management-io/addon-framework/pull/152) [@zhiweiyin318](https://github.com/zhiweiyin318))

## v0.6.0

### New Features
* Support manifests deletion orphan. ([#131](https://github.com/open-cluster-management-io/addon-framework/pull/131),[#137](https://github.com/open-cluster-management-io/addon-framework/pull/137) [@zhiweiyin318](https://github.com/zhiweiyin318))

### Added
* Make hub/managed kubeconfig secret name overridable. ([#133](https://github.com/open-cluster-management-io/addon-framework/pull/133) [@elgnay](https://github.com/elgnay))

### Changes
* Replace labbel/annotation to use those in api repo. ([#140](https://github.com/open-cluster-management-io/addon-framework/pull/140) [@qiujian16](https://github.com/qiujian16))
* Refactor workappier and workbuilder using api lib. ([#142](https://github.com/open-cluster-management-io/addon-framework/pull/142) [@zhiweiyin318](https://github.com/zhiweiyin318))
* Add pre-delete and hostedLocation annotation and will deprecate the labels. ([#132](https://github.com/open-cluster-management-io/addon-framework/pull/132) [@zhiweiyin318](https://github.com/zhiweiyin318))
* Replace deprecated ioutil functions. ([#143](https://github.com/open-cluster-management-io/addon-framework/pull/143) [@skitt](https://github.com/skitt))
* Ignore empty supported configs check if no configs in the mca. ([#136](https://github.com/open-cluster-management-io/addon-framework/pull/136) [@skeeey](https://github.com/skeeey))

### Bug Fixes
* Fix the manifestWork in hosted cluster ns is not deleted in hosted mode. ([#139](https://github.com/open-cluster-management-io/addon-framework/pull/139) [@zhiweiyin318](https://github.com/zhiweiyin318))

### Removed & Deprecated
* Remove unsupported config condition if config is corrected. ([#135](https://github.com/open-cluster-management-io/addon-framework/pull/135) [@skeeey](https://github.com/skeeey))
