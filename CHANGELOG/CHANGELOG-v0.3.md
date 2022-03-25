# Changelog 
All notable changes to this project will be documented in this file.

## v0.3.0

### New Features
* Feat: Conditional CSR controller spawning ([#84](https://github.com/open-cluster-management-io/addon-framework/pull/84) [@yue9944882](https://github.com/yue9944882))
* add pre-delete hook manifest ([#71](https://github.com/open-cluster-management-io/addon-framework/pull/71) [@zhiweiyin318](https://github.com/zhiweiyin318))
* Feat: Addon rbac permission utilities ([#76](https://github.com/open-cluster-management-io/addon-framework/pull/76) [@yue9944882](https://github.com/yue9944882))
* Feat: addon approver v1beta1 support ([#85](https://github.com/open-cluster-management-io/addon-framework/pull/85) [@yue9944882](https://github.com/yue9944882))

### Added
* Add trim crds descprition configration option ([#78](https://github.com/open-cluster-management-io/addon-framework/pull/78) [@zhiweiyin318](https://github.com/zhiweiyin318))
* Add an install strategy by label selector ([#79](https://github.com/open-cluster-management-io/addon-framework/pull/79) [@qiujian16](https://github.com/qiujian16))
* Add work probe mode ([#78](https://github.com/open-cluster-management-io/addon-framework/pull/78) [@qiujian16](https://github.com/qiujian16))

### Changes
* Implement an addon template using embed.FS ([#57](https://github.com/open-cluster-management-io/addon-framework/pull/57) [@zhiweiyin318](https://github.com/zhiweiyin318))
* Add work cache in agent deploy ([#69](https://github.com/open-cluster-management-io/addon-framework/pull/69) [@qiujian16](https://github.com/qiujian16))

### Bug Fixes
* Eliminate confused message ([#66](https://github.com/open-cluster-management-io/addon-framework/pull/66) [@skeeey](https://github.com/skeeey))
* Resign upon san mismatch ([#67](https://github.com/open-cluster-management-io/addon-framework/pull/67) [@yue9944882](https://github.com/yue9944882))
* Order manifests from HelmAgentAddon ([#70](https://github.com/open-cluster-management-io/addon-framework/pull/70) [@JustinKuli](https://github.com/JustinKuli))
* Skip templates if 'Kind' not present ([#72](https://github.com/open-cluster-management-io/addon-framework/pull/72) [@JustinKuli](https://github.com/JustinKuli))
* Synced fix to templateAgentAddon and add ut for them ([#73](https://github.com/open-cluster-management-io/addon-framework/pull/73) [@zhiweiyin318](https://github.com/zhiweiyin318))
* Should apply pre-delete hook when the managedCluster is deleting ([#81](https://github.com/open-cluster-management-io/addon-framework/pull/81) [@zhiweiyin318](https://github.com/zhiweiyin318))
* Fix Klog flags disppeared ([#83](https://github.com/open-cluster-management-io/addon-framework/pull/83) [@JiahaoWei-RH](https://github.com/JiahaoWei-RH))
* Fix readme url ([#87](https://github.com/open-cluster-management-io/addon-framework/pull/83) [@ldpliu](https://github.com/ldpliu))
* Fix spec comparison in applyWork ([#89](https://github.com/open-cluster-management-io/addon-framework/pull/89) [@qiujian16](https://github.com/qiujian16))

### Removed & Deprecated
N/C