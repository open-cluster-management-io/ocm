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
	// +kubebuilder:default={workDriver: kube}
	WorkConfiguration *WorkConfiguration `json:"workConfiguration,omitempty"`

	// AddOnManagerConfiguration contains the configuration of addon manager
	// +optional
	AddOnManagerConfiguration *AddOnManagerConfiguration `json:"addOnManagerConfiguration,omitempty"`

	// ResourceRequirement specify QoS classes of deployments managed by clustermanager.
	// It applies to all the containers in the deployments.
	// +optional
	ResourceRequirement *ResourceRequirement `json:"resourceRequirement,omitempty"`
}

// NodePlacement describes node scheduling configuration for the pods.
type NodePlacement struct {
	// NodeSelector defines which Nodes the Pods are scheduled on. The default is an empty list.
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Tolerations are attached by pods to tolerate any taint that matches
	// the triple <key,value,effect> using the matching operator <operator>.
	// The default is an empty list.
	// +optional
	Tolerations []v1.Toleration `json:"tolerations,omitempty"`
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

	// RegistrationDrivers represent the list of hub registration drivers that contain information used by hub to initialize the hub cluster
	// A RegistrationDriverHub contains details of authentication type and the hub cluster ARN
	// +optional
	// +listType=map
	// +listMapKey=authType
	RegistrationDrivers []RegistrationDriverHub `json:"registrationDrivers,omitempty"`
}

type RegistrationDriverHub struct {

	// Type of the authentication used by hub to initialize the Hub cluster. Possible values are csr and awsirsa.
	// +required
	// +kubebuilder:default:=csr
	// +kubebuilder:validation:Enum=csr;awsirsa
	AuthType string `json:"authType,omitempty"`

	// CSR represents the configuration for csr driver.
	// +optional
	CSR *CSRConfig `json:"csr,omitempty"`

	// AwsIrsa represents the configuration for awsisra driver.
	// +optional
	AwsIrsa *AwsIrsaConfig `json:"awsisra,omitempty"`
}

type CSRConfig struct {
	// AutoApprovedIdentities represent a list of approved users
	// +optional
	AutoApprovedIdentities []string `json:"autoApprovedIdentities,omitempty"`
}

type AwsIrsaConfig struct {
	// This represents the hub cluster ARN
	// Example - arn:eks:us-west-2:12345678910:cluster/hub-cluster1
	// +optional
	// +kubebuilder:validation:Pattern=`^arn:aws:eks:([a-zA-Z0-9-]+):(\d{12}):cluster/([a-zA-Z0-9-]+)$`
	HubClusterArn string `json:"hubClusterArn,omitempty"`

	// AutoApprovedIdentities represent a list of approved arn patterns
	// +optional
	AutoApprovedIdentities []string `json:"autoApprovedIdentities,omitempty"`

	// List of tags to be added to AWS resources created by hub while processing awsirsa registration request
	// Example - "product:v1:tenant:app-name=My-App"
	// +optional
	Tags []string `json:"tags,omitempty"`
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

	// WorkDriver represents the type of work driver. Possible values are "kube", "mqtt", or "grpc".
	// If not provided, the default value is "kube".
	// If set to non-"kube" drivers, the klusterlet need to use the same driver.
	// and the driver configuration must be provided in a secret named "work-driver-config"
	// in the namespace where the cluster manager is running, adhering to the following structure:
	// config.yaml: |
	//   <driver-config-in-yaml>
	//
	// For detailed driver configuration, please refer to the sdk-go documentation: https://github.com/open-cluster-management-io/sdk-go/blob/main/pkg/cloudevents/README.md#supported-protocols-and-drivers
	//
	// +optional
	// +kubebuilder:default:=kube
	// +kubebuilder:validation:Enum=kube;mqtt;grpc
	WorkDriver WorkDriverType `json:"workDriver,omitempty"`
}

// WorkDriverType represents the type of work driver.
type WorkDriverType string

const (
	// WorkDriverTypeKube is the work driver type for kube.
	WorkDriverTypeKube WorkDriverType = "kube"
	// WorkDriverTypeMqtt is the work driver type for mqtt.
	WorkDriverTypeMqtt WorkDriverType = "mqtt"
	// WorkDriverTypeGrpc is the work driver type for grpc.
	WorkDriverTypeGrpc WorkDriverType = "grpc"
)

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
	// FeatureGateModeTypeEnable is the feature gate type to enable a feature.
	FeatureGateModeTypeEnable FeatureGateModeType = "Enable"
	// FeatureGateModeTypeDisable is the feature gate type to disable a feature.
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
	// +kubebuilder:default=443
	// +kubebuilder:validation:Maximum=65535
	Port int32 `json:"port,omitempty"`
}

