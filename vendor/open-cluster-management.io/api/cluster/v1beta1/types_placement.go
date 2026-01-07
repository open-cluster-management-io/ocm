// Copyright Contributors to the Open Cluster Management project
package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	v1 "open-cluster-management.io/api/cluster/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:scope="Namespaced"
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Succeeded",type="string",JSONPath=".status.conditions[?(@.type==\"PlacementSatisfied\")].status"
// +kubebuilder:printcolumn:name="Reason",type="string",JSONPath=".status.conditions[?(@.type==\"PlacementSatisfied\")].reason"
// +kubebuilder:printcolumn:name="SelectedClusters",type="integer",JSONPath=".status.numberOfSelectedClusters"

// Placement defines a rule to select a set of ManagedClusters from the ManagedClusterSets bound
// to the placement namespace.
//
// Here is how the placement policy combines with other selection methods to determine a matching
// list of ManagedClusters:
//  1. Kubernetes clusters are registered with hub as cluster-scoped ManagedClusters;
//  2. ManagedClusters are organized into cluster-scoped ManagedClusterSets;
//  3. ManagedClusterSets are bound to workload namespaces;
//  4. Namespace-scoped Placements specify a slice of ManagedClusterSets which select a working set
//     of potential ManagedClusters;
//  5. Then Placements subselect from that working set using label/claim selection.
//
// A ManagedCluster will not be selected if no ManagedClusterSet is bound to the placement
// namespace. A user is able to bind a ManagedClusterSet to a namespace by creating a
// ManagedClusterSetBinding in that namespace if they have an RBAC rule to CREATE on the virtual
// subresource of `managedclustersets/bind`.
//
// A slice of PlacementDecisions with the label cluster.open-cluster-management.io/placement={placement name}
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
	// clusterSets represent the ManagedClusterSets from which the ManagedClusters are selected.
	// If the slice is empty, ManagedClusters will be selected from the ManagedClusterSets bound to the placement
	// namespace, otherwise ManagedClusters will be selected from the intersection of this slice and the
	// ManagedClusterSets bound to the placement namespace.
	// +optional
	ClusterSets []string `json:"clusterSets,omitempty"`

	// numberOfClusters represents the desired number of ManagedClusters to be selected which meet the
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

	// predicates represent a slice of predicates to select ManagedClusters. The predicates are ORed.
	// +optional
	Predicates []ClusterPredicate `json:"predicates,omitempty"`

	// prioritizerPolicy defines the policy of the prioritizers.
	// If this field is unset, then default prioritizer mode and configurations are used.
	// Referring to PrioritizerPolicy to see more description about Mode and Configurations.
	// +optional
	PrioritizerPolicy PrioritizerPolicy `json:"prioritizerPolicy"`

	// spreadPolicy defines how placement decisions should be distributed among a
	// set of ManagedClusters.
	// +optional
	SpreadPolicy SpreadPolicy `json:"spreadPolicy,omitempty"`

	// tolerations are applied to placements, and allow (but do not require) the managed clusters with
	// certain taints to be selected by placements with matching tolerations.
	// +optional
	Tolerations []Toleration `json:"tolerations,omitempty"`

	// decisionStrategy divides the created placement decisions into groups and defines the number of clusters per decision group.
	// +optional
	DecisionStrategy DecisionStrategy `json:"decisionStrategy,omitempty"`
}

// DecisionGroup define a subset of clusters that will be added to placementDecisions with groupName label.
type DecisionGroup struct {
	// groupName to set as the label value on created PlacementDecision
	// resources using the label key cluster.open-cluster-management.io/decision-group-name.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern="^[a-zA-Z0-9][-A-Za-z0-9_.]{0,61}[a-zA-Z0-9]$"
	// +required
	GroupName string `json:"groupName,omitempty"`

	// groupClusterSelector selects a subset of clusters by labels.
	// +kubebuilder:validation:Required
	// +required
	ClusterSelector GroupClusterSelector `json:"groupClusterSelector,omitempty"`
}

// GroupClusterSelector represents the AND of the containing selectors for groupClusterSelector. An empty group cluster selector matches all objects.
// A null group cluster selector matches no objects.
type GroupClusterSelector struct {
	// labelSelector represents a selector of ManagedClusters by label
	// +optional
	LabelSelector metav1.LabelSelector `json:"labelSelector,omitempty"`

	// claimSelector represents a selector of ManagedClusters by clusterClaims in status
	// +optional
	ClaimSelector ClusterClaimSelector `json:"claimSelector,omitempty"`
}

// Group the created placementDecision into decision groups based on the number of clusters per decision group.
type GroupStrategy struct {
	// decisionGroups represents a list of predefined groups to put decision results.
	// Decision groups will be constructed based on the DecisionGroups field at first. The clusters not included in the
	// DecisionGroups will be divided to other decision groups afterwards. Each decision group should not have the number
	// of clusters larger than the ClustersPerDecisionGroup.
	// +optional
	DecisionGroups []DecisionGroup `json:"decisionGroups,omitempty"`

	// clustersPerDecisionGroup is a specific number or percentage of the total selected clusters.
	// The specific number will divide the placementDecisions to decisionGroups each group has max number of clusters
	// equal to that specific number.
	// The percentage will divide the placementDecisions to decisionGroups each group has max number of clusters based
	// on the total num of selected clusters and percentage.
	// ex; for a total 100 clusters selected, ClustersPerDecisionGroup equal to 20% will divide the placement decision
	// to 5 groups each group should have 20 clusters.
	// Default is having all clusters in a single group.
	//
	// The predefined decisionGroups is expected to be a subset of the selected clusters and the number of items in each
	// group SHOULD be less than ClustersPerDecisionGroup. Once the number of items exceeds the ClustersPerDecisionGroup,
	// the decisionGroups will also be be divided into multiple decisionGroups with same GroupName but different GroupIndex.
	//
	// +kubebuilder:validation:XIntOrString
	// +kubebuilder:validation:Pattern=`^((100|[1-9][0-9]{0,1})%|[1-9][0-9]*)$`
	// +kubebuilder:default:="100%"
	// +optional
	ClustersPerDecisionGroup intstr.IntOrString `json:"clustersPerDecisionGroup,omitempty"`
}

// DecisionStrategy divide the created placement decision to groups and define number of clusters per decision group.
type DecisionStrategy struct {
	// groupStrategy defines strategies to divide selected clusters into decision groups.
	// +optional
	GroupStrategy GroupStrategy `json:"groupStrategy,omitempty"`
}

// ClusterPredicate represents a predicate to select ManagedClusters.
type ClusterPredicate struct {
	// requiredClusterSelector represents a selector of ManagedClusters by label and claim. If specified,
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
	// labelSelector represents a selector of ManagedClusters by label
	// +optional
	LabelSelector metav1.LabelSelector `json:"labelSelector,omitempty"`

	// claimSelector represents a selector of ManagedClusters by clusterClaims in status
	// +optional
	ClaimSelector ClusterClaimSelector `json:"claimSelector,omitempty"`

	// celSelector represents a selector of ManagedClusters by CEL expressions on ManagedCluster fields
	// +optional
	CelSelector ClusterCelSelector `json:"celSelector,omitempty"`
}

// ClusterCelSelector is a list of CEL expressions. The expressions are ANDed.
type ClusterCelSelector struct {
	// +optional
	CelExpressions []string `json:"celExpressions,omitempty"`
}

// ClusterClaimSelector is a claim query over a set of ManagedClusters. An empty cluster claim
// selector matches all objects. A null cluster claim selector matches no objects.
type ClusterClaimSelector struct {
	// matchExpressions is a list of cluster claim selector requirements. The requirements are ANDed.
	// +optional
	MatchExpressions []metav1.LabelSelectorRequirement `json:"matchExpressions,omitempty"`
}

// PrioritizerPolicy represents the policy of prioritizer
type PrioritizerPolicy struct {
	// Mode is either Exact, Additive, "" where "" is Additive by default.
	// In Additive mode, any prioritizer not explicitly enumerated is enabled in its default Configurations,
	// in which Steady and Balance prioritizers have the weight of 1 while other prioritizers have the weight of 0.
	// Additive doesn't require configuring all prioritizers. The default Configurations may change in the future,
	// and additional prioritization will happen.
	// In Exact mode, any prioritizer not explicitly enumerated is weighted as zero.
	// Exact requires knowing the full set of prioritizers you want, but avoids behavior changes between releases.
	// +kubebuilder:default:=Additive
	// +optional
	Mode PrioritizerPolicyModeType `json:"mode,omitempty"`

	// +optional
	Configurations []PrioritizerConfig `json:"configurations,omitempty"`
}

// PrioritizerPolicyModeType represents the type of PrioritizerPolicy.Mode
type PrioritizerPolicyModeType string

const (
	// Valid PrioritizerPolicyModeType value is Exact, Additive.
	PrioritizerPolicyModeAdditive PrioritizerPolicyModeType = "Additive"
	PrioritizerPolicyModeExact    PrioritizerPolicyModeType = "Exact"
)

// PrioritizerConfig represents the configuration of prioritizer
type PrioritizerConfig struct {
	// scoreCoordinate represents the configuration of the prioritizer and score source.
	// +kubebuilder:validation:Required
	// +required
	ScoreCoordinate *ScoreCoordinate `json:"scoreCoordinate,omitempty"`

	// weight defines the weight of the prioritizer score. The value must be ranged in [-10,10].
	// Each prioritizer will calculate an integer score of a cluster in the range of [-100, 100].
	// The final score of a cluster will be sum(weight * prioritizer_score).
	// A higher weight indicates that the prioritizer weights more in the cluster selection,
	// while 0 weight indicates that the prioritizer is disabled. A negative weight indicates
	// wants to select the last ones.
	// +kubebuilder:validation:Minimum:=-10
	// +kubebuilder:validation:Maximum:=10
	// +kubebuilder:default:=1
	// +optional
	Weight int32 `json:"weight,omitempty"`
}

// ScoreCoordinate represents the configuration of the score type and score source
type ScoreCoordinate struct {
	// type defines the type of the prioritizer score.
	// Type is either "BuiltIn", "AddOn" or "", where "" is "BuiltIn" by default.
	// When the type is "BuiltIn", need to specify a BuiltIn prioritizer name in BuiltIn.
	// When the type is "AddOn", need to configure the score source in AddOn.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=BuiltIn;AddOn
	// +kubebuilder:default:=BuiltIn
	// +required
	Type string `json:"type,omitempty"`

	// BuiltIn defines the name of a BuiltIn prioritizer. Below are the valid BuiltIn prioritizer names.
	// 1) Balance: balance the decisions among the clusters.
	// 2) Steady: ensure the existing decision is stabilized.
	// 3) ResourceAllocatableCPU & ResourceAllocatableMemory: sort clusters based on the allocatable.
	// 4) Spread: spread the workload evenly to topologies.
	// +optional
	BuiltIn string `json:"builtIn,omitempty"`

	// When type is "AddOn", AddOn defines the resource name and score name.
	// +optional
	AddOn *AddOnScore `json:"addOn,omitempty"`
}

const (
	// Valid ScoreCoordinate type is BuiltIn, AddOn.
	ScoreCoordinateTypeBuiltIn string = "BuiltIn"
	ScoreCoordinateTypeAddOn   string = "AddOn"
)

// AddOnScore represents the configuration of the addon score source.
type AddOnScore struct {
	// resourceName defines the resource name of the AddOnPlacementScore.
	// The placement prioritizer selects AddOnPlacementScore CR by this name.
	// +kubebuilder:validation:Required
	// +required
	ResourceName string `json:"resourceName"`

	// scoreName defines the score name inside AddOnPlacementScore.
	// AddOnPlacementScore contains a list of score names and values; scoreName specifies the score to be used by
	// the prioritizer.
	// +kubebuilder:validation:Required
	// +required
	ScoreName string `json:"scoreName"`
}

// SpreadPolicy defines how the placement decision should be spread among the ManagedClusters.
type SpreadPolicy struct {
	// SpreadConstraints defines how the placement decision should be distributed among a set of ManagedClusters.
	// The importance of the SpreadConstraintsTerms follows the natural order of their index in the slice.
	// The scheduler first consider SpreadConstraintsTerms with smaller index then those with larger index
	// to distribute the placement decision.
	// +optional
	// +kubebuilder:validation:MaxItems=8
	SpreadConstraints []SpreadConstraintsTerm `json:"spreadConstraints,omitempty"`
}

// SpreadConstraintsTerm defines a terminology to spread placement decisions.
type SpreadConstraintsTerm struct {
	// TopologyKey is either a label key or a cluster claim name of ManagedClusters.
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^([a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*/)?([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9]$`
	// +kubebuilder:validation:MaxLength=316
	TopologyKey string `json:"topologyKey"`

	// TopologyKeyType indicates the type of TopologyKey. It could be Label or Claim.
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=Label;Claim
	TopologyKeyType TopologyKeyType `json:"topologyKeyType"`

	// MaxSkew represents the degree to which the workload may be unevenly distributed.
	// Skew is the maximum difference between the number of selected ManagedClusters in a topology and the global minimum.
	// The global minimum is the minimum number of selected ManagedClusters for the topologies within the same TopologyKey.
	// The minimum possible value of MaxSkew is 1, and the default value is 1.
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=1
	MaxSkew int32 `json:"maxSkew"`

	// WhenUnsatisfiable represents the action of the scheduler when MaxSkew cannot be satisfied.
	// It could be DoNotSchedule or ScheduleAnyway. The default value is ScheduleAnyway.
	// DoNotSchedule instructs the scheduler not to schedule more ManagedClusters when MaxSkew is not satisfied.
	// ScheduleAnyway instructs the scheduler to keep scheduling even if MaxSkew is not satisfied.
	// +optional
	// +kubebuilder:validation:Enum=DoNotSchedule;ScheduleAnyway
	// +kubebuilder:default=ScheduleAnyway
	WhenUnsatisfiable UnsatisfiableMaxSkewAction `json:"whenUnsatisfiable"`
}

// TopologyKeyType represents the type of TopologyKey.
type TopologyKeyType string

const (
	// Valid TopologyKeyType value is Claim, Label.
	TopologyKeyTypeClaim TopologyKeyType = "Claim"
	TopologyKeyTypeLabel TopologyKeyType = "Label"
)

// UnsatisfiableMaxSkewAction represents the action when MaxSkew cannot be satisfied.
type UnsatisfiableMaxSkewAction string

const (
	// Valid UnsatisfiableMaxSkewAction value is DoNotSchedule, ScheduleAnyway.
	DoNotSchedule  UnsatisfiableMaxSkewAction = "DoNotSchedule"
	ScheduleAnyway UnsatisfiableMaxSkewAction = "ScheduleAnyway"
)

// Toleration represents the toleration object that can be attached to a placement.
// The placement this Toleration is attached to tolerates any taint that matches
// the triple <key,value,effect> using the matching operator <operator>.
type Toleration struct {
	// Key is the taint key that the toleration applies to. Empty means match all taint keys.
	// If the key is empty, operator must be Exists; this combination means to match all values and all keys.
	// +kubebuilder:validation:Pattern=`^([a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*/)?(([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])$`
	// +kubebuilder:validation:MaxLength=316
	// +optional
	Key string `json:"key,omitempty"`
	// Operator represents a key's relationship to the value.
	// Valid operators are Exists and Equal. Defaults to Equal.
	// Exists is equivalent to wildcard for value, so that a placement can
	// tolerate all taints of a particular category.
	// +kubebuilder:default:="Equal"
	// +optional
	Operator TolerationOperator `json:"operator,omitempty"`
	// Value is the taint value the toleration matches to.
	// If the operator is Exists, the value should be empty, otherwise just a regular string.
	// +kubebuilder:validation:MaxLength=1024
	// +optional
	Value string `json:"value,omitempty"`
	// Effect indicates the taint effect to match. Empty means match all taint effects.
	// When specified, allowed values are NoSelect, PreferNoSelect and NoSelectIfNew.
	// +kubebuilder:validation:Enum:=NoSelect;PreferNoSelect;NoSelectIfNew
	// +optional
	Effect v1.TaintEffect `json:"effect,omitempty"`
	// TolerationSeconds represents the period of time the toleration (which must be of effect
	// NoSelect/PreferNoSelect, otherwise this field is ignored) tolerates the taint.
	// The default value is nil, which indicates it tolerates the taint forever.
	// The start time of counting the TolerationSeconds should be the TimeAdded in Taint, not the cluster
	// scheduled time or TolerationSeconds added time.
	// +optional
	TolerationSeconds *int64 `json:"tolerationSeconds,omitempty"`
}

// TolerationOperator is the set of operators that can be used in a toleration.
type TolerationOperator string

// These are valid values for TolerationOperator
const (
	TolerationOpExists TolerationOperator = "Exists"
	TolerationOpEqual  TolerationOperator = "Equal"
)

// Present decision groups status based on the DecisionStrategy definition.
type DecisionGroupStatus struct {
	// Present the decision group index. If there is no decision strategy defined all placement decisions will be in group index 0
	// +optional
	DecisionGroupIndex int32 `json:"decisionGroupIndex"`

	// Decision group name that is defined in the DecisionStrategy's DecisionGroup.
	// +optional
	DecisionGroupName string `json:"decisionGroupName"`

	// List of placement decisions names associated with the decision group
	// +optional
	Decisions []string `json:"decisions"`

	// Total number of clusters in the decision group. Clusters count is equal or less than the clusterPerDecisionGroups defined in the decision strategy.
	// +kubebuilder:default:=0
	// +optional
	ClustersCount int32 `json:"clusterCount"`
}

type PlacementStatus struct {
	// numberOfSelectedClusters represents the number of selected ManagedClusters
	// +optional
	NumberOfSelectedClusters int32 `json:"numberOfSelectedClusters"`

	// List of decision groups determined by the placement and DecisionStrategy.
	// +optional
	DecisionGroups []DecisionGroupStatus `json:"decisionGroups"`

	// Conditions contains the different condition status for this Placement.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions"`
}

const (
	// PlacementConditionSatisfied means Placement requirements are satisfied.
	// A placement is not satisfied only if there is empty ClusterDecision in the status.decisions
	// of PlacementDecisions.
	PlacementConditionSatisfied string = "PlacementSatisfied"
	// PlacementConditionMisconfigured means Placement configuration is incorrect.
	PlacementConditionMisconfigured string = "PlacementMisconfigured"
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

const (
	// PlacementDisableAnnotation is used to disable scheduling for a placement.
	// It is a experimental flag to let placement controller ignore this placement,
	// so other placement consumers can chime in.
	PlacementDisableAnnotation = "cluster.open-cluster-management.io/experimental-scheduling-disable"
)
