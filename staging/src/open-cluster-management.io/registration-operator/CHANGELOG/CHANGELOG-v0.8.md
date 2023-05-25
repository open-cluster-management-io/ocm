# Changelog since v0.7.0
All notable changes to this project will be documented in this file.

## v0.8.0

### New Features
* Support Multi-arch images. ([#240](https://github.com/open-cluster-management-io/registration-operator/pull/240) [@yue9944882](https://github.com/yue9944882))
* Support Hosted mode. ([#227](https://github.com/open-cluster-management-io/registration-operator/pull/227) [@elgnay](https://github.com/elgnay), [#256](https://github.com/open-cluster-management-io/registration-operator/pull/256) [@zhujian7](https://github.com/zhujian7))
* Support to sync serviceAccount by token request. ([#259](https://github.com/open-cluster-management-io/registration-operator/pull/259) [@qiujian16](https://github.com/qiujian16))
* Support hubRegistrationFeatureGates and spokeRegistrationFeatureGates. ([#230](https://github.com/open-cluster-management-io/registration-operator/pull/230) [@ivan-cai](https://github.com/ivan-cai))

### Added
* Add goci lint. ([#243](https://github.com/open-cluster-management-io/registration-operator/pull/243) [@xuezhaojun](https://github.com/xuezhaojun))
* Add log flags. ([#249](https://github.com/open-cluster-management-io/registration-operator/pull/249) [@skeeey](https://github.com/skeeey))
* Add controller to sync image pull secret into addon namespaces. ([#253](https://github.com/open-cluster-management-io/registration-operator/pull/253) [@xuezhaojun](https://github.com/xuezhaojun))

### Changes
* Upgrade some libraries. ([#228](https://github.com/open-cluster-management-io/registration-operator/pull/228) [@zhiweiyin318](https://github.com/zhiweiyin318))
* Update golang builder in Dockerfile. ([#237](https://github.com/open-cluster-management-io/registration-operator/pull/237) [@elgnay](https://github.com/elgnay))
* Update makefile to pass IMAGE_TAG to make images. ([#239](https://github.com/open-cluster-management-io/registration-operator/pull/239) [@yue9944882](https://github.com/yue9944882))
* Update the managedClusterSet API and. ([#242](https://github.com/open-cluster-management-io/registration-operator/pull/242) [@ldpliu](https://github.com/ldpliu))
* Update the join permission. ([#236](https://github.com/open-cluster-management-io/registration-operator/pull/236) [@elgnay](https://github.com/elgnay), [#241](https://github.com/open-cluster-management-io/registration-operator/pull/241) [@ldpliu](https://github.com/ldpliu), [#248](https://github.com/open-cluster-management-io/registration-operator/pull/248) [@ldpliu](https://github.com/ldpliu))
* Update file name to reflect the change in Makefile. ([#245](https://github.com/open-cluster-management-io/registration-operator/pull/245) [@yitiangf](https://github.com/yitiangf))
* Split registration and work permissions. ([#250](https://github.com/open-cluster-management-io/registration-operator/pull/250),[#252](https://github.com/open-cluster-management-io/registration-operator/pull/252) [@haoqing0110](https://github.com/haoqing0110))
* Keep appliedManifestWork & managedClusterClaim CRDs when uninstalling klusterlet. ([#255](https://github.com/open-cluster-management-io/registration-operator/pull/255) [@elgnay](https://github.com/elgnay))
* Add HubApiServerHostAlias for registration-agent and work-agent. ([#258](https://github.com/open-cluster-management-io/registration-operator/pull/258) [@Promacanthus](https://github.com/Promacanthus))

### Bug Fixes
* Fix the issue that there is no lease permission for leader election. ([#229](https://github.com/open-cluster-management-io/registration-operator/pull/229) [@qiujian16](https://github.com/qiujian16), [#231](https://github.com/open-cluster-management-io/registration-operator/pull/231) [@haoqing0110](https://github.com/haoqing0110), [#232](https://github.com/open-cluster-management-io/registration-operator/pull/232) [@elgnay](https://github.com/elgnay), [#233](https://github.com/open-cluster-management-io/registration-operator/pull/233) [@skeeey](https://github.com/skeeey), [#260](https://github.com/open-cluster-management-io/registration-operator/pull/260) [@haoqing0110](https://github.com/haoqing0110))
* Fix the issue that there is some missing permission on kube v1.11.0. ([#234](https://github.com/open-cluster-management-io/registration-operator/pull/234) [@elgnay](https://github.com/elgnay))
* Fix the issue that it is failed to apply Klusterlet after upgrade. ([#257](https://github.com/open-cluster-management-io/registration-operator/pull/257) [@haoqing0110](https://github.com/haoqing0110))

### Removed & Deprecated
* Prune unused dockerfile cmd. ([#238](https://github.com/open-cluster-management-io/registration-operator/pull/238) [@yue9944882](https://github.com/yue9944882))