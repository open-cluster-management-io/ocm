package v1beta1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:scope="Namespaced"
// +kubebuilder:subresource:status

// PlacementDecision indicates a decision from a placement
// PlacementDecision should has a label cluster.open-cluster-management.io/placement={placement name}
// to reference a certain placement.
//
// If a placement has spec.numberOfClusters specified, the total number of decisions contained in
// status.decisions of PlacementDecisions should always be NumberOfClusters; otherwise, the total
// number of decisions should be the number of ManagedClusters which match the placement requirements.
//
// Some of the decisions might be empty when there are no enough ManagedClusters meet the placement
// requirements.
type PlacementDecision struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Status represents the current status of the PlacementDecision
	// +optional
	Status PlacementDecisionStatus `json:"status,omitempty"`
}

// The placementDecsion label name holding the placement name
const (
	PlacementLabel string = "cluster.open-cluster-management.io/placement"
)

// PlacementDecisionStatus represents the current status of the PlacementDecision.
type PlacementDecisionStatus struct {
	// Decisions is a slice of decisions according to a placement
	// The number of decisions should not be larger than 100
	// +kubebuilder:validation:Required
	// +required
	Decisions []ClusterDecision `json:"decisions"`
}

// ClusterDecision represents a decision from a placement
// An empty ClusterDecision indicates it is not scheduled yet.
type ClusterDecision struct {
	// ClusterName is the name of the ManagedCluster. If it is not empty, its value should be unique cross all
	// placement decisions for the Placement.
	// +kubebuilder:validation:Required
	// +required
	ClusterName string `json:"clusterName"`

	// Reason represents the reason why the ManagedCluster is selected.
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
