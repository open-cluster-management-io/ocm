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
//   1. The workload can run on any ManagedCluster in the ManagedClusterSet
//   2. The workload cannot run on any ManagedCluster outside the ManagedClusterSet
//   3. The service exposed by the workload can be shared in any ManagedCluster in the ManagedClusterSet
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
	Spec ManagedClusterSetSpec `json:"spec"`

	// Status represents the current status of the ManagedClusterSet
	// +optional
	Status ManagedClusterSetStatus `json:"status,omitempty"`
}

// ManagedClusterSetSpec describes the attributes of the ManagedClusterSet
type ManagedClusterSetSpec struct {
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

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:scope="Cluster"

// ClusterClaim represents cluster information that a managed cluster claims
// ClusterClaims with well known names include,
//   1. id.k8s.io, it contains a unique identifier for the cluster.
//   2. clusterset.k8s.io, it contains an identifier that relates the cluster
//      to the ClusterSet in which it belongs.
type ClusterClaim struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the attributes of the ClusterClaim.
	Spec ClusterClaimSpec `json:"spec,omitempty"`
}

type ClusterClaimSpec struct {
	// Value is a claim-dependent string
	// +kubebuilder:validation:MaxLength=1024
	// +kubebuilder:validation:MinLength=1
	Value string `json:"value,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterClaimList is a collection of ClusterClaim.
type ClusterClaimList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata.
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#types-kinds
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	// Items is a list of ClusterClaim.
	Items []ClusterClaim `json:"items"`
}

// ReservedClusterClaimNames includes a list of reserved names for ClusterNames.
// When exposing ClusterClaims created on managed cluster, the registration agent gives high
// priority to the reserved ClusterClaims.
var ReservedClusterClaimNames = [...]string{
	// unique identifier for the cluster
	"id.k8s.io",
	// kubernetes version
	"kubeversion.open-cluster-management.io",
	// platform the managed cluster is running on, like AWS, GCE, and Equinix Metal
	"platform.open-cluster-management.io",
	// product name, like OpenShift, Anthos, EKS and GKE
	"product.open-cluster-management.io",
}
