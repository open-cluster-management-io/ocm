package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

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
	// +kubebuilder:validation:MaxLength=57
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
	WorkConfiguration *WorkAgentConfiguration `json:"workConfiguration,omitempty"`

	// HubApiServerHostAlias contains the host alias for hub api server.
	// registration-agent and work-agent will use it to communicate with hub api server.
	// +optional
	HubApiServerHostAlias *HubApiServerHostAlias `json:"hubApiServerHostAlias,omitempty"`

	// ResourceRequirement specify QoS classes of deployments managed by klusterlet.
	// It applies to all the containers in the deployments.
	// +optional
	ResourceRequirement *ResourceRequirement `json:"resourceRequirement,omitempty"`

	// PriorityClassName is the name of the PriorityClass that will be used by the
	// deployed klusterlet agent. It will be ignored when the PriorityClass/v1 API
	// is not available on the managed cluster.
	// +optional
	PriorityClassName string `json:"priorityClassName,omitempty"`
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

	// KubeAPIQPS indicates the maximum QPS while talking with apiserver on the spoke cluster.
	// If it is set empty, use the default value: 50
	// +optional
	// +kubebuilder:default:=50
	KubeAPIQPS int32 `json:"kubeAPIQPS,omitempty"`

	// KubeAPIBurst indicates the maximum burst of the throttle while talking with apiserver on the spoke cluster.
	// If it is set empty, use the default value: 100
	// +optional
	// +kubebuilder:default:=100
	KubeAPIBurst int32 `json:"kubeAPIBurst,omitempty"`

	// BootstrapKubeConfigs defines the ordered list of bootstrap kubeconfigs. The order decides which bootstrap kubeconfig to use first when rebootstrap.
	//
	// When the agent loses the connection to the current hub over HubConnectionTimeoutSeconds, or the managedcluster CR
	// is set `hubAcceptsClient=false` on the hub, the controller marks the related bootstrap kubeconfig as "failed".
	//
	// A failed bootstrapkubeconfig won't be used for the duration specified by SkipFailedBootstrapKubeConfigSeconds.
	// But if the user updates the content of a failed bootstrapkubeconfig, the "failed" mark will be cleared.
	// +optional
	BootstrapKubeConfigs BootstrapKubeConfigs `json:"bootstrapKubeConfigs,omitempty"`

	// This provides driver details required to register with hub
	// +optional
	RegistrationDriver RegistrationDriver `json:"registrationDriver,omitempty"`

	// ClusterClaimConfiguration represents the configuration of ClusterClaim
	// Effective only when the `ClusterClaim` feature gate is enabled.
	// +optional
	ClusterClaimConfiguration *ClusterClaimConfiguration `json:"clusterClaimConfiguration,omitempty"`
}

// ClusterClaimConfiguration represents the configuration of ClusterClaim
type ClusterClaimConfiguration struct {
	// Maximum number of custom ClusterClaims allowed.
	// +kubebuilder:validation:Required
	// +kubebuilder:default:=20
	// +required
	MaxCustomClusterClaims int32 `json:"maxCustomClusterClaims"`

	// Custom suffixes for reserved ClusterClaims.
	// +optional
	// +kubebuilder:validation:MaxItems=10
	// +kubebuilder:validation:items:MinLength=1
	// +kubebuilder:validation:items:MaxLength=64
	ReservedClusterClaimSuffixes []string `json:"reservedClusterClaimSuffixes,omitempty"`
}

type RegistrationDriver struct {
	// Type of the authentication used by managedcluster to register as well as pull work from hub. Possible values are csr and awsirsa.
	// +required
	// +kubebuilder:default:=csr
	// +kubebuilder:validation:Enum=csr;awsirsa
	AuthType string `json:"authType,omitempty"`

	// Contain the details required for registering with hub cluster (ie: an EKS cluster) using AWS IAM roles for service account.
	// This is required only when the authType is awsirsa.
	AwsIrsa *AwsIrsa `json:"awsIrsa,omitempty"`
}

type AwsIrsa struct {
	// The arn of the hub cluster (ie: an EKS cluster). This will be required to pass information to hub, which hub will use to create IAM identities for this klusterlet.
	// Example - arn:eks:us-west-2:12345678910:cluster/hub-cluster1.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=`^arn:aws:eks:([a-zA-Z0-9-]+):(\d{12}):cluster/([a-zA-Z0-9-]+)$`
	HubClusterArn string `json:"hubClusterArn"`
	// The arn of the managed cluster (ie: an EKS cluster). This will be required to generate the md5hash which will be used as a suffix to create IAM role on hub
	// as well as used by kluslerlet-agent, to assume role suffixed with the md5hash, on startup.
	// Example - arn:eks:us-west-2:12345678910:cluster/managed-cluster1.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=`^arn:aws:eks:([a-zA-Z0-9-]+):(\d{12}):cluster/([a-zA-Z0-9-]+)$`
	ManagedClusterArn string `json:"managedClusterArn"`
}

type TypeBootstrapKubeConfigs string

