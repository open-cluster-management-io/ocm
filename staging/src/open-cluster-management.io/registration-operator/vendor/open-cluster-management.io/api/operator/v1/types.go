package v1

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster

// ClusterManager configures the controllers on the hub that govern registration and work distribution for attached Klusterlets.
// In Default mode, ClusterManager will only be deployed in open-cluster-management-hub namespace.
// In Hosted mode, ClusterManager will be deployed in the namespace with the same name as cluster manager.
type ClusterManager struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec represents a desired deployment configuration of controllers that govern registration and work distribution for attached Klusterlets.
	// +kubebuilder:default={deployOption: {mode: Default}}
	Spec ClusterManagerSpec `json:"spec"`

	// Status represents the current status of controllers that govern the lifecycle of managed clusters.
	// +optional
	Status ClusterManagerStatus `json:"status,omitempty"`
}

// ClusterManagerSpec represents a desired deployment configuration of controllers that govern registration and work distribution for attached Klusterlets.
type ClusterManagerSpec struct {
	// RegistrationImagePullSpec represents the desired image of registration controller/webhook installed on hub.
	// +optional
	// +kubebuilder:default=quay.io/open-cluster-management/registration
	RegistrationImagePullSpec string `json:"registrationImagePullSpec,omitempty"`

	// WorkImagePullSpec represents the desired image configuration of work controller/webhook installed on hub.
	// +optional
	// +kubebuilder:default=quay.io/open-cluster-management/work
	WorkImagePullSpec string `json:"workImagePullSpec,omitempty"`

	// PlacementImagePullSpec represents the desired image configuration of placement controller/webhook installed on hub.
	// +optional
	// +kubebuilder:default=quay.io/open-cluster-management/placement
	PlacementImagePullSpec string `json:"placementImagePullSpec,omitempty"`

	// AddOnManagerImagePullSpec represents the desired image configuration of addon manager controller/webhook installed on hub.
	// +optional
	// +kubebuilder:default=quay.io/open-cluster-management/addon-manager
	AddOnManagerImagePullSpec string `json:"addOnManagerImagePullSpec,omitempty"`

	// NodePlacement enables explicit control over the scheduling of the deployed pods.
	// +optional
	NodePlacement NodePlacement `json:"nodePlacement,omitempty"`

	// DeployOption contains the options of deploying a cluster-manager
	// Default mode is used if DeployOption is not set.
	// +optional
	// +kubebuilder:default={mode: Default}
	DeployOption ClusterManagerDeployOption `json:"deployOption,omitempty"`

	// RegistrationConfiguration contains the configuration of registration
	// +optional
	RegistrationConfiguration *RegistrationHubConfiguration `json:"registrationConfiguration,omitempty"`

	// WorkConfiguration contains the configuration of work
	// +optional
	WorkConfiguration *WorkConfiguration `json:"workConfiguration,omitempty"`

	// AddOnManagerConfiguration contains the configuration of addon manager
	// +optional
	AddOnManagerConfiguration *AddOnManagerConfiguration `json:"addOnManagerConfiguration,omitempty"`
}

type RegistrationHubConfiguration struct {
	// AutoApproveUser represents a list of users that can auto approve CSR and accept client. If the credential of the
	// bootstrap-hub-kubeconfig matches to the users, the cluster created by the bootstrap-hub-kubeconfig will
	// be auto-registered into the hub cluster. This takes effect only when ManagedClusterAutoApproval feature gate
	// is enabled.
	// +optional
	AutoApproveUsers []string `json:"autoApproveUsers,omitempty"`

	// FeatureGates represents the list of feature gates for registration
	// If it is set empty, default feature gates will be used.
	// If it is set, featuregate/Foo is an example of one item in FeatureGates:
	//   1. If featuregate/Foo does not exist, registration-operator will discard it
	//   2. If featuregate/Foo exists and is false by default. It is now possible to set featuregate/Foo=[false|true]
	//   3. If featuregate/Foo exists and is true by default. If a cluster-admin upgrading from 1 to 2 wants to continue having featuregate/Foo=false,
	//  	he can set featuregate/Foo=false before upgrading. Let's say the cluster-admin wants featuregate/Foo=false.
	// +optional
	FeatureGates []FeatureGate `json:"featureGates,omitempty"`
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
}

