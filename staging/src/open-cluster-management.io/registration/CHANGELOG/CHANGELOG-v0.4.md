# Changelog since v0.3.0
All notable changes to this project will be documented in this file.

## v0.4.0

### New Features 
* Support managedClusterAddon registration and maintain the lease of managedClusterAddon.

### Added
* Support DNS names for client cert. 
* Support to use non-root user to run registration.
* Support to use null, true or false as managed cluster name.
* Add clusterClaim feature gate.

### Changes
* Upgrade k8s api lib to v0.21.1.
* Upgrade k8s client-go lib to v0.21.1.
* Upgrade OCM CRDs to v1.
* Upgrade Go to 1.16.
* Merge managed cluster capacity status.
* Reset csr name only if csr sync failed or secret saved.
* Use `livez` API instead of `readyz` API to check the status of managed cluster kube-apiserver.
* Use `clusterRole` instead of `role` for managed cluster add-ons to reduce number of hub resources.

### Bug Fixes
* Fix the managedClusterAddon lease compatibility issue.
* Fix the invalid client certificate issue.

### Removed & Deprecated
N/C
