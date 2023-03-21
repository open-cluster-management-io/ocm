# Changelog since v0.2.0
All notable changes to this project will be documented in this file.

## v0.3.0

### New Features
* Support cert rotation for webhooks.

### Added
* Add cert rotation controller.
* Add metrics permissions.
* Create a new Makefile target for spoke deploy on kind.
* Add crd & rbac rule for ClusterClaim controller.

### Changes
* Upgrade operator-sdk to v1.1.0. 
* Update ManagedClusterSet api to make ManagedClusterSet exclusive.
* Using hub kubeconfig secret controller instead of mounting the secret.

### Bug Fixes
* Fix wrong condition message in klusterlet.
* Fix wrong image path.
* Fix kind spoke deploy.

### Removed & Deprecated
    N/C
