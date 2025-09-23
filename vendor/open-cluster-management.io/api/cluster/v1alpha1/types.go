// Copyright Contributors to the Open Cluster Management project
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:scope="Cluster"

// ClusterClaim represents cluster information that a managed cluster claims
// ClusterClaims with well known names include,
//  1. id.k8s.io, it contains a unique identifier for the cluster.
//  2. clusterset.k8s.io, it contains an identifier that relates the cluster
//     to the ClusterSet in which it belongs.
//
// ClusterClaims created on a managed cluster will be collected and saved into
// the status of the corresponding ManagedCluster on hub.
type ClusterClaim struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the attributes of the ClusterClaim.
	Spec ClusterClaimSpec `json:"spec,omitempty"`
}

type ClusterClaimSpec struct {
	// value is a claim-dependent string
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
