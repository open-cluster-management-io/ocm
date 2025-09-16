// Copyright Contributors to the Open Cluster Management project
package v1beta2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope="Namespaced",shortName={"mclsetbinding","mclsetbindings"}
// +kubebuilder:storageversion

// ManagedClusterSetBinding projects a ManagedClusterSet into a certain namespace.
// You can create a ManagedClusterSetBinding in a namespace and bind it to a
// ManagedClusterSet if both have a RBAC rules to CREATE on the virtual subresource of managedclustersets/bind.
// Workloads that you create in the same namespace can only be distributed to ManagedClusters
// in ManagedClusterSets that are bound in this namespace by higher-level controllers.
type ManagedClusterSetBinding struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the attributes of ManagedClusterSetBinding.
	Spec ManagedClusterSetBindingSpec `json:"spec"`

	// Status represents the current status of the ManagedClusterSetBinding
	// +optional
	Status ManagedClusterSetBindingStatus `json:"status,omitempty"`
}

// ManagedClusterSetBindingSpec defines the attributes of ManagedClusterSetBinding.
type ManagedClusterSetBindingSpec struct {
	// clusterSet is the name of the ManagedClusterSet to bind. It must match the
	// instance name of the ManagedClusterSetBinding and cannot change once created.
	// User is allowed to set this field if they have an RBAC rule to CREATE on the
	// virtual subresource of managedclustersets/bind.
	// +kubebuilder:validation:MinLength=1
	ClusterSet string `json:"clusterSet"`
}

const (
	// ClusterSetBindingBoundType is a condition type of clustersetbinding representing
	// whether the ClusterSetBinding is bound to a clusterset.
	ClusterSetBindingBoundType = "Bound"
)

// ManagedClusterSetBindingStatus represents the current status of the ManagedClusterSetBinding.
type ManagedClusterSetBindingStatus struct {
	// Conditions contains the different condition statuses for this ManagedClusterSetBinding.
	Conditions []metav1.Condition `json:"conditions"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ManagedClusterSetBindingList is a collection of ManagedClusterSetBinding.
type ManagedClusterSetBindingList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata.
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#types-kinds
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	// Items is a list of ManagedClusterSetBinding.
	Items []ManagedClusterSetBinding `json:"items"`
}
