# Changelog 
All notable changes to this project will be documented in this file.

## v0.5.0

### New Features
* Support addOn configuration apis. ([#120](https://github.com/open-cluster-management-io/addon-framework/pull/120),[#125](https://github.com/open-cluster-management-io/addon-framework/pull/125) [@skeeey](https://github.com/skeeey))
* Allow trigger reconcile externally. ([#126](https://github.com/open-cluster-management-io/addon-framework/pull/126) [@qiujian16](https://github.com/qiujian16))

### Added
* Add managedClusterFilter to filter clusters to install addOn. ([#110](https://github.com/open-cluster-management-io/addon-framework/pull/110) [@xuezhaojun](https://github.com/xuezhaojun))
* Add an annotation to disable addon automatic installation. ([#112](https://github.com/open-cluster-management-io/addon-framework/pull/112) [@zhujian7](https://github.com/zhujian7) )
* Add work builder to build and apply multi works. ([#122](https://github.com/open-cluster-management-io/addon-framework/pull/122),[#124](https://github.com/open-cluster-management-io/addon-framework/pull/124) [@zhiweiyin318](https://github.com/zhiweiyin318))

### Changes
* Refactor the hosting controller into the addondeploy controller. ([#111](https://github.com/open-cluster-management-io/addon-framework/pull/111) [@qiujian16](https://github.com/qiujian16))
* Refactor addon examples deploy and add a busybox addon example. ([#114](https://github.com/open-cluster-management-io/addon-framework/pull/114) [@zhiweiyin318](https://github.com/zhiweiyin318))
* Refactor work apply. ([#119](https://github.com/open-cluster-management-io/addon-framework/pull/119) [@zhiweiyin318](https://github.com/zhiweiyin318))
* Use index in agent deploy. ([#117](https://github.com/open-cluster-management-io/addon-framework/pull/117) [@qiujian16](https://github.com/qiujian16))
* Update go version 1.18 and dep API version. ([#113](https://github.com/open-cluster-management-io/addon-framework/pull/113/files),[#123](https://github.com/open-cluster-management-io/addon-framework/pull/123) [@zhiweiyin318](https://github.com/zhiweiyin318), [#121](https://github.com/open-cluster-management-io/addon-framework/pull/121) [@JustinKuli](https://github.com/JustinKuli))

### Bug Fixes
* Fix to empty label in helloworld helm example. ([#109](https://github.com/open-cluster-management-io/addon-framework/pull/109) [@zhujian7](https://github.com/zhujian7))
* Fix cannot cleanup manifestWork when the cluster is deleting. ([#115](https://github.com/open-cluster-management-io/addon-framework/pull/115) [@zhiweiyin318](https://github.com/zhiweiyin318))
* Fix multi-resource file convert to manifests. ([#118](https://github.com/open-cluster-management-io/addon-framework/pull/118) [@xuezhaojun](https://github.com/xuezhaojun))

### Removed & Deprecated
* Remove template validation in building AgentAddon. ([#129](https://github.com/open-cluster-management-io/addon-framework/pull/129) [@zhiweiyin318](https://github.com/zhiweiyin318))
