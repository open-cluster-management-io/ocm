# Changelog since v0.9.0
All notable changes to this project will be documented in this file.

## v0.10.0

### New Features 
N/C

### Added
N/C

### Changes
* Upgrade golang to 1.19. ([#279](https://github.com/open-cluster-management-io/registration/pull/279) [@skeeey](https://github.com/skeeey))
* Upgrade ginkgo to v2. ([#283](https://github.com/open-cluster-management-io/registration/pull/283) [@xuezhaojun](https://github.com/xuezhaojun))
* Upgrade github actions checkout and setup-go to v3. ([#289](https://github.com/open-cluster-management-io/registration/pull/289) [@ycyaoxdu](https://github.com/ycyaoxdu))
* Reimplement webhooks using controller runtime. ([#278](https://github.com/open-cluster-management-io/registration/pull/278) [@ldpliu](https://github.com/ldpliu))
* Use common labels in api repo ([#290](https://github.com/open-cluster-management-io/registration/pull/290) [@qiujian16](https://github.com/qiujian16))


### Bug Fixes
* Avoid frequent CSR creation. ([#277](https://github.com/open-cluster-management-io/registration/pull/277) [@qiujian16](https://github.com/qiujian16))
* Ensure the lease of a cluster is updated after the cluster is restored. ([#280](https://github.com/open-cluster-management-io/registration/pull/280) [@skeeey](https://github.com/skeeey))
* Rollback add-on lease part to support lower version Kubernetes (less than 1.14)([#291](https://github.com/open-cluster-management-io/registration/pull/291) [@skeeey](https://github.com/skeeey))


### Removed & Deprecated
N/C
