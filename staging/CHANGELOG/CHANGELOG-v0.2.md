# Changelog 
All notable changes to this project will be documented in this file.

## v0.2.0

### New Features
* Helm chart interface to implement an addon agent. ([#62](https://github.com/open-cluster-management-io/addon-framework/pull/62) [@zhiweiyin318](https://github.com/zhiweiyin318))

### Added
* Add certrotation from registration-operator to addon-framework. ([#42](https://github.com/open-cluster-management-io/addon-framework/pull/42) [@xuezhaojun](https://github.com/xuezhaojun))
* Feat: apply an additional parent check when looking for issuer. ([#53](https://github.com/open-cluster-management-io/addon-framework/pull/53) [@yue9944882](https://github.com/yue9944882))
* Add csr helpers func. ([#59](https://github.com/open-cluster-management-io/addon-framework/pull/59) [@qiujian16](https://github.com/qiujian16))
* Feat: Factor out a health prober option for addons. ([#51](https://github.com/open-cluster-management-io/addon-framework/pull/51) [@yue9944882](https://github.com/yue9944882))
* Preserve last observed generation. ([#63](https://github.com/open-cluster-management-io/addon-framework/pull/63) [@yue9944882](https://github.com/yue9944882))

### Changes
* Remove bindata and update api. ([#47](https://github.com/open-cluster-management-io/addon-framework/pull/47) [@qiujian16](https://github.com/qiujian16))
* Make the rotated target cert extensible. ([#45](https://github.com/open-cluster-management-io/addon-framework/pull/45) [@yue9944882](https://github.com/yue9944882))
* Clarify addon agent's doc. ([#48](https://github.com/open-cluster-management-io/addon-framework/pull/48) [@yue9944882](https://github.com/yue9944882))
* Update the content of deploying helloworld agent in the readme. ([#50](https://github.com/open-cluster-management-io/addon-framework/pull/50) [@zhujian7](https://github.com/zhujian7))
* Replace md5 with sha56. ([#58](https://github.com/open-cluster-management-io/addon-framework/pull/58) [@xuezhaojun](https://github.com/xuezhaojun))
* Modify readme: Deploy the helloworld addon. ([#60](https://github.com/open-cluster-management-io/addon-framework/pull/60) [@ycyaoxdu](https://github.com/ycyaoxdu))

### Bug Fixes
* If manged cluster is deleting, dont recover addon's manifestwork. ([#44](https://github.com/open-cluster-management-io/addon-framework/pull/44) [@skeeey](https://github.com/skeeey))

### Removed & Deprecated
N/C