const (
	LocalSecrets TypeBootstrapKubeConfigs = "LocalSecrets"
	None         TypeBootstrapKubeConfigs = "None"
)

type BootstrapKubeConfigs struct {
	// Type specifies the type of priority bootstrap kubeconfigs.
	// By default, it is set to None, representing no priority bootstrap kubeconfigs are set.
	// +required
	// +kubebuilder:default:=None
	// +kubebuilder:validation:Enum=None;LocalSecrets
	Type TypeBootstrapKubeConfigs `json:"type,omitempty"`

	// LocalSecretsConfig include a list of secrets that contains the kubeconfigs for ordered bootstrap kubeconifigs.
	// The secrets must be in the same namespace where the agent controller runs.
	// +optional
	LocalSecrets *LocalSecretsConfig `json:"localSecretsConfig,omitempty"`
}

type LocalSecretsConfig struct {
	// KubeConfigSecrets is a list of secret names. The secrets are in the same namespace where the agent controller runs.
	// +required
	// +kubebuilder:validation:minItems=2
	KubeConfigSecrets []KubeConfigSecret `json:"kubeConfigSecrets"`

	// HubConnectionTimeoutSeconds is used to set the timeout of connecting to the hub cluster.
	// When agent loses the connection to the hub over the timeout seconds, the agent do a rebootstrap.
	// By default is 10 mins.
	// +optional
	// +kubebuilder:default:=600
	// +kubebuilder:validation:Minimum=180
	HubConnectionTimeoutSeconds int32 `json:"hubConnectionTimeoutSeconds,omitempty"`
}

type KubeConfigSecret struct {
	// Name is the name of the secret.
	// +required
	Name string `json:"name"`
}

