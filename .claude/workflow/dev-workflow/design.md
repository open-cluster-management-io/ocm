# Design: Support policy network when creating namespace for ClusterManager

## Overview

Add a `--enable-network-policy` flag to the ClusterManager operator binary. When enabled, two NetworkPolicy manifest files are applied to the `open-cluster-management-hub` namespace as part of the hub resource reconciliation. When disabled (default), the NetworkPolicies are cleaned up if they exist. This follows the same conditional resource pattern used by AddOnManager and WorkController.

## Package & File Changes

### manifests/cluster-manager/hub/network-policy/

- `default-deny-all.yaml` (NEW) — NetworkPolicy that denies all Ingress and Egress for pods with label `open-cluster-management.io/created-by-clustermanager: {{ .ClusterManagerName }}`
- `allow-apiserver-egress.yaml` (NEW) — NetworkPolicy that allows Egress on UDP/TCP ports 53, 443, 6443 for the same pod selector

### manifests/

- `config.go` — Add `NetworkPolicyEnabled bool` field to `HubConfig` struct

### pkg/operator/operators/clustermanager/

- `options.go` — Add `EnableNetworkPolicy bool` field to `Options` struct

### pkg/cmd/hub/

- `operator.go` — Register `--enable-network-policy` flag via `flags.BoolVar()`

### pkg/operator/operators/clustermanager/controllers/clustermanagercontroller/

- `clustermanager_controller.go` — Pass `EnableNetworkPolicy` option to `HubConfig.NetworkPolicyEnabled`
- `clustermanager_hub_reconcile.go` — Add `networkPolicyFiles` variable, conditionally include in `getHubResources()`, clean up when disabled

### deploy/cluster-manager/ (Operator RBAC)

- `config/rbac/cluster_role.yaml` — Add `networking.k8s.io` networkpolicies rule (get, list, create, update, patch, delete)
- `chart/cluster-manager/templates/cluster_role.yaml` — Same networkpolicies rule for Helm chart
- `olm-catalog/latest/manifests/cluster-manager.clusterserviceversion.yaml` — Same networkpolicies rule for OLM CSV

### test/integration/operator/

- `clustermanager_test.go` — Add test cases for NetworkPolicy creation when flag enabled and absence when disabled

## Interface Definitions

No new interfaces needed. Only struct field additions:

```go
// In manifests/config.go - HubConfig struct
NetworkPolicyEnabled bool

// In pkg/operator/operators/clustermanager/options.go - Options struct
EnableNetworkPolicy bool
```

## Dependencies

- Internal: `pkg/operator/helpers` (existing ApplyDirectly, CleanUpStaticObject), `manifests` (existing embed)
- External: none
- API repo: no changes needed

## Implementation Modules

1. **Manifest files + HubConfig** — Create NetworkPolicy YAML templates and add config field
   - Source files: `manifests/cluster-manager/hub/network-policy/default-deny-all.yaml`, `manifests/cluster-manager/hub/network-policy/allow-apiserver-egress.yaml`, `manifests/config.go`
   - No test file for this module (tested via integration)
   - Key scenarios: YAML renders correctly with template variables

2. **Flag + conditional reconciliation + operator RBAC** — Add binary flag, wire it through to hub reconcile, add conditional apply/cleanup logic, update operator ClusterRole with networking.k8s.io permissions
   - Source files: `pkg/operator/operators/clustermanager/options.go`, `pkg/cmd/hub/operator.go`, `pkg/operator/operators/clustermanager/controllers/clustermanagercontroller/clustermanager_controller.go`, `pkg/operator/operators/clustermanager/controllers/clustermanagercontroller/clustermanager_hub_reconcile.go`, `deploy/cluster-manager/config/rbac/cluster_role.yaml`, `deploy/cluster-manager/chart/cluster-manager/templates/cluster_role.yaml`, `deploy/cluster-manager/olm-catalog/latest/manifests/cluster-manager.clusterserviceversion.yaml`
   - No unit test file (logic is simple flag wiring, tested via integration)
   - Key scenarios: flag disabled = no NetworkPolicy; flag enabled = NetworkPolicies applied; flag disabled after enabled = cleanup

3. **Integration tests** — Verify NetworkPolicy lifecycle with ClusterManager
   - Test file: `test/integration/operator/clustermanager_test.go`
   - Key scenarios:
     - Start operator with `EnableNetworkPolicy=true`, verify both NetworkPolicies exist in hub namespace
     - Start operator with `EnableNetworkPolicy=false` (default), verify no NetworkPolicies exist
     - Verify NetworkPolicies are cleaned up when ClusterManager is deleted

## Test Strategy

- Unit tests: none needed (no complex logic to unit test; flag wiring and manifest application are best tested via integration)
- Integration tests: `test/integration/operator/` suite
  - Add test case in `clustermanager_test.go` using existing `startHubOperator()` pattern with `EnableNetworkPolicy` set
  - Use `gomega.Eventually()` to check NetworkPolicy existence via `kubeClient.NetworkingV1().NetworkPolicies(hubNamespace).Get()`
  - Verify cleanup on deletion follows existing test patterns

## Risks & Mitigations

- **Pod selector mismatch**: The label value `open-cluster-management.io/created-by-clustermanager` is set to `clusterManager.GetName()` (typically `"hub"`) at runtime. The manifest must use `{{ .ClusterManagerName }}` template variable, not a hardcoded value.
- **envtest NetworkPolicy support**: envtest may not enforce NetworkPolicies at the network level, but we can still verify the resources are created/deleted. Integration tests check resource existence, not network behavior.
