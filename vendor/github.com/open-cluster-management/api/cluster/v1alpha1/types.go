package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope="Cluster"

// ManagedClusterSet defines a group of ManagedClusters that user's workload can run on.
// A workload can be defined to deployed on a ManagedClusterSet, which mean:
//
// 1. The workload can run on any ManagedCluster in the ManagedClusterSet
// 2. The workload cannot run on any ManagedCluster outside the ManagedClusterSet
// 3. The service exposed by the workload can be shared in any ManagedCluster in the ManagedClusterSet
type ManagedClusterSet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the attributes of the desired ManagedClusters
	Spec ManagedClusterSetSpec `json:"spec"`

	// Status represents the current status of the ManagedClusterSet
	// +optional
	Status ManagedClusterSetStatus `json:"status,omitempty"`
}

// ManagedClusterSetSpec describes the attributes of the desired ManagedClusters
type ManagedClusterSetSpec struct {
	// ClusterSelectors represents a slice of selectors to select ManagedClusters
	// If empty, the ManagedClusterSet will include no ManagedCluster.
	// If more than one ClusterSelector are specified in the slice, OR operation
	// will be used between them.
	// +optional
	ClusterSelectors []ClusterSelector `json:"clusterSelectors,omitempty"`
}

// ClusterSelector represents a selector of ManagedClusters
// ClusterNames and LabelSelector are mutually exclusive. They cannot be set at the
// same time. If none of them is set, no ManagedCluster will be selected
type ClusterSelector struct {
	// ClusterNames represents a list of cluster name
	// +optional
	ClusterNames []string `json:"clusterNames,omitempty"`

	// LabelSelector represents a label selector to select cluster by label
	// +optional
	LabelSelector *metav1.LabelSelector `json:"labelSelector,omitempty"`
}

// ManagedClusterSetStatus represents the current status of the ManagedClusterSet.
type ManagedClusterSetStatus struct {
	// Conditions contains the different condition statuses for this ManagedClusterSet.
	Conditions []metav1.Condition `json:"conditions"`
}

const (
	// ManagedClusterSetConditionEmpty means no ManagedCluster is included in the
	// ManagedClusterSet.
	ManagedClusterSetConditionEmpty string = "ClusterSetEmpty"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ManagedClusterSetList is a collection of ManagedClusterSet.
type ManagedClusterSetList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata.
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#types-kinds
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	// Items is a list of ManagedClusterSet.
	Items []ManagedClusterSet `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:scope="Namespaced"

// ManagedClusterSetBinding projects a ManagedClusterSet into a certain namespace.
// User is able to create a ManagedClusterSetBinding in a namespace and bind it to a
// ManagedClusterSet if they have an RBAC rule to CREATE on the virtual subresource of
// managedclustersets/bind. Workloads created in the same namespace can only be
// distributed to ManagedClusters in ManagedClusterSets bound in this namespace by
// higher level controllers.
type ManagedClusterSetBinding struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the attributes of ManagedClusterSetBinding.
	Spec ManagedClusterSetBindingSpec `json:"spec"`
}

// ManagedClusterSetBindingSpec defines the attributes of ManagedClusterSetBinding.
type ManagedClusterSetBindingSpec struct {
	// ClusterSet is the name of the ManagedClusterSet to bind. It must match the
	// instance name of the ManagedClusterSetBinding and cannot change once created.
	// User is allowed to set this field if they have an RBAC rule to CREATE on the
	// virtual subresource of managedclustersets/bind.
	// +kubebuilder:validation:MinLength=1
	ClusterSet string `json:"clusterSet"`
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