type WorkAgentConfiguration struct {
	// FeatureGates represents the list of feature gates for work
	// If it is set empty, default feature gates will be used.
	// If it is set, featuregate/Foo is an example of one item in FeatureGates:
	//   1. If featuregate/Foo does not exist, registration-operator will discard it
	//   2. If featuregate/Foo exists and is false by default. It is now possible to set featuregate/Foo=[false|true]
	//   3. If featuregate/Foo exists and is true by default. If a cluster-admin upgrading from 1 to 2 wants to continue having featuregate/Foo=false,
	//  	he can set featuregate/Foo=false before upgrading. Let's say the cluster-admin wants featuregate/Foo=false.
	// +optional
	FeatureGates []FeatureGate `json:"featureGates,omitempty"`

	// KubeAPIQPS indicates the maximum QPS while talking with apiserver on the spoke cluster.
	// If it is set empty, use the default value: 50
	// +optional
	// +kubebuilder:default:=50
	KubeAPIQPS int32 `json:"kubeAPIQPS,omitempty"`

	// KubeAPIBurst indicates the maximum burst of the throttle while talking with apiserver on the spoke cluster.
	// If it is set empty, use the default value: 100
	// +optional
	// +kubebuilder:default:=100
	KubeAPIBurst int32 `json:"kubeAPIBurst,omitempty"`

	// HubKubeAPIQPS indicates the maximum QPS while talking with apiserver on the hub cluster.
	// If it is set empty, use the default value: 50
	// +optional
	// +kubebuilder:default:=50
	HubKubeAPIQPS int32 `json:"hubKubeAPIQPS,omitempty"`

	// HubKubeAPIBurst indicates the maximum burst of the throttle while talking with apiserver on the hub cluster.
	// If it is set empty, use the default value: 100
	// +optional
	// +kubebuilder:default:=100
	HubKubeAPIBurst int32 `json:"hubKubeAPIBurst,omitempty"`

	// AppliedManifestWorkEvictionGracePeriod is the eviction grace period the work agent will wait before
	// evicting the AppliedManifestWorks, whose corresponding ManifestWorks are missing on the hub cluster, from
	// the managed cluster. If not present, the default value of the work agent will be used.
	// +optional
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Pattern="^([0-9]+(s|m|h))+$"
	AppliedManifestWorkEvictionGracePeriod *metav1.Duration `json:"appliedManifestWorkEvictionGracePeriod,omitempty"`

	// StatusSyncInterval is the interval for the work agent to check the status of ManifestWorks.
	// Larger value means less frequent status sync and less api calls to the managed cluster, vice versa.
	// The value(x) should be: 5s <= x <= 1h.
	// +optional
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Pattern="^([0-9]+(s|m|h))+$"
	StatusSyncInterval *metav1.Duration `json:"statusSyncInterval,omitempty"`
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

const (
	// The types of klusterlet condition status.
	// ConditionKlusterletApplied is the klusterlet condition status which means all components have been applied on the managed cluster.
	ConditionKlusterletApplied = "Applied"
	// ConditionReadyToApply is a klusterlet condition status which means it is ready to apply the resources on the managed cluster.
	ConditionReadyToApply = "ReadyToApply"
	// ConditionKlusterletAvailable is the klusterlet condition status which means all components are available and ready to serve.
	ConditionKlusterletAvailable = "Available"
	// ConditionHubConnectionDegraded is the klusterlet condition status which means the agent on the managed cluster cannot access the hub cluster.
	ConditionHubConnectionDegraded = "HubConnectionDegraded"
	// ConditionRegistrationDesiredDegraded is the klusterlet condition status which means the registration agent on the managed cluster is not ready to serve.
	ConditionRegistrationDesiredDegraded = "RegistrationDesiredDegraded"
	// ConditionWorkDesiredDegraded is the klusterlet condition status which means the work agent on the managed cluster is not ready to serve.
	ConditionWorkDesiredDegraded = "WorkDesiredDegraded"

	// ReasonKlusterletApplied is the reason of ConditionKlusterletApplied condition to show resources are applied.
	ReasonKlusterletApplied = "KlusterletApplied"
	// ReasonKlusterletApplyFailed is the reason of ConditionKlusterletApplied condition to show it is failed to apply resources.
	ReasonKlusterletApplyFailed = "KlusterletApplyFailed"
	// ReasonKlusterletCRDApplyFailed is the reason of ConditionKlusterletApplied condition to show it is failed to apply CRDs.
	ReasonKlusterletCRDApplyFailed = "CRDApplyFailed"
	// ReasonManagedClusterResourceApplyFailed is the reason of ConditionKlusterletApplied condition to show it is failed to apply resources on managed cluster.
	ReasonManagedClusterResourceApplyFailed = "ManagedClusterResourceApplyFailed"
	// ReasonManagementClusterResourceApplyFailed is the reason of ConditionKlusterletApplied condition to show it is failed to apply resources on management cluster.
	ReasonManagementClusterResourceApplyFailed = "ManagementClusterResourceApplyFailed"

	// ReasonKlusterletPrepareFailed is the reason of ConditionReadyToApply condition to show it is failed to get the kubeConfig
	// of managed cluster from the external-managed-kubeconfig secret in the hosted mode.
	ReasonKlusterletPrepareFailed = "KlusterletPrepareFailed"
	// ReasonKlusterletPrepared is the reason of ConditionReadyToApply condition to show the kubeConfig of managed cluster is
	// validated from the external-managed-kubeconfig secret in the hosted mode.
	ReasonKlusterletPrepared = "KlusterletPrepared"

	// ReasonKlusterletGetDeploymentFailed is the reason of ConditionKlusterletAvailable/ConditionRegistrationDesiredDegraded/ConditionWorkDesiredDegraded
	// condition to show it is failed to get deployments.
	ReasonKlusterletGetDeploymentFailed = "GetDeploymentFailed"
	// ReasonKlusterletUnavailablePods is the reason of ConditionKlusterletAvailable/ConditionRegistrationDesiredDegraded/ConditionWorkDesiredDegraded
	// condition to show there is unavailable pod.
	ReasonKlusterletUnavailablePods = "UnavailablePods"
	// ReasonKlusterletDeploymentsFunctional is the reason of ConditionKlusterletAvailable/ConditionRegistrationDesiredDegraded/ConditionWorkDesiredDegraded
	// condition to show all deployments are functional.
	ReasonKlusterletDeploymentsFunctional = "DeploymentsFunctional"
	// ReasonKlusterletNoAvailablePods is the reason of ConditionKlusterletAvailable/ConditionRegistrationDesiredDegraded/ConditionWorkDesiredDegraded
	// condition to show there is no available pod.
	ReasonKlusterletNoAvailablePods = "NoAvailablePods"

	// ReasonKlusterletAvailable is the reason of ConditionKlusterletAvailable condition to show all deployed resources are available.
	ReasonKlusterletAvailable = "KlusterletAvailable"

	// ReasonHubConnectionFunctional is the reason of ConditionHubConnectionDegraded condition to show spoke cluster connects hub cluster.
	ReasonHubConnectionFunctional = "HubConnectionFunctional"
	// ReasonHubKubeConfigSecretMissing is the reason of ConditionHubConnectionDegraded condition to show hubKubeConfigSecret is missing.
	ReasonHubKubeConfigSecretMissing = "HubKubeConfigSecretMissing"
	// ReasonHubKubeConfigMissing is the reason of ConditionHubConnectionDegraded condition to show hubKubeConfig in hubKubeConfigSecret is missing.
	ReasonHubKubeConfigMissing = "HubKubeConfigMissing"
	// ReasonHubKubeConfigError is the reason of ConditionHubConnectionDegraded condition to show it is failed to get hubKubeConfig.
	ReasonHubKubeConfigError = "HubKubeConfigError"
	// ReasonClusterNameMissing is the reason of ConditionHubConnectionDegraded condition to show the cluster-name is missing in the hubKubeConfigSecret.
	ReasonClusterNameMissing = "ClusterNameMissing"
	// ReasonHubKubeConfigUnauthorized is the reason of ConditionHubConnectionDegraded condition to show there is no permission to access hub using the hubKubeConfigSecret.
	ReasonHubKubeConfigUnauthorized = "HubKubeConfigUnauthorized"
)