type WorkConfiguration struct {
	// FeatureGates represents the list of feature gates for work
	// If it is set empty, default feature gates will be used.
	// If it is set, featuregate/Foo is an example of one item in FeatureGates:
	//   1. If featuregate/Foo does not exist, registration-operator will discard it
	//   2. If featuregate/Foo exists and is false by default. It is now possible to set featuregate/Foo=[false|true]
	//   3. If featuregate/Foo exists and is true by default. If a cluster-admin upgrading from 1 to 2 wants to continue having featuregate/Foo=false,
	//  	he can set featuregate/Foo=false before upgrading. Let's say the cluster-admin wants featuregate/Foo=false.
	// +optional
	FeatureGates []FeatureGate `json:"featureGates,omitempty"`
}

type AddOnManagerConfiguration struct {
	// FeatureGates represents the list of feature gates for addon manager
	// If it is set empty, default feature gates will be used.
	// If it is set, featuregate/Foo is an example of one item in FeatureGates:
	//   1. If featuregate/Foo does not exist, registration-operator will discard it
	//   2. If featuregate/Foo exists and is false by default. It is now possible to set featuregate/Foo=[false|true]
	//   3. If featuregate/Foo exists and is true by default. If a cluster-admin upgrading from 1 to 2 wants to continue having featuregate/Foo=false,
	//  	he can set featuregate/Foo=false before upgrading. Let's say the cluster-admin wants featuregate/Foo=false.
	// +optional
	FeatureGates []FeatureGate `json:"featureGates,omitempty"`
}

type FeatureGate struct {
	// Feature is the key of feature gate. e.g. featuregate/Foo.
	// +kubebuilder:validation:Required
	// +required
	Feature string `json:"feature"`

	// Mode is either Enable, Disable, "" where "" is Disable by default.
	// In Enable mode, a valid feature gate `featuregate/Foo` will be set to "--featuregate/Foo=true".
	// In Disable mode, a valid feature gate `featuregate/Foo` will be set to "--featuregate/Foo=false".
	// +kubebuilder:default:=Disable
	// +kubebuilder:validation:Enum:=Enable;Disable
	// +optional
	Mode FeatureGateModeType `json:"mode,omitempty"`
}

type FeatureGateModeType string

const (
	// Valid FeatureGateModeType value is Enable, Disable.
	FeatureGateModeTypeEnable  FeatureGateModeType = "Enable"
	FeatureGateModeTypeDisable FeatureGateModeType = "Disable"
)

// HostedClusterManagerConfiguration represents customized configurations we need to set for clustermanager in the Hosted mode.
type HostedClusterManagerConfiguration struct {
	// RegistrationWebhookConfiguration represents the customized webhook-server configuration of registration.
	// +optional
	RegistrationWebhookConfiguration WebhookConfiguration `json:"registrationWebhookConfiguration,omitempty"`

	// WorkWebhookConfiguration represents the customized webhook-server configuration of work.
	// +optional
	WorkWebhookConfiguration WebhookConfiguration `json:"workWebhookConfiguration,omitempty"`
}

// WebhookConfiguration has two properties: Address and Port.
type WebhookConfiguration struct {
	// Address represents the address of a webhook-server.
	// It could be in IP format or fqdn format.
	// The Address must be reachable by apiserver of the hub cluster.
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=^(([a-zA-Z0-9]|[a-zA-Z0-9][a-zA-Z0-9\-]*[a-zA-Z0-9])\.)*([A-Za-z0-9]|[A-Za-z0-9][A-Za-z0-9\-]*[A-Za-z0-9])$
	Address string `json:"address"`

	// Port represents the port of a webhook-server. The default value of Port is 443.
	// +optional
	// +default=443
	// +kubebuilder:default=443
	// +kubebuilder:validation:Maximum=65535
	Port int32 `json:"port,omitempty"`
}

// KlusterletDeployOption describes the deploy options for klusterlet
type KlusterletDeployOption struct {
	// Mode can be Default or Hosted. It is Default mode if not specified
	// In Default mode, all klusterlet related resources are deployed on the managed cluster.
	// In Hosted mode, only crd and configurations are installed on the spoke/managed cluster. Controllers run in another
	// cluster (defined as management-cluster) and connect to the mangaged cluster with the kubeconfig in secret of
	// "external-managed-kubeconfig"(a kubeconfig of managed-cluster with cluster-admin permission).
	// Note: Do not modify the Mode field once it's applied.
	// +optional
	Mode InstallMode `json:"mode"`
}

