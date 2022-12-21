package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// LegacyClusterSetLabel LabelKey
const ClusterSetLabel = "cluster.open-cluster-management.io/clusterset"

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope="Cluster",shortName={"mclset","mclsets"}
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:name="Empty",type="string",JSONPath=".status.conditions[?(@.type==\"ClusterSetEmpty\")].status"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// ManagedClusterSet defines a group of ManagedClusters that user's workload can run on.
// A workload can be defined to deployed on a ManagedClusterSet, which mean:
//  1. The workload can run on any ManagedCluster in the ManagedClusterSet
//  2. The workload cannot run on any ManagedCluster outside the ManagedClusterSet
//  3. The service exposed by the workload can be shared in any ManagedCluster in the ManagedClusterSet
//
// In order to assign a ManagedCluster to a certian ManagedClusterSet, add a label with name
// `cluster.open-cluster-management.io/clusterset` on the ManagedCluster to refers to the ManagedClusterSet.
// User is not allow to add/remove this label on a ManagedCluster unless they have a RBAC rule to CREATE on
// a virtual subresource of managedclustersets/join. In order to update this label, user must have the permission
// on both the old and new ManagedClusterSet.
type ManagedClusterSet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the attributes of the ManagedClusterSet
	// +kubebuilder:default={clusterSelector: {selectorType: LegacyClusterSetLabel}}
	Spec ManagedClusterSetSpec `json:"spec"`

	// Status represents the current status of the ManagedClusterSet
	// +optional
	Status ManagedClusterSetStatus `json:"status,omitempty"`
}

// ManagedClusterSetSpec describes the attributes of the ManagedClusterSet
type ManagedClusterSetSpec struct {
	// ClusterSelector represents a selector of ManagedClusters
	// +optional
	// +kubebuilder:default:={selectorType: LegacyClusterSetLabel}
	ClusterSelector ManagedClusterSelector `json:"clusterSelector,omitempty"`
}

// ManagedClusterSelector represents a selector of ManagedClusters
type ManagedClusterSelector struct {
	// SelectorType could only be "LegacyClusterSetLabel" or "LabelSelector"
	// "LegacyClusterSetLabel" means to use label "cluster.open-cluster-management.io/clusterset:<ManagedClusterSet Name>"" to select target clusters.
	// "LabelSelector" means use labelSelector to select target managedClusters
	// +kubebuilder:validation:Enum=LegacyClusterSetLabel;LabelSelector
	// +kubebuilder:default:=LegacyClusterSetLabel
	// +required
	SelectorType SelectorType `json:"selectorType,omitempty"`

	// LabelSelector define the general labelSelector which clusterset will use to select target managedClusters
	// +optional
	LabelSelector *metav1.LabelSelector `json:"labelSelector,omitempty"`
}

type SelectorType string

const (
	// "LegacyClusterSetLabel" means to use label "cluster.open-cluster-management.io/clusterset:<ManagedClusterSet Name>"" to select target clusters.
	LegacyClusterSetLabel SelectorType = "LegacyClusterSetLabel"
	// "LabelSelector" means use labelSelector to select target managedClusters
	LabelSelector SelectorType = "LabelSelector"
)

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
