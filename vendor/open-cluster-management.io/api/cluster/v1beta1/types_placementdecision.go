// Copyright Contributors to the Open Cluster Management project
package v1beta1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:scope="Namespaced"
// +kubebuilder:subresource:status

// PlacementDecision indicates a decision from a placement.
// PlacementDecision must have a cluster.open-cluster-management.io/placement={placement name} label to reference a certain placement.
//
// If a placement has spec.numberOfClusters specified, the total number of decisions contained in
// the status.decisions of PlacementDecisions must be the same as NumberOfClusters. Otherwise, the
// total number of decisions must equal the number of ManagedClusters that
// match the placement requirements.
//
// Some of the decisions might be empty when there are not enough ManagedClusters to meet the placement requirements.
type PlacementDecision struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Status represents the current status of the PlacementDecision
	// +optional
	Status PlacementDecisionStatus `json:"status,omitempty"`
}

// The placementDecsion labels
const (
	// Placement owner name.
	PlacementLabel string = "cluster.open-cluster-management.io/placement"
	// decision group index.
	DecisionGroupIndexLabel string = "cluster.open-cluster-management.io/decision-group-index"
	// decision group name.
	DecisionGroupNameLabel string = "cluster.open-cluster-management.io/decision-group-name"
)

// PlacementDecisionStatus represents the current status of the PlacementDecision.
type PlacementDecisionStatus struct {
	// decisions is a slice of decisions according to a placement
	// The number of decisions should not be larger than 100
	// +kubebuilder:validation:Required
	// +required
	Decisions []ClusterDecision `json:"decisions"`
}

// ClusterDecision represents a decision from a placement
// An empty ClusterDecision indicates it is not scheduled yet.
type ClusterDecision struct {
	// clusterName is the name of the ManagedCluster. If it is not empty, its value should be unique across all
	// placement decisions for the Placement.
	// +kubebuilder:validation:Required
	// +required
	ClusterName string `json:"clusterName"`

	// reason represents the reason why the ManagedCluster is selected.
	// +kubebuilder:validation:Required
	// +required
	Reason string `json:"reason"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterDecisionList is a collection of PlacementDecision.
type PlacementDecisionList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata.
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#types-kinds
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	// Items is a list of PlacementDecision.
	Items []PlacementDecision `json:"items"`
}
