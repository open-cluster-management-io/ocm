# Requirements: Support policy network when creating namespace for ClusterManager

## Type

feature

## Summary

When the ClusterManager operator creates the `open-cluster-management-hub` namespace, it should optionally create two NetworkPolicy resources to enforce network isolation for hub components. This feature is controlled by a flag on the workmanager controller binary and is disabled by default.

## Scenarios

- As a cluster administrator, I want to enable network policies for the ClusterManager hub namespace, so that hub component pods have restricted network access by default.
- When the feature flag is enabled, the system should create two NetworkPolicy CRs in the `open-cluster-management-hub` namespace:
  1. `default-deny-all` — denies all Ingress and Egress traffic for selected pods
  2. `allow-apiserver-egress` — allows Egress on UDP/TCP ports 53, 443, and 6443 for selected pods
- Both NetworkPolicies select pods with the label `open-cluster-management.io/created-by-clustermanager: cluster-manager`.
- When the feature flag is disabled (default), no NetworkPolicy resources are created — existing behavior is preserved.
- When ClusterManager is deleted, the NetworkPolicies in the namespace should be cleaned up along with other resources.

## Acceptance Criteria

- [ ] A flag on the workmanager controller binary controls whether network policy creation is enabled (default: disabled)
- [ ] When enabled, a `default-deny-all` NetworkPolicy is created in `open-cluster-management-hub` namespace that denies all Ingress and Egress for pods with label `open-cluster-management.io/created-by-clustermanager: cluster-manager`
- [ ] When enabled, an `allow-apiserver-egress` NetworkPolicy is created in `open-cluster-management-hub` namespace that allows Egress on UDP/TCP ports 53, 443, and 6443 for the same pod selector
- [ ] When the flag is disabled, no NetworkPolicies are created (backward compatible)
- [ ] NetworkPolicies are cleaned up when ClusterManager is deleted
- [ ] Unit tests cover NetworkPolicy creation and the feature flag logic
- [ ] Integration tests verify NetworkPolicy creation/deletion with ClusterManager lifecycle

## Constraints

- Backward compatible: default is disabled, existing ClusterManager deployments are unaffected
- Only applies to the `open-cluster-management-hub` namespace (not hosted mode management namespace)
- NetworkPolicies must not break OCM hub component communication when the allowed egress ports are properly configured

## Dependencies

- No API repo changes needed (feature is controlled by a binary flag, not a CRD field)
- No other external dependencies

## Out of Scope

- NetworkPolicy support for Klusterlet (spoke-side)
- NetworkPolicy for hosted mode management namespace
- Custom/user-defined NetworkPolicy rules
- Ingress allow rules (only egress allow is in scope)
