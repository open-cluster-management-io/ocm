package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope="Cluster",shortName={"mclset","mclsets"}

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
// +kubebuilder:resource:scope="Namespaced",shortName={"mclsetbinding","mclsetbindings"}

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
// ClusterClaims created on a managed cluster will be collected and saved into
// the status of the corresponding ManagedCluster on hub.
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

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:scope="Namespaced"
// +kubebuilder:subresource:status

// Placement defines a rule to select a set of ManagedClusters from the ManagedClusterSets bound
// to the placement namespace.
//
// Here is how the placement policy combines with other selection methods to determine a matching
// list of ManagedClusters:
// 1) Kubernetes clusters are registered with hub as cluster-scoped ManagedClusters;
// 2) ManagedClusters are organized into cluster-scoped ManagedClusterSets;
// 3) ManagedClusterSets are bound to workload namespaces;
// 4) Namespace-scoped Placements specify a slice of ManagedClusterSets which select a working set
//    of potential ManagedClusters;
// 5) Then Placements subselect from that working set using label/claim selection.
//
// No ManagedCluster will be selected if no ManagedClusterSet is bound to the placement
// namespace. User is able to bind a ManagedClusterSet to a namespace by creating a
// ManagedClusterSetBinding in that namespace if they have a RBAC rule to CREATE on the virtual
// subresource of `managedclustersets/bind`.
//
// A slice of PlacementDecisions with label cluster.open-cluster-management.io/placement={placement name}
// will be created to represent the ManagedClusters selected by this placement.
//
// If a ManagedCluster is selected and added into the PlacementDecisions, other components may
// apply workload on it; once it is removed from the PlacementDecisions, the workload applied on
// this ManagedCluster should be evicted accordingly.
type Placement struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the attributes of Placement.
	// +kubebuilder:validation:Required
	// +required
	Spec PlacementSpec `json:"spec"`

	// Status represents the current status of the Placement
	// +optional
	Status PlacementStatus `json:"status,omitempty"`
}

// PlacementSpec defines the attributes of Placement.
// An empty PlacementSpec selects all ManagedClusters from the ManagedClusterSets bound to
// the placement namespace. The containing fields are ANDed.
type PlacementSpec struct {
	// ClusterSets represent the ManagedClusterSets from which the ManagedClusters are selected.
	// If the slice is empty, ManagedClusters will be selected from the ManagedClusterSets bound to the placement
	// namespace, otherwise ManagedClusters will be selected from the intersection of this slice and the
	// ManagedClusterSets bound to the placement namespace.
	// +optional
	ClusterSets []string `json:"clusterSets,omitempty"`

	// NumberOfClusters represents the desired number of ManagedClusters to be selected which meet the
	// placement requirements.
	// 1) If not specified, all ManagedClusters which meet the placement requirements (including ClusterSets,
	//    and Predicates) will be selected;
	// 2) Otherwise if the nubmer of ManagedClusters meet the placement requirements is larger than
	//    NumberOfClusters, a random subset with desired number of ManagedClusters will be selected;
	// 3) If the nubmer of ManagedClusters meet the placement requirements is equal to NumberOfClusters,
	//    all of them will be selected;
	// 4) If the nubmer of ManagedClusters meet the placement requirements is less than NumberOfClusters,
	//    all of them will be selected, and the status of condition `PlacementConditionSatisfied` will be
	//    set to false;
	// +optional
	NumberOfClusters *int32 `json:"numberOfClusters,omitempty"`

	// Predicates represent a slice of predicates to select ManagedClusters. The predicates are ORed.
	// +optional
	Predicates []ClusterPredicate `json:"predicates,omitempty"`
}

// ClusterPredicate represents a predicate to select ManagedClusters.
type ClusterPredicate struct {
	// RequiredClusterSelector represents a selector of ManagedClusters by label and claim. If specified,
	// 1) Any ManagedCluster, which does not match the selector, should not be selected by this ClusterPredicate;
	// 2) If a selected ManagedCluster (of this ClusterPredicate) ceases to match the selector (e.g. due to
	//    an update) of any ClusterPredicate, it will be eventually removed from the placement decisions;
	// 3) If a ManagedCluster (not selected previously) starts to match the selector, it will either
	//    be selected or at least has a chance to be selected (when NumberOfClusters is specified);
	// +optional
	RequiredClusterSelector ClusterSelector `json:"requiredClusterSelector,omitempty"`
}

// ClusterSelector represents the AND of the containing selectors. An empty cluster selector matches all objects.
// A null cluster selector matches no objects.
type ClusterSelector struct {
	// LabelSelector represents a selector of ManagedClusters by label
	// +optional
	LabelSelector metav1.LabelSelector `json:"labelSelector,omitempty"`

	// ClaimSelector represents a selector of ManagedClusters by clusterClaims in status
	// +optional
	ClaimSelector ClusterClaimSelector `json:"claimSelector,omitempty"`
}

// ClusterClaimSelector is a claim query over a set of ManagedClusters. An empty cluster claim
// selector matches all objects. A null cluster claim selector matches no objects.
type ClusterClaimSelector struct {
	// matchExpressions is a list of cluster claim selector requirements. The requirements are ANDed.
	// +optional
	MatchExpressions []metav1.LabelSelectorRequirement `json:"matchExpressions,omitempty"`
}

type PlacementStatus struct {
	// NumberOfSelectedClusters represents the number of selected ManagedClusters
	// +optional
	NumberOfSelectedClusters int32 `json:"numberOfSelectedClusters"`

	// Conditions contains the different condition statuses for this Placement.
	// +optional
	Conditions []metav1.Condition `json:"conditions"`
}

const (
	// PlacementConditionSatisfied means Placement requirements are satisfied.
	// A placement is not satisfied only if there is empty ClusterDecision in the status.decisions
	// of PlacementDecisions.
	PlacementConditionSatisfied string = "PlacementSatisfied"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// PlacementList is a collection of Placements.
type PlacementList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata.
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#types-kinds
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	// Items is a list of Placements.
	Items []Placement `json:"items"`
}

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
