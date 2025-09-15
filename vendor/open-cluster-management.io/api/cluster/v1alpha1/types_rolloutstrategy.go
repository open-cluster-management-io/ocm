// Copyright Contributors to the Open Cluster Management project
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// +k8s:deepcopy-gen=true

// RolloutStrategy API used by workload applier APIs to define how the workload will be applied to
// the selected clusters by the Placement and DecisionStrategy.

type RolloutType string

const (
	//All means apply the workload to all clusters in the decision groups at once.
	All RolloutType = "All"
	//Progressive means apply the workload to the selected clusters progressively per cluster.
	Progressive RolloutType = "Progressive"
	//ProgressivePerGroup means apply the workload to the selected clusters progressively per group.
	ProgressivePerGroup RolloutType = "ProgressivePerGroup"
)

// Rollout strategy to apply workload to the selected clusters by Placement and DecisionStrategy.
type RolloutStrategy struct {
	// Rollout strategy Types are All, Progressive and ProgressivePerGroup
	// 1) All means apply the workload to all clusters in the decision groups at once.
	// 2) Progressive means apply the workload to the selected clusters progressively per cluster. The
	//    workload will not be applied to the next cluster unless one of the current applied clusters
	//    reach the successful state and haven't breached the MaxFailures configuration.
	// 3) ProgressivePerGroup means apply the workload to decisionGroup clusters progressively per
	//    group. The workload will not be applied to the next decisionGroup unless all clusters in the
	//    current group reach the successful state and haven't breached the MaxFailures configuration.

	// +kubebuilder:validation:Enum=All;Progressive;ProgressivePerGroup
	// +kubebuilder:default:=All
	// +optional
	Type RolloutType `json:"type,omitempty"`

	// all defines required fields for RolloutStrategy type All
	// +optional
	All *RolloutAll `json:"all,omitempty"`

	// progressive defines required fields for RolloutStrategy type Progressive
	// +optional
	Progressive *RolloutProgressive `json:"progressive,omitempty"`

	// progressivePerGroup defines required fields for RolloutStrategy type ProgressivePerGroup
	// +optional
	ProgressivePerGroup *RolloutProgressivePerGroup `json:"progressivePerGroup,omitempty"`
}

// Timeout to consider while applying the workload.
type RolloutConfig struct {
	// MinSuccessTime is a "soak" time. In other words, the minimum amount of time the workload
	// applier controller will wait from the start of each rollout before proceeding (assuming a
	// successful state has been reached and MaxFailures wasn't breached).
	// MinSuccessTime is only considered for rollout types Progressive and ProgressivePerGroup.
	// The default value is 0 meaning the workload applier proceeds immediately after a successful
	// state is reached.
	// MinSuccessTime must be defined in [0-9h]|[0-9m]|[0-9s] format examples; 2h , 90m , 360s
	// +kubebuilder:default:="0"
	// +optional
	MinSuccessTime metav1.Duration `json:"minSuccessTime,omitempty"`
	// ProgressDeadline defines how long workload applier controller will wait for the workload to
	// reach a successful state in the cluster.
	// If the workload does not reach a successful state after ProgressDeadline, will stop waiting
	// and workload will be treated as "timeout" and be counted into MaxFailures. Once the MaxFailures
	// is breached, the rollout will stop.
	// ProgressDeadline default value is "None", meaning the workload applier will wait for a
	// successful state indefinitely.
	// ProgressDeadline must be defined in [0-9h]|[0-9m]|[0-9s] format examples; 2h , 90m , 360s
	// +kubebuilder:validation:Pattern="^(([0-9])+[h|m|s])|None$"
	// +kubebuilder:default:="None"
	// +optional
	ProgressDeadline string `json:"progressDeadline,omitempty"`
	// MaxFailures is a percentage or number of clusters in the current rollout that can fail before
	// proceeding to the next rollout. Fail means the cluster has a failed status or timeout status
	// (does not reach successful status after ProgressDeadline).
	// Once the MaxFailures is breached, the rollout will stop.
	// MaxFailures is only considered for rollout types Progressive and ProgressivePerGroup. For
	// Progressive, this is considered over the total number of clusters. For ProgressivePerGroup,
	// this is considered according to the size of the current group. For both Progressive and
	// ProgressivePerGroup, the MaxFailures does not apply for MandatoryDecisionGroups, which tolerate
	// no failures.
	// Default is that no failures are tolerated.
	// +kubebuilder:validation:Pattern="^((100|[0-9]{1,2})%|[0-9]+)$"
	// +kubebuilder:validation:XIntOrString
	// +kubebuilder:default=0
	// +optional
	MaxFailures intstr.IntOrString `json:"maxFailures,omitempty"`
}

// MandatoryDecisionGroup set the decision group name or group index.
// GroupName is considered first to select the decisionGroups then GroupIndex.
type MandatoryDecisionGroup struct {
	// groupName of the decision group should match the placementDecisions label value with label key
	// cluster.open-cluster-management.io/decision-group-name
	// +optional
	GroupName string `json:"groupName,omitempty"`

	// groupIndex of the decision group should match the placementDecisions label value with label key
	// cluster.open-cluster-management.io/decision-group-index
	// +optional
	GroupIndex int32 `json:"groupIndex,omitempty"`
}

// MandatoryDecisionGroups
type MandatoryDecisionGroups struct {
	// List of the decision groups names or indexes to apply the workload first and fail if workload
	// did not reach successful state.
	// GroupName or GroupIndex must match with the decisionGroups defined in the placement's
	// decisionStrategy
	// +optional
	MandatoryDecisionGroups []MandatoryDecisionGroup `json:"mandatoryDecisionGroups,omitempty"`
}

// RolloutAll is a RolloutStrategy Type
type RolloutAll struct {
	// +optional
	RolloutConfig `json:",inline"`
}

// RolloutProgressivePerGroup is a RolloutStrategy Type
type RolloutProgressivePerGroup struct {
	// +optional
	RolloutConfig `json:",inline"`

	// +optional
	MandatoryDecisionGroups `json:",inline"`
}

// RolloutProgressive is a RolloutStrategy Type
type RolloutProgressive struct {
	// +optional
	RolloutConfig `json:",inline"`

	// +optional
	MandatoryDecisionGroups `json:",inline"`

	// maxConcurrency is the max number of clusters to deploy workload concurrently. The default value
	// for MaxConcurrency is determined from the clustersPerDecisionGroup defined in the
	// placement->DecisionStrategy.
	// +kubebuilder:validation:Pattern="^((100|[0-9]{1,2})%|[0-9]+)$"
	// +kubebuilder:validation:XIntOrString
	// +optional
	MaxConcurrency intstr.IntOrString `json:"maxConcurrency,omitempty"`
}
