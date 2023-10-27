package v1alpha1

import (
	"k8s.io/apimachinery/pkg/util/intstr"
)

// +k8s:deepcopy-gen=true

// RolloutStrategy API used by workload applier APIs to define how the workload will be applied to the selected clusters by the Placement and DecisionStrategy.

const (
	//All means apply the workload to all clusters in the decision groups at once.
	All string = "All"
	//Progressive means apply the workload to the selected clusters progressively per cluster.
	Progressive string = "Progressive"
	//ProgressivePerGroup means apply the workload to the selected clusters progressively per group.
	ProgressivePerGroup string = "ProgressivePerGroup"
)

// Rollout strategy to apply workload to the selected clusters by Placement and DecisionStrategy.
type RolloutStrategy struct {
	// Rollout strategy Types are All, Progressive and ProgressivePerGroup
	// 1) All means apply the workload to all clusters in the decision groups at once.
	// 2) Progressive means apply the workload to the selected clusters progressively per cluster. The workload will not be applied to the next cluster unless one of the current applied clusters reach the successful state or timeout.
	// 3) ProgressivePerGroup means apply the workload to decisionGroup clusters progressively per group. The workload will not be applied to the next decisionGroup unless all clusters in the current group reach the successful state or timeout.
	// +kubebuilder:validation:Enum=All;Progressive;ProgressivePerGroup
	// +kubebuilder:default:=All
	// +optional
	Type string `json:"type,omitempty"`

	// All define required fields for RolloutStrategy type All
	// +optional
	All *RolloutAll `json:"all,omitempty"`

	// Progressive define required fields for RolloutStrategy type Progressive
	// +optional
	Progressive *RolloutProgressive `json:"progressive,omitempty"`

	// ProgressivePerGroup define required fields for RolloutStrategy type ProgressivePerGroup
	// +optional
	ProgressivePerGroup *RolloutProgressivePerGroup `json:"progressivePerGroup,omitempty"`
}

// Timeout to consider while applying the workload.
type Timeout struct {
	// Timeout define how long workload applier controller will wait till workload reach successful state in the cluster.
	// Timeout default value is None meaning the workload applier will not proceed apply workload to other clusters if did not reach the successful state.
	// Timeout must be defined in [0-9h]|[0-9m]|[0-9s] format examples; 2h , 90m , 360s
	// +kubebuilder:validation:Pattern="^(([0-9])+[h|m|s])|None$"
	// +kubebuilder:default:=None
	// +optional
	Timeout string `json:"timeout,omitempty"`
}

// MandatoryDecisionGroup set the decision group name or group index.
// GroupName is considered first to select the decisionGroups then GroupIndex.
type MandatoryDecisionGroup struct {
	// GroupName of the decision group should match the placementDecisions label value with label key cluster.open-cluster-management.io/decision-group-name
	// +optional
	GroupName string `json:"groupName,omitempty"`

	// GroupIndex of the decision group should match the placementDecisions label value with label key cluster.open-cluster-management.io/decision-group-index
	// +optional
	GroupIndex int32 `json:"groupIndex,omitempty"`
}

// MandatoryDecisionGroups
type MandatoryDecisionGroups struct {
	// List of the decision groups names or indexes to apply the workload first and fail if workload did not reach successful state.
	// GroupName or GroupIndex must match with the decisionGroups defined in the placement's decisionStrategy
	// +optional
	MandatoryDecisionGroups []MandatoryDecisionGroup `json:"mandatoryDecisionGroups,omitempty"`
}

// RolloutAll is a RolloutStrategy Type
type RolloutAll struct {
	// +optional
	Timeout `json:",inline"`
}

// RolloutProgressivePerGroup is a RolloutStrategy Type
type RolloutProgressivePerGroup struct {
	// +optional
	MandatoryDecisionGroups `json:",inline"`

	// +optional
	Timeout `json:",inline"`
}

// RolloutProgressive is a RolloutStrategy Type
type RolloutProgressive struct {
	// +optional
	MandatoryDecisionGroups `json:",inline"`

	// MaxConcurrency is the max number of clusters to deploy workload concurrently. The default value for MaxConcurrency is determined from the clustersPerDecisionGroup defined in the placement->DecisionStrategy.
	// +kubebuilder:validation:Pattern="^((100|[0-9]{1,2})%|[0-9]+)$"
	// +kubebuilder:validation:XIntOrString
	// +optional
	MaxConcurrency intstr.IntOrString `json:"maxConcurrency,omitempty"`

	// +optional
	Timeout `json:",inline"`
}
