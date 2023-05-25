# Changelog since v0.2.0
All notable changes to this project will be documented in this file.

## v0.3.0

### New Features
* Support extensible scheduling. User is able to select clusters based on customized scores. ([#51](https://github.com/open-cluster-management-io/placement/pull/51) [@haoqing0110](https://github.com/haoqing0110))

### Added
* Add scalability test for 100/1000/2000 managedclusters. ([#41](https://github.com/open-cluster-management-io/placement/pull/41) [@suigh](https://github.com/suigh))
* Add scalability test for placement create/update. ([#44](https://github.com/open-cluster-management-io/placement/pull/44) [@suigh](https://github.com/suigh))
* Add scalability test doc. ([#45](https://github.com/open-cluster-management-io/placement/pull/45) [@suigh](https://github.com/suigh))
* Add resync controller to resync placements using customized scores. ([#55](https://github.com/open-cluster-management-io/placement/pull/55) [@haoqing0110](https://github.com/haoqing0110))

### Changes
* Update Golang to v1.17. ([#57](https://github.com/open-cluster-management-io/placement/pull/57) [@haoqing0110](https://github.com/haoqing0110))

### Bug Fixes
* Fix [error reported after creating more than 1000 managedclusters](https://github.com/open-cluster-management-io/placement/issues/40) with adding length check for event. ([#43](https://github.com/open-cluster-management-io/placement/pull/43) [@suigh](https://github.com/suigh)) 
* Remind user to deploy placement controller before running some test cases. ([#47](https://github.com/open-cluster-management-io/placement/pull/47) [@suigh](https://github.com/suigh)) 
* Update golang.org/x/crypto to fix CVE-2021-43565. ([#58](https://github.com/open-cluster-management-io/placement/pull/58) [@haoqing0110](https://github.com/haoqing0110))

### Removed & Deprecated
N/C