// ClusterManagerDeployOption describes the deploy options for cluster-manager
type ClusterManagerDeployOption struct {
	// Mode can be Default or Hosted.
	// In Default mode, the Hub is installed as a whole and all parts of Hub are deployed in the same cluster.
	// In Hosted mode, only crd and configurations are installed on one cluster(defined as hub-cluster). Controllers run in another
	// cluster (defined as management-cluster) and connect to the hub with the kubeconfig in secret of "external-hub-kubeconfig"(a kubeconfig
	// of hub-cluster with cluster-admin permission).
	// Note: Do not modify the Mode field once it's applied.
	// +required
	// +default=Default
	// +kubebuilder:validation:Required
	// +kubebuilder:default=Default
	// +kubebuilder:validation:Enum=Default;Hosted
	Mode InstallMode `json:"mode,omitempty"`

	// Hosted includes configurations we needs for clustermanager in the Hosted mode.
	// +optional
	Hosted *HostedClusterManagerConfiguration `json:"hosted,omitempty"`
}

// InstallMode represents the mode of deploy cluster-manager or klusterlet
type InstallMode string

const (
	// InstallModeDefault is the default deploy mode.
	// The cluster-manager will be deployed in the hub-cluster, the klusterlet will be deployed in the managed-cluster.
	InstallModeDefault InstallMode = "Default"

	// InstallModeHosted means deploying components outside.
	// The cluster-manager will be deployed outside of the hub-cluster, the klusterlet will be deployed outside of the managed-cluster.
	InstallModeHosted InstallMode = "Hosted"
)

// ClusterManagerStatus represents the current status of the registration and work distribution controllers running on the hub.
type ClusterManagerStatus struct {
	// ObservedGeneration is the last generation change you've dealt with
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions contain the different condition statuses for this ClusterManager.
	// Valid condition types are:
	// Applied: Components in hub are applied.
	// Available: Components in hub are available and ready to serve.
	// Progressing: Components in hub are in a transitioning state.
	// Degraded: Components in hub do not match the desired configuration and only provide
	// degraded service.
	Conditions []metav1.Condition `json:"conditions"`

	// Generations are used to determine when an item needs to be reconciled or has changed in a way that needs a reaction.
	// +optional
	Generations []GenerationStatus `json:"generations,omitempty"`

	// RelatedResources are used to track the resources that are related to this ClusterManager.
	// +optional
	RelatedResources []RelatedResourceMeta `json:"relatedResources,omitempty"`
}

// RelatedResourceMeta represents the resource that is managed by an operator
type RelatedResourceMeta struct {
	// group is the group of the resource that you're tracking
	// +required
	Group string `json:"group"`

	// version is the version of the thing you're tracking
	// +required
	Version string `json:"version"`

	// resource is the resource type of the resource that you're tracking
	// +required
	Resource string `json:"resource"`

	// namespace is where the thing you're tracking is
	// +optional
	Namespace string `json:"namespace"`

	// name is the name of the resource that you're tracking
	// +required
	Name string `json:"name"`
}

// GenerationStatus keeps track of the generation for a given resource so that decisions about forced updates can be made.
// The definition matches the GenerationStatus defined in github.com/openshift/api/v1
type GenerationStatus struct {
	// group is the group of the resource that you're tracking
	// +required
	Group string `json:"group"`

	// version is the version of the resource that you're tracking
	// +required
	Version string `json:"version"`

	// resource is the resource type of the resource that you're tracking
	// +required
	Resource string `json:"resource"`

	// namespace is where the resource that you're tracking is
	// +optional
	Namespace string `json:"namespace"`

	// name is the name of the resource that you're tracking
	// +required
	Name string `json:"name"`

	// lastGeneration is the last generation of the resource that controller applies
	// +required
	LastGeneration int64 `json:"lastGeneration" protobuf:"varint,5,opt,name=lastGeneration"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterManagerList is a collection of deployment configurations for registration and work distribution controllers.
type ClusterManagerList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata.
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#types-kinds
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	// Items is a list of deployment configurations for registration and work distribution controllers.
	Items []ClusterManager `json:"items"`
}

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

	// ClusterName is the name of the managed cluster to be created on hub.
	// The Klusterlet agent generates a random name if it is not set, or discovers the appropriate cluster name on OpenShift.
	// +optional
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=^[a-z0-9]([-a-z0-9]*[a-z0-9])?$
	ClusterName string `json:"clusterName,omitempty"`

	// ExternalServerURLs represents the a list of apiserver urls and ca bundles that is accessible externally
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

// NodePlacement describes node scheduling configuration for the pods.
type NodePlacement struct {
	// NodeSelector defines which Nodes the Pods are scheduled on. The default is an empty list.
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Tolerations is attached by pods to tolerate any taint that matches
	// the triple <key,value,effect> using the matching operator <operator>.
	// The default is an empty list.
	// +optional
	Tolerations []v1.Toleration `json:"tolerations,omitempty"`
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