// ClusterManagerDeployOption describes the deployment options for cluster-manager
type ClusterManagerDeployOption struct {
	// Mode can be Default or Hosted.
	// In Default mode, the Hub is installed as a whole and all parts of Hub are deployed in the same cluster.
	// In Hosted mode, only crd and configurations are installed on one cluster(defined as hub-cluster). Controllers run in another
	// cluster (defined as management-cluster) and connect to the hub with the kubeconfig in secret of "external-hub-kubeconfig"(a kubeconfig
	// of hub-cluster with cluster-admin permission).
	// Note: Do not modify the Mode field once it's applied.
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:default=Default
	// +kubebuilder:validation:Enum=Default;Hosted
	Mode InstallMode `json:"mode,omitempty"`

	// Hosted includes configurations we need for clustermanager in the Hosted mode.
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
	// The cluster-manager will be deployed outside the hub-cluster, the klusterlet will be deployed outside the managed-cluster.
	InstallModeHosted InstallMode = "Hosted"

	// InstallModeSingleton means deploying components as a single controller.
	InstallModeSingleton InstallMode = "Singleton"

	// InstallModeSingleton means deploying components as a single controller in hosted mode.
	InstallModeSingletonHosted InstallMode = "SingletonHosted"
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

const (
	// The types of ClusterManager condition status.
	// ConditionClusterManagerApplied is the ClusterManager condition status which means all components have been applied on the hub.
	ConditionClusterManagerApplied = "Applied"
	// ConditionHubRegistrationDegraded is the ClusterManager condition status which means the registration is not ready to serve on the hub.
	ConditionHubRegistrationDegraded = "HubRegistrationDegraded"
	// ConditionHubPlacementDegraded is the ClusterManager condition status which means the placement is not ready to serve on the hub.
	ConditionHubPlacementDegraded = "HubPlacementDegraded"
	// ConditionProgressing is the ClusterManager condition status which means the ClusterManager are in upgrading phase.
	ConditionProgressing = "Progressing"
	// ConditionMigrationSucceeded is the ClusterManager condition status which means the API migration is succeeded on the hub.
	ConditionMigrationSucceeded = "MigrationSucceeded"

	// ReasonClusterManagerApplied is the reason of the ConditionClusterManagerApplied condition to show all resources are applied.
	ReasonClusterManagerApplied = "ClusterManagerApplied"
	// ReasonRuntimeResourceApplyFailed is the reason of the ConditionClusterManagerApplied condition to show it is failed to apply deployments.
	ReasonRuntimeResourceApplyFailed = "RuntimeResourceApplyFailed"
	// ReasonServiceAccountSyncFailed is the reason of the ConditionClusterManagerApplied condition to show it is failed to apply serviceAccounts.
	ReasonServiceAccountSyncFailed = "ServiceAccountSyncFailed"
	// ReasonClusterManagerCRDApplyFailed is the reason of the ConditionClusterManagerApplied condition to show it is failed to apply CRDs.
	ReasonClusterManagerCRDApplyFailed = "CRDApplyFailed"
	// ReasonWebhookApplyFailed is the reason of the ConditionClusterManagerApplied condition to show it is failed to apply webhooks.
	ReasonWebhookApplyFailed = "WebhookApplyFailed"

	// ReasonDeploymentRolling is the reason of the ConditionProgressing condition to show the deployed deployments are rolling.
	ReasonDeploymentRolling = "ClusterManagerDeploymentRolling"
	// ReasonUpToDate is the reason of the ConditionProgressing condition to show the deployed deployments are up-to-date.
	ReasonUpToDate = "ClusterManagerUpToDate"

	// ReasonStorageVersionMigrationFailed is the reason of the ConditionMigrationSucceeded condition to show the API storageVersion migration is failed.
	ReasonStorageVersionMigrationFailed = "StorageVersionMigrationFailed"
	// ReasonStorageVersionMigrationProcessing is the reason of the ConditionMigrationSucceeded condition to show the API storageVersion migration is not completed.
	ReasonStorageVersionMigrationProcessing = "StorageVersionMigrationProcessing"
	// ReasonStorageVersionMigrationSucceed is the reason of the ConditionMigrationSucceeded condition to show the API storageVersion migration is succeeded.
	ReasonStorageVersionMigrationSucceed = "StorageVersionMigrationSucceed"

	// ReasonGetRegistrationDeploymentFailed is the reason of the ConditionRegistrationDegraded condition to show getting registration deployment failed.
	ReasonGetRegistrationDeploymentFailed = "GetRegistrationDeploymentFailed"
	// ReasonUnavailableRegistrationPod is the reason of the ConditionRegistrationDegraded condition to show the registration pods are unavailable.
	ReasonUnavailableRegistrationPod = "UnavailableRegistrationPod"
	// ReasonRegistrationFunctional is the reason of the ConditionRegistrationDegraded condition to show registration is functional.
	ReasonRegistrationFunctional = "RegistrationFunctional"

	// ReasonGetPlacementDeploymentFailed is the reason of the ConditionPlacementDegraded condition to show it is failed get placement deployment.
	ReasonGetPlacementDeploymentFailed = "GetPlacementDeploymentFailed"
	// ReasonUnavailablePlacementPod is the reason of the ConditionPlacementDegraded condition to show  the registration pods are unavailable.
	ReasonUnavailablePlacementPod = "UnavailablePlacementPod"
	// ReasonPlacementFunctional is the reason of the ConditionPlacementDegraded condition to show placement is functional.
	ReasonPlacementFunctional = "PlacementFunctional"
)
