package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Progressing",type=string,JSONPath=`.status.conditions[?(@.type=="Progressing")].status`
// +kubebuilder:printcolumn:name="Available",type=string,JSONPath=`.status.conditions[?(@.type=="Available")].status`
// +kubebuilder:printcolumn:name="Degraded",type=string,JSONPath=`.status.conditions[?(@.type=="Degraded")].status`

// ManagedClusterAddOn is the Custom Resource object which holds the current state
// of an add-on. This object is used by add-on operators to convey their state.
// This resource should be created in the ManagedCluster namespace.
type ManagedClusterAddOn struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	// spec holds configuration that could apply to any operator.
	// +kubebuilder:validation:Required
	// +required
	Spec ManagedClusterAddOnSpec `json:"spec"`

	// status holds the information about the state of an operator.  It is consistent with status information across
	// the Kubernetes ecosystem.
	// +optional
	Status ManagedClusterAddOnStatus `json:"status"`
}

// ManagedClusterAddOnSpec is empty for now.
type ManagedClusterAddOnSpec struct {
}

// ManagedClusterAddOnStatus provides information about the status of the operator.
// +k8s:deepcopy-gen=true
type ManagedClusterAddOnStatus struct {
	// conditions describe the state of the managed and monitored components for the operator.
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +optional
	Conditions []Condition `json:"conditions,omitempty"  patchStrategy:"merge" patchMergeKey:"type"`

	// relatedObjects is a list of objects that are "interesting" or related to this operator. Common uses are:
	// 1. the detailed resource driving the operator
	// 2. operator namespaces
	// 3. operand namespaces
	// 4. related ClusterManagementAddon resource
	// +optional
	RelatedObjects []ObjectReference `json:"relatedObjects,omitempty"`

	// addOnMeta is a reference to the metadata information for the add-on.
	// This should be same as the addOnMeta for the corresponding ClusterManagementAddOn resource.
	// +optional
	AddOnMeta AddOnMeta `json:"addOnMeta"`

	// addOnConfiguration is a reference to configuration information for the add-on.
	// This resource is use to locate the configuration resource for the add-on.
	// +optional
	AddOnConfiguration ConfigCoordinates `json:"addOnConfiguration"`
}

// ObjectReference contains enough information to let you inspect or modify the referred object.
type ObjectReference struct {
	// group of the referent.
	// +kubebuilder:validation:Required
	// +required
	Group string `json:"group"`
	// resource of the referent.
	// +kubebuilder:validation:Required
	// +required
	Resource string `json:"resource"`
	// name of the referent.
	// +kubebuilder:validation:Required
	// +required
	Name string `json:"name"`
}

// ManagedClusterAddOnList is a list of ManagedClusterAddOn resources.
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ManagedClusterAddOnList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []ManagedClusterAddOn `json:"items"`
}

// Condition contains details for one aspect of the current state of this API Resource.
// ---
// This struct is intended for direct use as an array at the field path .status.conditions.  For example,
// type FooStatus struct{
//     // Represents the observations of a foo's current state.
//     // Known .status.conditions.type are: "Available", "Progressing", and "Degraded"
//     // +patchMergeKey=type
//     // +patchStrategy=merge
//     // +listType=map
//     // +listMapKey=type
//     Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
//
//     // other fields
// }
type Condition struct {
	// type of condition in CamelCase or in foo.example.com/CamelCase.
	// ---
	// Many .condition.type values are consistent across resources like Available, but because arbitrary conditions can be
	// useful (see .node.status.conditions), the ability to deconflict is important.
	// The regex it matches is (dns1123SubdomainFmt/)?(qualifiedNameFmt)
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^([a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*/)?(([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])$`
	// +kubebuilder:validation:MaxLength=316
	Type string `json:"type" protobuf:"bytes,1,opt,name=type"`
	// status of the condition, one of True, False, Unknown.
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=True;False;Unknown
	Status metav1.ConditionStatus `json:"status" protobuf:"bytes,2,opt,name=status"`
	// observedGeneration represents the .metadata.generation that the condition was set based upon.
	// For instance, if .metadata.generation is currently 12, but the .status.conditions[x].observedGeneration is 9, the condition is out of date
	// with respect to the current state of the instance.
	// +optional
	// +kubebuilder:validation:Minimum=0
	ObservedGeneration int64 `json:"observedGeneration,omitempty" protobuf:"varint,3,opt,name=observedGeneration"`
	// lastTransitionTime is the last time the condition transitioned from one status to another.
	// This should be when the underlying condition changed.  If that is not known, then using the time when the API field changed is acceptable.
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Format=date-time
	LastTransitionTime metav1.Time `json:"lastTransitionTime" protobuf:"bytes,4,opt,name=lastTransitionTime"`
	// reason contains a programmatic identifier indicating the reason for the condition's last transition.
	// Producers of specific condition types may define expected values and meanings for this field,
	// and whether the values are considered a guaranteed API.
	// The value should be a CamelCase string.
	// This field may not be empty.
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MaxLength=1024
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=`^[A-Za-z]([A-Za-z0-9_,:]*[A-Za-z0-9_])?$`
	Reason string `json:"reason" protobuf:"bytes,5,opt,name=reason"`
	// message is a human readable message indicating details about the transition.
	// This may be an empty string.
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MaxLength=32768
	Message string `json:"message" protobuf:"bytes,6,opt,name=message"`
}

const (
	// Available indicates that the agent is functional and available in the cluster.
	Available string = "Available"

	// Progressing indicates that the operator is actively rolling out new code,
	// propagating config changes, or otherwise moving from one steady state to
	// another.  Operators should not report progressing when they are reconciling
	// a previously known state.
	Progressing string = "Progressing"

	// Degraded indicates that the operator's current state does not match its
	// desired state over a period of time resulting in a lower quality of service.
	// The period of time may vary by component, but a Degraded state represents
	// persistent observation of a condition.  As a result, a component should not
	// oscillate in and out of a Degraded state.  A service may be Available even
	// if it's degraded.  For example, your service may desire 3 running pods, but 1
	// pod is crash-looping.  The service is Available but Degraded because it
	// may have a lower quality of service.  A component may be Progressing but
	// not Degraded because the transition from one state to another does not
	// persist over a long enough period to report Degraded.  A service should not
	// report Degraded during the course of a normal upgrade.  A service may report
	// Degraded in response to a persistent infrastructure failure that requires
	// administrator intervention.  For example, if a control plane host is unhealthy
	// and must be replaced.  An operator should report Degraded if unexpected
	// errors occur over a period, but the expectation is that all unexpected errors
	// are handled as operators mature.
	Degraded string = "Degraded"
)
