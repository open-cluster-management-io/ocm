package v1

import (
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope="Cluster",shortName={"mcl","mcls"}
// +kubebuilder:printcolumn:JSONPath=`.spec.hubAcceptsClient`,name="Hub Accepted",type=boolean
// +kubebuilder:printcolumn:JSONPath=`.spec.managedClusterClientConfigs[*].url`,name="Managed Cluster URLs",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=="ManagedClusterJoined")].status`,name="Joined",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=="ManagedClusterConditionAvailable")].status`,name="Available",type=string
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date

// ManagedCluster represents the desired state and current status of managed
// cluster. ManagedCluster is a cluster scoped resource. The name is the cluster
// UID.
//
// The cluster join process follows a double opt-in process:
//
// 1. Agent on managed cluster creates CSR on hub with cluster UID and agent name.
// 2. Agent on managed cluster creates ManagedCluster on hub.
// 3. Cluster admin on hub approves the CSR for UID and agent name of the ManagedCluster.
// 4. Cluster admin sets spec.acceptClient of ManagedCluster to true.
// 5. Cluster admin on managed cluster creates credential of kubeconfig to hub.
//
// Once the hub creates the cluster namespace, the Klusterlet agent on the ManagedCluster
// pushes the credential to the hub to use against the kube-apiserver of the ManagedCluster.
type ManagedCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec represents a desired configuration for the agent on the managed cluster.
	Spec ManagedClusterSpec `json:"spec"`

	// Status represents the current status of joined managed cluster
	// +optional
	Status ManagedClusterStatus `json:"status,omitempty"`
}

// ManagedClusterSpec provides the information to securely connect to a remote server
// and verify its identity.
type ManagedClusterSpec struct {
	// ManagedClusterClientConfigs represents a list of the apiserver address of the managed cluster.
	// If it is empty, the managed cluster has no accessible address for the hub to connect with it.
	// +optional
	ManagedClusterClientConfigs []ClientConfig `json:"managedClusterClientConfigs,omitempty"`

	// hubAcceptsClient represents that hub accepts the joining of Klusterlet agent on
	// the managed cluster with the hub. The default value is false, and can only be set
	// true when the user on hub has an RBAC rule to UPDATE on the virtual subresource
	// of managedclusters/accept.
	// When the value is set true, a namespace whose name is the same as the name of ManagedCluster
	// is created on the hub. This namespace represents the managed cluster, also role/rolebinding is created on
	// the namespace to grant the permision of access from the agent on the managed cluster.
	// When the value is set to false, the namespace representing the managed cluster is
	// deleted.
	// +required
	HubAcceptsClient bool `json:"hubAcceptsClient"`

	// LeaseDurationSeconds is used to coordinate the lease update time of Klusterlet agents on the managed cluster.
	// If its value is zero, the Klusterlet agent will update its lease every 60 seconds by default
	// +optional
	// +kubebuilder:default=60
	LeaseDurationSeconds int32 `json:"leaseDurationSeconds,omitempty"`

	// Taints is a property of managed cluster that allow the cluster to be repelled when scheduling.
	// Taints, including 'ManagedClusterUnavailable' and 'ManagedClusterUnreachable', can not be added/removed by agent
	// running on the managed cluster; while it's fine to add/remove other taints from either hub cluser or managed cluster.
	// +optional
	Taints []Taint `json:"taints,omitempty"`
}

// ClientConfig represents the apiserver address of the managed cluster.
// TODO include credential to connect to managed cluster kube-apiserver
type ClientConfig struct {
	// URL is the URL of apiserver endpoint of the managed cluster.
	// +required
	URL string `json:"url"`

	// CABundle is the ca bundle to connect to apiserver of the managed cluster.
	// System certs are used if it is not set.
	// +optional
	CABundle []byte `json:"caBundle,omitempty"`
}

