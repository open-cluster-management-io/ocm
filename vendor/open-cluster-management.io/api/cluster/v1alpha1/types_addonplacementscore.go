// Copyright Contributors to the Open Cluster Management project
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:scope="Namespaced"
// +kubebuilder:subresource:status

// AddOnPlacementScore represents a bundle of scores of one managed cluster, which could be used by placement.
// AddOnPlacementScore is a namespace scoped resource. The namespace of the resource is the cluster namespace.
type AddOnPlacementScore struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Status represents the status of the AddOnPlacementScore.
	// +optional
	Status AddOnPlacementScoreStatus `json:"status,omitempty"`
}

// AddOnPlacementScoreStatus represents the current status of AddOnPlacementScore.
type AddOnPlacementScoreStatus struct {
	// Conditions contain the different condition statuses for this AddOnPlacementScore.
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Scores contain a list of score name and value of this managed cluster.
	// +listType=map
	// +listMapKey=name
	// +optional
	Scores []AddOnPlacementScoreItem `json:"scores,omitempty"`

	// validUntil defines the valid time of the scores.
	// After this time, the scores are considered to be invalid by placement. nil means never expire.
	// The controller owning this resource should keep the scores up-to-date.
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Format=date-time
	// +optional
	ValidUntil *metav1.Time `json:"validUntil"`
}

// AddOnPlacementScoreItem represents the score name and value.
type AddOnPlacementScoreItem struct {
	// name is the name of the score
	// +kubebuilder:validation:Required
	// +required
	Name string `json:"name"`

	// value is the value of the score. The score range is from -100 to 100.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum:=-100
	// +kubebuilder:validation:Maximum:=100
	// +required
	Value int32 `json:"value"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// AddOnPlacementScoreList is a collection of AddOnPlacementScore.
type AddOnPlacementScoreList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata.
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#types-kinds
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	// Items is a list of AddOnPlacementScore
	Items []AddOnPlacementScore `json:"items"`
}
