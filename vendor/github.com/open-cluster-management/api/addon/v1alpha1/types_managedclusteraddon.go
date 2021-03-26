package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Available",type=string,JSONPath=`.status.conditions[?(@.type=="Available")].status`
// +kubebuilder:printcolumn:name="Degraded",type=string,JSONPath=`.status.conditions[?(@.type=="Degraded")].status`
// +kubebuilder:printcolumn:name="Progressing",type=string,JSONPath=`.status.conditions[?(@.type=="Progressing")].status`

// ManagedClusterAddOn is the Custom Resource object which holds the current state
// of an add-on. This object is used by add-on operators to convey their state.
// This resource should be created in the ManagedCluster namespace.
type ManagedClusterAddOn struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	// spec holds configuration that could apply to any operator.
	// +kubebuilder:validation:Required
	// +required
	Spec ManagedClusterAddOnSpec `json:"spec"`

	// status holds the information about the state of an operator.  It is consistent with status information across
	// the Kubernetes ecosystem.
	// +optional
	Status ManagedClusterAddOnStatus `json:"status"`
}

// ManagedClusterAddOnSpec is empty for now.
type ManagedClusterAddOnSpec struct {
}

// ManagedClusterAddOnStatus provides information about the status of the operator.
// +k8s:deepcopy-gen=true
type ManagedClusterAddOnStatus struct {
	// conditions describe the state of the managed and monitored components for the operator.
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"  patchStrategy:"merge" patchMergeKey:"type"`

	// relatedObjects is a list of objects that are "interesting" or related to this operator. Common uses are:
	// 1. the detailed resource driving the operator
	// 2. operator namespaces
	// 3. operand namespaces
	// 4. related ClusterManagementAddon resource
	// +optional
	RelatedObjects []ObjectReference `json:"relatedObjects,omitempty"`

	// addOnMeta is a reference to the metadata information for the add-on.
	// This should be same as the addOnMeta for the corresponding ClusterManagementAddOn resource.
	// +optional
	AddOnMeta AddOnMeta `json:"addOnMeta"`

	// addOnConfiguration is a reference to configuration information for the add-on.
	// This resource is use to locate the configuration resource for the add-on.
	// +optional
	AddOnConfiguration ConfigCoordinates `json:"addOnConfiguration"`
}

// ObjectReference contains enough information to let you inspect or modify the referred object.
type ObjectReference struct {
	// group of the referent.
	// +kubebuilder:validation:Required
	// +required
	Group string `json:"group"`
	// resource of the referent.
	// +kubebuilder:validation:Required
	// +required
	Resource string `json:"resource"`
	// name of the referent.
	// +kubebuilder:validation:Required
	// +required
	Name string `json:"name"`
}

// ManagedClusterAddOnList is a list of ManagedClusterAddOn resources.
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ManagedClusterAddOnList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []ManagedClusterAddOn `json:"items"`
}