// The managed cluster this Taint is attached to has the "effect" on
// any placement that does not tolerate the Taint.
type Taint struct {
	// Key is the taint key applied to a cluster. e.g. bar or foo.example.com/bar.
	// The regex it matches is (dns1123SubdomainFmt/)?(qualifiedNameFmt)
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^([a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*/)?(([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])$`
	// +kubebuilder:validation:MaxLength=316
	// +required
	Key string `json:"key"`
	// Value is the taint value corresponding to the taint key.
	// +kubebuilder:validation:MaxLength=1024
	// +optional
	Value string `json:"value,omitempty"`
	// Effect indicates the effect of the taint on placements that do not tolerate the taint.
	// Valid effects are NoSelect, PreferNoSelect and NoSelectIfNew.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum:=NoSelect;PreferNoSelect;NoSelectIfNew
	// +required
	Effect TaintEffect `json:"effect"`
	// TimeAdded represents the time at which the taint was added.
	// +nullable
	// +required
	TimeAdded metav1.Time `json:"timeAdded"`
}

type TaintEffect string

const (
	// TaintEffectNoSelect means placements are not allowed to select the cluster unless they tolerate the taint.
	// The cluster will be removed from the placement cluster decisions if a placement has already selected
	// this cluster.
	TaintEffectNoSelect TaintEffect = "NoSelect"
	// TaintEffectPreferNoSelect means the scheduler tries not to select the cluster, rather than prohibiting
	// placements from selecting the cluster entirely.
	TaintEffectPreferNoSelect TaintEffect = "PreferNoSelect"
	// TaintEffectNoSelectIfNew means placements are not allowed to select the cluster unless
	// 1) they tolerate the taint;
	// 2) they have already had the cluster in their cluster decisions;
	TaintEffectNoSelectIfNew TaintEffect = "NoSelectIfNew"
)

const (
	// ManagedClusterTaintUnavailable is the key of the taint added to a managed cluster when it is not available.
	// To be specific, the cluster has a condtion 'ManagedClusterConditionAvailable' with status of 'False';
	ManagedClusterTaintUnavailable string = "cluster.open-cluster-management.io/unavailable"
	// ManagedClusterTaintUnreachable is the key of the taint added to a managed cluster when it is not reachable.
	// To be specific,
	// 1) The cluster has no condition 'ManagedClusterConditionAvailable';
	// 2) Or the status of condtion 'ManagedClusterConditionAvailable' is 'Unknown';
	ManagedClusterTaintUnreachable string = "cluster.open-cluster-management.io/unreachable"
)

// ManagedClusterStatus represents the current status of joined managed cluster.
type ManagedClusterStatus struct {
	// Conditions contains the different condition statuses for this managed cluster.
	Conditions []metav1.Condition `json:"conditions"`

	// Capacity represents the total resource capacity from all nodeStatuses
	// on the managed cluster.
	Capacity ResourceList `json:"capacity,omitempty"`

	// Allocatable represents the total allocatable resources on the managed cluster.
	Allocatable ResourceList `json:"allocatable,omitempty"`

	// Version represents the kubernetes version of the managed cluster.
	Version ManagedClusterVersion `json:"version,omitempty"`

	// ClusterClaims represents cluster information that a managed cluster claims,
	// for example a unique cluster identifier (id.k8s.io) and kubernetes version
	// (kubeversion.open-cluster-management.io). They are written from the managed
	// cluster. The set of claims is not uniform across a fleet, some claims can be
	// vendor or version specific and may not be included from all managed clusters.
	// +optional
	ClusterClaims []ManagedClusterClaim `json:"clusterClaims,omitempty"`
}

// ManagedClusterVersion represents version information about the managed cluster.
// TODO add managed agent versions
type ManagedClusterVersion struct {
	// Kubernetes is the kubernetes version of managed cluster.
	// +optional
	Kubernetes string `json:"kubernetes,omitempty"`
}

// ManagedClusterClaim represents a ClusterClaim collected from a managed cluster.
type ManagedClusterClaim struct {
	// Name is the name of a ClusterClaim resource on managed cluster. It's a well known
	// or customized name to identify the claim.
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name,omitempty"`

	// Value is a claim-dependent string
	// +kubebuilder:validation:MaxLength=1024
	// +kubebuilder:validation:MinLength=1
	Value string `json:"value,omitempty"`
}

const (
	// ManagedClusterConditionJoined means the managed cluster has successfully joined the hub.
	ManagedClusterConditionJoined string = "ManagedClusterJoined"
	// ManagedClusterConditionHubAccepted means the request to join the cluster is
	// approved by cluster-admin on hub.
	ManagedClusterConditionHubAccepted string = "HubAcceptedManagedCluster"
	// ManagedClusterConditionHubDenied means the request to join the cluster is denied by
	// cluster-admin on hub.
	ManagedClusterConditionHubDenied string = "HubDeniedManagedCluster"
	// ManagedClusterConditionAvailable means the managed cluster is available. If a managed
	// cluster is available, the kube-apiserver is healthy and the Klusterlet agent is
	// running with the minimum deployment on this managed cluster
	ManagedClusterConditionAvailable string = "ManagedClusterConditionAvailable"
)

// ResourceName is the name identifying various resources in a ResourceList.
type ResourceName string

const (
	// ResourceCPU defines the number of CPUs in cores. (500m = .5 cores)
	ResourceCPU ResourceName = "cpu"
	// ResourceMemory defines the amount of memory in bytes. (500Gi = 500GiB = 500 * 1024 * 1024 * 1024)
	ResourceMemory ResourceName = "memory"
)

// ResourceList defines a map for the quantity of different resources, the definition
// matches the ResourceList defined in k8s.io/api/core/v1.
type ResourceList map[ResourceName]resource.Quantity

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ManagedClusterList is a collection of managed cluster.
type ManagedClusterList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata.
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#types-kinds
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	// Items is a list of managed clusters.
	Items []ManagedCluster `json:"items"`
}

const (
	// ClusterNameLabelKey is the key of a label to set ManagedCluster name.
	ClusterNameLabelKey = "open-cluster-management.io/cluster-name"
)
