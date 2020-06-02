package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster

// ClusterManager configures the controllers on the hub that govern registration and work distribution for attached klusterlets.
// ClusterManager will be only deployed in open-cluster-management-hub namespace.
type ClusterManager struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// Spec represents a desired deployment configuration of controllers that govern registration and work distribution for attached klusterlets.
	Spec ClusterManagerSpec `json:"spec" protobuf:"bytes,2,opt,name=spec"`

	// Status represents the current status of controllers that govern the lifecycle of managed clusters.
	// +optional
	Status ClusterManagerStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

// ClusterManagerSpec represents a desired deployment configuration of controllers that govern registration and work distribution for attached klusterlets.
type ClusterManagerSpec struct {
	// RegistrationImagePullSpec represents the desired image of registration controller installed on hub.
	// +required
	RegistrationImagePullSpec string `json:"registrationImagePullSpec" protobuf:"bytes,1,opt,name=registrationImagePullSpec"`
}

// ClusterManagerStatus represents the current status of the registration and work distribution controllers running on the hub.
type ClusterManagerStatus struct {
	// Conditions contain the different condition statuses for this ClusterManager.
	// Valid condition types are:
	// Applied: components in hub are applied.
	// Available: components in hub are available and ready to serve.
	// Progressing: components in hub are in a transitioning state.
	// Degraded: components in hub do not match the desired configuration and only provide
	// degraded service.
	Conditions []StatusCondition `json:"conditions" protobuf:"bytes,1,opt,name=conditions"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterManagerList is a collection of deployment configurations for registration and work distribution controllers.
type ClusterManagerList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata.
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#types-kinds
	// +optional
	metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// Items is a list of deployment configurations for registration and work distribution controllers.
	Items []ClusterManager `json:"items" protobuf:"bytes,2,rep,name=items"`
}

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster

// Klusterlet represents controllers on the managed cluster. When configured,
// the Klusterlet requires a secret named of bootstrap-hub-kubeconfig in the
// same namespace to allow API requests to the hub for the registration protocol.
type Klusterlet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// Spec represents the desired deployment configuration of klusterlet agent.
	Spec KlusterletSpec `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`

	// Status represents the current status of klusterlet agent.
	Status KlusterletStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

// KlusterletSpec represents the desired deployment configuration of klusterlet agent.
type KlusterletSpec struct {
	// Namespace is the namespace to deploy the agent.
	// The namespace must have a prefix of "open-cluster-management-", and if it is not set,
	// the namespace of "open-cluster-management-spoke" is used to deploy agent.
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

	// ExternalServerURLs represents the a list of apiserver urls and ca bundles that is accessible externally
	// If it is set empty, spoke cluster has no externally accessible url that hub cluster can visit.
	// +optional
	ExternalServerURLs []ServerURL `json:"externalServerURLs,omitempty" protobuf:"bytes,5,opt,name=externalServerURLs"`
}

// ServerURL represents the apiserver url and ca bundle that is accessible externally
type ServerURL struct {
	// URL is the url of apiserver endpoint of the spoke cluster.
	// +required
	URL string `json:"url" protobuf:"bytes,1,opt,name=url"`

	// CABundle is the ca bundle to connect to apiserver of the spoke cluster.
	// System certs are used if it is not set.
	// +optional
	CABundle []byte `json:"caBundle,omitempty" protobuf:"bytes,2,opt,name=caBundle"`
}

// KlusterletStatus represents the current status of klusterlet agent.
type KlusterletStatus struct {
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

// KlusterletList is a collection of klusterlet agent.
type KlusterletList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata.
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#types-kinds
	// +optional
	metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// Items is a list of klusterlet agent.
	Items []Klusterlet `json:"items" protobuf:"bytes,2,rep,name=items"`
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
