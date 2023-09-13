package v1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster

// Klusterlet represents controllers to install the resources for a managed cluster.
// When configured, the Klusterlet requires a secret named bootstrap-hub-kubeconfig in the
// agent namespace to allow API requests to the hub for the registration protocol.
// In Hosted mode, the Klusterlet requires an additional secret named external-managed-kubeconfig
// in the agent namespace to allow API requests to the managed cluster for resources installation.
type Klusterlet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec represents the desired deployment configuration of Klusterlet agent.
	Spec KlusterletSpec `json:"spec,omitempty"`

	// Status represents the current status of Klusterlet agent.
	Status KlusterletStatus `json:"status,omitempty"`
}

// KlusterletSpec represents the desired deployment configuration of Klusterlet agent.
type KlusterletSpec struct {
	// Namespace is the namespace to deploy the agent on the managed cluster.
	// The namespace must have a prefix of "open-cluster-management-", and if it is not set,
	// the namespace of "open-cluster-management-agent" is used to deploy agent.
	// In addition, the add-ons are deployed to the namespace of "{Namespace}-addon".
	// In the Hosted mode, this namespace still exists on the managed cluster to contain
	// necessary resources, like service accounts, roles and rolebindings, while the agent
	// is deployed to the namespace with the same name as klusterlet on the management cluster.
	// +optional
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=^open-cluster-management-[-a-z0-9]*[a-z0-9]$
	Namespace string `json:"namespace,omitempty"`

	// RegistrationImagePullSpec represents the desired image configuration of registration agent.
	// quay.io/open-cluster-management.io/registration:latest will be used if unspecified.
	// +optional
	RegistrationImagePullSpec string `json:"registrationImagePullSpec,omitempty"`

	// WorkImagePullSpec represents the desired image configuration of work agent.
	// quay.io/open-cluster-management.io/work:latest will be used if unspecified.
	// +optional
	WorkImagePullSpec string `json:"workImagePullSpec,omitempty"`

	// ImagePullSpec represents the desired image configuration of agent, it takes effect only when
	// singleton mode is set. quay.io/open-cluster-management.io/registration-operator:latest will
	// be used if unspecified
	// +optional
	ImagePullSpec string `json:"imagePullSpec,omitempty"`

	// ClusterName is the name of the managed cluster to be created on hub.
	// The Klusterlet agent generates a random name if it is not set, or discovers the appropriate cluster name on OpenShift.
	// +optional
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=^[a-z0-9]([-a-z0-9]*[a-z0-9])?$
	ClusterName string `json:"clusterName,omitempty"`

	// ExternalServerURLs represents a list of apiserver urls and ca bundles that is accessible externally
	// If it is set empty, managed cluster has no externally accessible url that hub cluster can visit.
	// +optional
	ExternalServerURLs []ServerURL `json:"externalServerURLs,omitempty"`

	// NodePlacement enables explicit control over the scheduling of the deployed pods.
	// +optional
	NodePlacement NodePlacement `json:"nodePlacement,omitempty"`

	// DeployOption contains the options of deploying a klusterlet
	// +optional
	DeployOption KlusterletDeployOption `json:"deployOption,omitempty"`

	// RegistrationConfiguration contains the configuration of registration
	// +optional
	RegistrationConfiguration *RegistrationConfiguration `json:"registrationConfiguration,omitempty"`

	// WorkConfiguration contains the configuration of work
	// +optional
	WorkConfiguration *WorkConfiguration `json:"workConfiguration,omitempty"`

	// HubApiServerHostAlias contains the host alias for hub api server.
	// registration-agent and work-agent will use it to communicate with hub api server.
	// +optional
	HubApiServerHostAlias *HubApiServerHostAlias `json:"hubApiServerHostAlias,omitempty"`
}

// ServerURL represents the apiserver url and ca bundle that is accessible externally
type ServerURL struct {
	// URL is the url of apiserver endpoint of the managed cluster.
	// +required
	URL string `json:"url"`

	// CABundle is the ca bundle to connect to apiserver of the managed cluster.
	// System certs are used if it is not set.
	// +optional
	CABundle []byte `json:"caBundle,omitempty"`
}

