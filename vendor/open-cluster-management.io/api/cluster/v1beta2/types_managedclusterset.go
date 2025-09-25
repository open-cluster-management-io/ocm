// Copyright Contributors to the Open Cluster Management project
package v1beta2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "open-cluster-management.io/api/cluster/v1"
)

// ExclusiveClusterSetLabel LabelKey
const ClusterSetLabel = "cluster.open-cluster-management.io/clusterset"

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope="Cluster",shortName={"mclset","mclsets"}
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:name="Empty",type="string",JSONPath=".status.conditions[?(@.type==\"ClusterSetEmpty\")].status"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// ManagedClusterSet defines a group of ManagedClusters that you can run
// workloads on. You can define a workload to be deployed on a ManagedClusterSet. See the following options  for the workload:
// - The workload can run on any ManagedCluster in the ManagedClusterSet
// - The workload cannot run on any ManagedCluster outside the ManagedClusterSet
// - The service exposed by the workload can be shared in any ManagedCluster in the ManagedClusterSet
//
// To assign a ManagedCluster to a certain ManagedClusterSet, add a label with the name cluster.open-cluster-management.io/clusterset
// on the ManagedCluster to refer to the ManagedClusterSet. You are not
// allowed to add or remove this label on a ManagedCluster unless you have an
// RBAC rule to CREATE on a virtual subresource of managedclustersets/join.
// To update this label, you must have the permission on both
// the old and new ManagedClusterSet.
type ManagedClusterSet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the attributes of the ManagedClusterSet
	// +kubebuilder:default={clusterSelector: {selectorType: ExclusiveClusterSetLabel}}
	Spec ManagedClusterSetSpec `json:"spec"`

	// Status represents the current status of the ManagedClusterSet
	// +optional
	Status ManagedClusterSetStatus `json:"status,omitempty"`
}

// ManagedClusterSetSpec describes the attributes of the ManagedClusterSet
type ManagedClusterSetSpec struct {
	// clusterSelector represents a selector of ManagedClusters
	// +optional
	// +kubebuilder:default:={selectorType: ExclusiveClusterSetLabel}
	ClusterSelector ManagedClusterSelector `json:"clusterSelector,omitempty"`

	// managedNamespaces defines the list of namespace on the managedclusters
	// across the clusterset to be managed.
	// +optional
	// +listType=map
	// +listMapKey=name
	ManagedNamespaces []v1.ManagedNamespaceConfig `json:"managedNamespaces,omitempty"`
}

// ManagedClusterSelector represents a selector of ManagedClusters
type ManagedClusterSelector struct {
	// selectorType could only be "ExclusiveClusterSetLabel" or "LabelSelector"
	// "ExclusiveClusterSetLabel" means to use label "cluster.open-cluster-management.io/clusterset:<ManagedClusterSet Name>"" to select target clusters.
	// "LabelSelector" means use labelSelector to select target managedClusters
	// +kubebuilder:validation:Enum=ExclusiveClusterSetLabel;LabelSelector
	// +kubebuilder:default:=ExclusiveClusterSetLabel
	// +required
	SelectorType SelectorType `json:"selectorType,omitempty"`

	// labelSelector define the general labelSelector which clusterset will use to select target managedClusters
	// +optional
	LabelSelector *metav1.LabelSelector `json:"labelSelector,omitempty"`
}

type SelectorType string

const (
	// "ExclusiveClusterSetLabel" means to use label "cluster.open-cluster-management.io/clusterset:<ManagedClusterSet Name>"" to select target clusters.
	ExclusiveClusterSetLabel SelectorType = "ExclusiveClusterSetLabel"
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
