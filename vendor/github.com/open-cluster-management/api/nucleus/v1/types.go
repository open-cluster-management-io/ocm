package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster

// HubCore represents a deployment of nucleus hub core component.
// HubCore will be only deployed in open-cluster-management namespace.
type HubCore struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// Spec represents a desired deployment configuration of nucleus hub
	Spec HubCoreSpec `json:"spec" protobuf:"bytes,2,opt,name=spec"`

	// Status represents the current status of nucleus hub
	// +optional
	Status HubCoreStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

// HubCoreSpec represents a desired deployment configuration of nucleus hub.
type HubCoreSpec struct {
	// RegistrationImagePullSpec represents the desired image of registration controller installed on hub.
	// +required
	RegistrationImagePullSpec string `json:"registrationImagePullSpec" protobuf:"bytes,1,opt,name=registrationImagePullSpec"`
}

// HubCoreStatus represents the current status of nucleus hub.
type HubCoreStatus struct {
	// Conditions contain the different condition statuses for this hubcore.
	// Valid condition types are:
	// Applied: components in hub is applied.
	// Available: components in hub are available and ready to serve.
	// Progressing: components in hub are in a transitioning state.
	// Degraded: components in hub do not match the desired configuration and only provide
	// degraded service.
	Conditions []StatusCondition `json:"conditions" protobuf:"bytes,1,opt,name=conditions"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// HubCoreList is a collection of nucleus hub.
type HubCoreList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata.
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#types-kinds
	// +optional
	metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// Items is a list of nucleus hub.
	Items []HubCore `json:"items" protobuf:"bytes,2,rep,name=items"`
}

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster

// SpokeCore represents a deployment of nucleus core agent on spoke cluster.
// When the deployment of spoke core agent is deployed, it will requires a secret
// with the name of bootstrap-hub-kubeconfig in the namespace defined in the spec.
type SpokeCore struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// Spec represents the desired deployment configuratioin of nucleus agent.
	Spec SpokeCoreSpec `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`

	// Status represents the current status of nucleus agent.
	Status SpokeCoreStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

// SpokeCoreSpec represents the desired deployment configuratioin of nucleus spoke agent.
type SpokeCoreSpec struct {
	// Namespace is the namespace to deploy the agent.
	// The namespace must have a prefix of "open-cluster-management-", and if it is not set,
	// the namespace of "open-cluster-management" is used to deploy agent.
	// +optional
	Namespace string `json:"namespace,omitempty" protobuf:"bytes,1,opt,name=namespace"`

	// RegistrationImagePullSpec represents the desired image configuration of registration agent.
	// +required
	RegistrationImagePullSpec string `json:"registrationImagePullSpec" protobuf:"bytes,2,opt,name=registrationImagePullSpec"`

	// WorkImagePullSpec represents the desired image configuration of work agent.
	// +required
	WorkImagePullSpec string `json:"workImagePullSpec,omitempty" protobuf:"bytes,3,opt,name=workImagePullSpec"`

	// ClusterName is the name of the spoke cluster to be created on hub.
	// The spoke agent generates a random name if it is not set, or discovers the appropriate cluster name on openshift.
	// +optional
	ClusterName string `json:"clusterName,omitempty" protobuf:"bytes,4,opt,name=clusterName"`
}

// SpokeCoreStatus represents the current status of nucleus spoke agent.
type SpokeCoreStatus struct {
	// Conditions contain the different condition statuses for this spokecore.
	// Valid condition types are:
	// Applied: components in spoke is applied.
	// Available: components in spoke are available and ready to serve.
	// Progressing: components in spoke are in a transitioning state.
	// Degraded: components in spoke do not match the desired configuration and only provide
	// degraded service.
	Conditions []StatusCondition `json:"conditions" protobuf:"bytes,1,opt,name=conditions"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// SpokeCoreList is a collection of nucleus spoke agent.
type SpokeCoreList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata.
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#types-kinds
	// +optional
	metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// Items is a list of nucleus spoke agent.
	Items []SpokeCore `json:"items" protobuf:"bytes,2,rep,name=items"`
}

// StatusCondition contains condition information.
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
