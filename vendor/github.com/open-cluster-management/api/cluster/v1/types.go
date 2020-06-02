package v1

import (
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// SpokeCluster represents the desired state and current status of spoke
// cluster. SpokeCluster is a cluster scoped resource. The name is the cluster
// UID.
//
// The cluster join process follows a double opt-in process:
//
// 1. agent on spoke cluster creates CSR on hub with cluster UID and agent name.
// 2. agent on spoke cluster creates spokecluster on hub.
// 3. cluster admin on hub approves the CSR for the spoke's cluster UID and agent name.
// 4. cluster admin set spec.acceptSpokeCluster of spokecluster to true.
// 5. cluster admin on spoke creates credential of kubeconfig to spoke.
//
// Once the hub creates the cluster namespace, the spoke agent pushes the
// credential to the hub to use against the spoke's kube-apiserver.
type SpokeCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// Spec represents a desired configuration for the agent on the spoke cluster.
	Spec SpokeClusterSpec `json:"spec" protobuf:"bytes,2,opt,name=spec"`

	// Status represents the current status of joined spoke cluster
	// +optional
	Status SpokeClusterStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

// SpokeClusterSpec provides the information to securely connect to a remote server
// and verify its identity.
type SpokeClusterSpec struct {
	// SpokeClientConfigs represents a list of the apiserver address of the spoke cluster.
	// If it is empty, spoke cluster has no accessible address to be visited from hub.
	// +optional
	SpokeClientConfigs []ClientConfig `json:"spokeClientConfigs,omitempty" protobuf:"bytes,1,opt,name=spokeClientConfigs"`

	// AcceptSpokeCluster reprsents that hub accepts the join of spoke agent.
	// Its default value is false, and can only be set true when the user on hub
	// has an RBAC rule to UPDATE on the virtual subresource of spokeclusters/accept.
	// When the vaule is set true, a namespace whose name is same as the name of SpokeCluster
	// is created on hub representing the spoke cluster, also role/rolebinding is created on
	// the namespace to grant the permision of access from agent on spoke.
	// When the value is set false, the namespace representing the spoke cluster is
	// deleted.
	// +required
	HubAcceptsClient bool `json:"hubAcceptsClient" protobuf:"bytes,2,opt,name=hubAcceptsClient"`

	// LeaseDurationSeconds is used to coordinate the lease update time of spoke agents.
	// If its value is zero, the spoke agent will update its lease per 60s by default
	// +optional
	LeaseDurationSeconds int32 `json:"leaseDurationSeconds,omitempty" protobuf:"varint,3,opt,name=leaseDurationSeconds"`
}

// ClientConfig represents the apiserver address of the spoke cluster.
// TODO include credential to connect to spoke cluster kube-apiserver
type ClientConfig struct {
	// URL is the url of apiserver endpoint of the spoke cluster.
	// +required
	URL string `json:"url" protobuf:"bytes,1,opt,name=url"`

	// CABundle is the ca bundle to connect to apiserver of the spoke cluster.
	// System certs are used if it is not set.
	// +optional
	CABundle []byte `json:"caBundle,omitempty" protobuf:"bytes,2,opt,name=caBundle"`
}

// SpokeClusterStatus represents the current status of joined spoke cluster.
type SpokeClusterStatus struct {
	// Conditions contains the different condition statuses for this spoke cluster.
	Conditions []StatusCondition `json:"conditions" protobuf:"bytes,1,rep,name=conditions"`

	// Capacity represents the total resource capacity from all nodeStatuses
	// on the spoke cluster.
	Capacity ResourceList `json:"capacity,omitempty" protobuf:"bytes,2,rep,name=capacity,casttype=ResourceList,castkey=ResourceName"`

	// Allocatable represents the total allocatable resources on the spoke cluster.
	Allocatable ResourceList `json:"allocatable,omitempty" protobuf:"bytes,3,rep,name=allocatable,casttype=ResourceList,castkey=ResourceName"`

	// Version represents the kubernetes version of the spoke cluster.
	Version SpokeVersion `json:"version,omitempty" protobuf:"bytes,4,opt,name=version"`
}

// SpokeVersion represents version information about the spoke cluster.
// TODO add spoke agent versions
type SpokeVersion struct {
	// Kubernetes is the kubernetes version of spoke cluster
	// +optional
	Kubernetes string `json:"kubernetes,omitempty" protobuf:"bytes,1,opt,name=kubernetes"`
}

const (
	// SpokeClusterConditionJoined means the spoke cluster has successfully joined the hub
	SpokeClusterConditionJoined string = "SpokeClusterJoined"
	// SpokeClusterConditionHubAccepted means the request to join the cluster is
	// approved by cluster-admin on hub
	SpokeClusterConditionHubAccepted string = "HubAcceptedSpoke"
	// SpokeClusterConditionHubDenied means the request to join the cluster is denied by
	// cluster-admin on hub
	SpokeClusterConditionHubDenied string = "HubDeniedSpoke"
	// SpokeClusterConditionAvailable means the spoke cluster is available, if a spoke
	// cluster is available, the kube-apiserver is health and the registration agent is
	// running with the minimum deployment on this spoke cluster
	SpokeClusterConditionAvailable string = "SpokeClusterConditionAvailable"
)

// ResourceName is the name identifying various resources in a ResourceList.
type ResourceName string

const (
	// CPU, in cores. (500m = .5 cores)
	ResourceCPU ResourceName = "cpu"
	// Memory, in bytes. (500Gi = 500GiB = 500 * 1024 * 1024 * 1024)
	ResourceMemory ResourceName = "memory"
)

// ResourceList defines a map for the quantity of different resources, the definition
// matches the ResourceList defined in k8s.io/api/core/v1
type ResourceList map[ResourceName]resource.Quantity

// StatusCondition contains condition information for a spoke cluster.
type StatusCondition struct {
	// Type is the type of the cluster condition.
	// +required
	Type string `json:"type" protobuf:"bytes,1,opt,name=type"`

	// Status is the status of the condition. One of True, False, Unknown.
	// +required
	Status metav1.ConditionStatus `json:"status" protobuf:"bytes,2,opt,name=status,casttype=k8s.io/apimachinery/pkg/apis/meta/v1.ConditionStatus"`

	// LastTransitionTime is the last time the condition changed from one status to another.
	// +required
	LastTransitionTime metav1.Time `json:"lastTransitionTime" protobuf:"bytes,3,opt,name=lastTransitionTime"`

	// Reason is a (brief) reason for the condition's last status change.
	// +required
	Reason string `json:"reason" protobuf:"bytes,4,opt,name=reason"`

	// Message is a human-readable message indicating details about the last status change.
	// +required
	Message string `json:"message" protobuf:"bytes,5,opt,name=message"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// SpokeClusterList is a collection of spoke cluster.
type SpokeClusterList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata.
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#types-kinds
	// +optional
	metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// Items is a list of spoke cluster.
	Items []SpokeCluster `json:"items" protobuf:"bytes,2,rep,name=items"`
}
