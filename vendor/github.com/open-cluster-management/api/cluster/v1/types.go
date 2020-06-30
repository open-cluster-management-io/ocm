package v1

import (
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ManagedCluster represents the desired state and current status of managed
// cluster. ManagedCluster is a cluster scoped resource. The name is the cluster
// UID.
//
// The cluster join process follows a double opt-in process:
//
// 1. agent on managed cluster creates CSR on hub with cluster UID and agent name.
// 2. agent on managed cluster creates ManagedCluster on hub.
// 3. cluster admin on hub approves the CSR for the ManagedCluster's UID and agent name.
// 4. cluster admin sets spec.acceptClient of ManagedCluster to true.
// 5. cluster admin on managed cluster creates credential of kubeconfig to hub.
//
// Once the hub creates the cluster namespace, the Klusterlet agent on the Managed Cluster
// pushes the credential to the hub to use against the managed cluster's kube-apiserver.
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
	// If it is empty, managed cluster has no accessible address to be visited from hub.
	// +optional
	ManagedClusterClientConfigs []ClientConfig `json:"managedClusterClientConfigs,omitempty"`

	// hubAcceptsClient represents that hub accepts the join of Klusterlet agent on
	// the managed cluster to the hub. The default value is false, and can only be set
	// true when the user on hub has an RBAC rule to UPDATE on the virtual subresource
	// of managedclusters/accept.
	// When the value is set true, a namespace whose name is same as the name of ManagedCluster
	// is created on hub representing the managed cluster, also role/rolebinding is created on
	// the namespace to grant the permision of access from agent on managed cluster.
	// When the value is set false, the namespace representing the managed cluster is
	// deleted.
	// +required
	HubAcceptsClient bool `json:"hubAcceptsClient"`

	// LeaseDurationSeconds is used to coordinate the lease update time of Klusterlet agents on the managed cluster.
	// If its value is zero, the Klusterlet agent will update its lease every 60s by default
	// +optional
	LeaseDurationSeconds int32 `json:"leaseDurationSeconds,omitempty"`
}

// ClientConfig represents the apiserver address of the managed cluster.
// TODO include credential to connect to managed cluster kube-apiserver
type ClientConfig struct {
	// URL is the url of apiserver endpoint of the managed cluster.
	// +required
	URL string `json:"url"`

	// CABundle is the ca bundle to connect to apiserver of the managed cluster.
	// System certs are used if it is not set.
	// +optional
	CABundle []byte `json:"caBundle,omitempty"`
}

// ManagedClusterStatus represents the current status of joined managed cluster.
type ManagedClusterStatus struct {
	// Conditions contains the different condition statuses for this managed cluster.
	Conditions []StatusCondition `json:"conditions"`

	// Capacity represents the total resource capacity from all nodeStatuses
	// on the managed cluster.
	Capacity ResourceList `json:"capacity,omitempty"`

	// Allocatable represents the total allocatable resources on the managed cluster.
	Allocatable ResourceList `json:"allocatable,omitempty"`

	// Version represents the kubernetes version of the managed cluster.
	Version ManagedClusterVersion `json:"version,omitempty"`
}

// ManagedClusterVersion represents version information about the managed cluster.
// TODO add managed agent versions
type ManagedClusterVersion struct {
	// Kubernetes is the kubernetes version of managed cluster.
	// +optional
	Kubernetes string `json:"kubernetes,omitempty"`
}

const (
	// ManagedClusterConditionJoined means the managed cluster has successfully joined the hub
	ManagedClusterConditionJoined string = "ManagedClusterJoined"
	// ManagedClusterConditionHubAccepted means the request to join the cluster is
	// approved by cluster-admin on hub
	ManagedClusterConditionHubAccepted string = "HubAcceptedManagedCluster"
	// ManagedClusterConditionHubDenied means the request to join the cluster is denied by
	// cluster-admin on hub
	ManagedClusterConditionHubDenied string = "HubDeniedManagedCluster"
	// ManagedClusterConditionAvailable means the managed cluster is available, if a managed
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
// matches the ResourceList defined in k8s.io/api/core/v1
type ResourceList map[ResourceName]resource.Quantity

// StatusCondition contains condition information for a managed cluster.
type StatusCondition struct {
	// Type is the type of the cluster condition.
	// +required
	Type string `json:"type"`

	// Status is the status of the condition. One of True, False, Unknown.
	// +required
	Status metav1.ConditionStatus `json:"status"`

	// LastTransitionTime is the last time the condition changed from one status to another.
	// +required
	LastTransitionTime metav1.Time `json:"lastTransitionTime"`

	// Reason is a (brief) reason for the condition's last status change.
	// +required
	Reason string `json:"reason"`

	// Message is a human-readable message indicating details about the last status change.
	// +required
	Message string `json:"message"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ManagedClusterList is a collection of managed cluster.
type ManagedClusterList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata.
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#types-kinds
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	// Items is a list of managed cluster.
	Items []ManagedCluster `json:"items"`
}