// HubApiServerHostAlias holds the mapping between IP and hostname that will be injected as an entry in the
// pod's hosts file.
type HubApiServerHostAlias struct {
	// IP address of the host file entry.
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^(?:(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.){3}(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)$`
	IP string `json:"ip"`

	// Hostname for the above IP address.
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^(([a-zA-Z0-9]|[a-zA-Z0-9][a-zA-Z0-9\-]*[a-zA-Z0-9])\.)*([A-Za-z0-9]|[A-Za-z0-9][A-Za-z0-9\-]*[A-Za-z0-9])$`
	Hostname string `json:"hostname"`
}

type RegistrationConfiguration struct {
	// clientCertExpirationSeconds represents the seconds of a client certificate to expire. If it is not set or 0, the default
	// duration seconds will be set by the hub cluster. If the value is larger than the max signing duration seconds set on
	// the hub cluster, the max signing duration seconds will be set.
	// +optional
	ClientCertExpirationSeconds int32 `json:"clientCertExpirationSeconds,omitempty"`

	// FeatureGates represents the list of feature gates for registration
	// If it is set empty, default feature gates will be used.
	// If it is set, featuregate/Foo is an example of one item in FeatureGates:
	//   1. If featuregate/Foo does not exist, registration-operator will discard it
	//   2. If featuregate/Foo exists and is false by default. It is now possible to set featuregate/Foo=[false|true]
	//   3. If featuregate/Foo exists and is true by default. If a cluster-admin upgrading from 1 to 2 wants to continue having featuregate/Foo=false,
	//  	he can set featuregate/Foo=false before upgrading. Let's say the cluster-admin wants featuregate/Foo=false.
	// +optional
	FeatureGates []FeatureGate `json:"featureGates,omitempty"`

	// ClusterAnnotations is annotations with the reserve prefix "agent.open-cluster-management.io" set on
	// ManagedCluster when creating only, other actors can update it afterwards.
	// +optional
	ClusterAnnotations map[string]string `json:"clusterAnnotations,omitempty"`
}

const (
	// ClusterAnnotationsKeyPrefix is the prefix of annotations set on ManagedCluster when creating only.
	ClusterAnnotationsKeyPrefix = "agent.open-cluster-management.io"
)

// KlusterletDeployOption describes the deployment options for klusterlet
type KlusterletDeployOption struct {
	// Mode can be Default, Hosted, Singleton or SingletonHosted. It is Default mode if not specified
	// In Default mode, all klusterlet related resources are deployed on the managed cluster.
	// In Hosted mode, only crd and configurations are installed on the spoke/managed cluster. Controllers run in another
	// cluster (defined as management-cluster) and connect to the mangaged cluster with the kubeconfig in secret of
	// "external-managed-kubeconfig"(a kubeconfig of managed-cluster with cluster-admin permission).
	// In Singleton mode, registration/work agent is started as a single deployment.
	// In SingletonHosted mode, agent is started as a single deployment in hosted mode.
	// Note: Do not modify the Mode field once it's applied.
	// +optional
	Mode InstallMode `json:"mode"`
}

// KlusterletStatus represents the current status of Klusterlet agent.
type KlusterletStatus struct {
	// ObservedGeneration is the last generation change you've dealt with
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions contain the different condition statuses for this Klusterlet.
	// Valid condition types are:
	// Applied: Components have been applied in the managed cluster.
	// Available: Components in the managed cluster are available and ready to serve.
	// Progressing: Components in the managed cluster are in a transitioning state.
	// Degraded: Components in the managed cluster do not match the desired configuration and only provide
	// degraded service.
	Conditions []metav1.Condition `json:"conditions"`

	// Generations are used to determine when an item needs to be reconciled or has changed in a way that needs a reaction.
	// +optional
	Generations []GenerationStatus `json:"generations,omitempty"`

	// RelatedResources are used to track the resources that are related to this Klusterlet.
	// +optional
	RelatedResources []RelatedResourceMeta `json:"relatedResources,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// KlusterletList is a collection of Klusterlet agents.
type KlusterletList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata.
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#types-kinds
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	// Items is a list of Klusterlet agents.
	Items []Klusterlet `json:"items"`
}
