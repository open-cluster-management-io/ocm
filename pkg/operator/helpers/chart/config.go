package chart

import (
	corev1 "k8s.io/api/core/v1"

	operatorv1 "open-cluster-management.io/api/operator/v1"
)

type ClusterManagerChartConfig struct {
	// CreateNamespace is used in the render function to append the release ns in the objects.
	CreateNamespace bool `json:"createNamespace,omitempty"`
	// ReplicaCount is the replicas for the clusterManager operator deployment.
	ReplicaCount int `json:"replicaCount,omitempty"`
	// Images is the configurations for all images used in operator deployment and clusterManager CR.
	Images ImagesConfig `json:"images,omitempty"`
	// PodSecurityContext is the pod SecurityContext in the operator deployment
	PodSecurityContext corev1.PodSecurityContext `json:"podSecurityContext,omitempty"`
	// SecurityContext is the container SecurityContext in operator deployment
	SecurityContext corev1.SecurityContext `json:"securityContext,omitempty"`
	// Resources is the resource requirements of the operator deployment
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
	// NodeSelector is the nodeSelector of the operator deployment
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
	// Tolerations is the tolerations of the operator deployment
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
	// Affinity is the affinity of the operator deployment
	Affinity corev1.Affinity `json:"affinity,omitempty"`
	// CreateBootstrapToken is to enable/disable the bootstrap token secret for auto approve.
	CreateBootstrapToken bool `json:"createBootstrapToken,omitempty"`
	// CreateBootstrapSA is to create a serviceAccount to generate token.
	CreateBootstrapSA bool `json:"createBootstrapSA,omitempty"`
	// ClusterManager is the configuration of clusterManager CR
	ClusterManager ClusterManagerConfig `json:"clusterManager,omitempty"`
	// EnableSyncLabels is to enable the feature which can sync the labels from clustermanager to all hub resources.
	EnableSyncLabels bool `json:"enableSyncLabels,omitempty"`
}

type KlusterletChartConfig struct {
	// CreateNamespace is used in the render function to append the release ns in the objects.
	CreateNamespace bool `json:"createNamespace,omitempty"`
	// ReplicaCount is the replicas for the klusterlet operator deployment.
	ReplicaCount int `json:"replicaCount,omitempty"`
	// Images is the configurations for all images used in operator deployment and klusterlet CR.
	Images ImagesConfig `json:"images,omitempty"`
	// PodSecurityContext is the pod SecurityContext in the operator deployment
	PodSecurityContext corev1.PodSecurityContext `json:"podSecurityContext,omitempty"`
	// SecurityContext is the container SecurityContext in operator deployment
	SecurityContext corev1.SecurityContext `json:"securityContext,omitempty"`
	// Resources is the resource requirements of the operator deployment
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
	// NodeSelector is the nodeSelector of the operator deployment
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
	// Tolerations is the tolerations of the operator deployment
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
	// Affinity is the affinity of the operator deployment
	Affinity corev1.Affinity `json:"affinity,omitempty"`
	// Klusterlet is the configuration of klusterlet CR
	Klusterlet KlusterletConfig `json:"klusterlet,omitempty"`
	// PriorityClassName is the name of the PriorityClass that will be used by the deployed klusterlet agent and operator.
	PriorityClassName string `json:"priorityClassName,omitempty"`

	// EnableSyncLabels is to enable the feature which can sync the labels from klusterlet to all agent resources.
	EnableSyncLabels bool `json:"enableSyncLabels,omitempty"`

	// BootstrapHubKubeConfig should be the kubeConfig file of the hub cluster via setting --set-file=<the kubeConfig file of hub cluster> optional
	BootstrapHubKubeConfig string `json:"bootstrapHubKubeConfig,omitempty"`

	// when MultipleHubs feature gate in klusterlet.registrationConfiguration is enabled, need to set multiple bootstrap hub kubeConfigs here.
	MultiHubBootstrapHubKubeConfigs []BootStrapKubeConfig `json:"multiHubBootstrapHubKubeConfigs,omitempty"`

	// ExternalManagedKubeConfig should be the kubeConfig file of the managed cluster via setting --set-file=<the kubeConfig file of managed cluster>
	// only need to set in the hosted mode. optional
	ExternalManagedKubeConfig string `json:"externalManagedKubeConfig,omitempty"`

	// NoOperator is to only deploy the klusterlet CR if set true.
	NoOperator bool `json:"noOperator,omitempty"`
}

type BootStrapKubeConfig struct {
	// the boostStrap secret name
	Name string `json:"name,omitempty"`
	// the kubeConfig file of the hub cluster
	KubeConfig string `json:"kubeConfig,omitempty"`
}

type ImagesConfig struct {
	// Registry is registry name must NOT contain a trailing slash.
	Registry string `json:"registry,omitempty"`
	// Tag is the operator image tag.
	Tag string `json:"tag,omitempty"`
	// ImagePullPolicy is the image pull policy of operator image. Default is IfNotPresent.
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`
	// The image pull secret name is open-cluster-management-image-pull-credentials.
	// Please set the userName and password if you use a private image registry.
	ImageCredentials ImageCredentials `json:"imageCredentials,omitempty"`
	// Overrides is to override the image of the component, if this is specified,
	// the registry and tag will be ignored.
	Overrides Overrides `json:"overrides,omitempty"`
}

type ImageCredentials struct {
	CreateImageCredentials bool   `json:"createImageCredentials,omitempty"`
	UserName               string `json:"userName,omitempty"`
	Password               string `json:"password,omitempty"`
	DockerConfigJson       string `json:"dockerConfigJson,omitempty"`
}

type Overrides struct {
	// RegistrationImage is the image of the registration component.
	RegistrationImage string `json:"registrationImage,omitempty"`
	// WorkImage is the image of the work component.
	WorkImage string `json:"workImage,omitempty"`
	// OperatorImage is the image of the operator component.
	OperatorImage string `json:"operatorImage,omitempty"`
	// PlacementImage is the image of the placement component
	PlacementImage string `json:"placementImage,omitempty"`
	// AddOnManagerImage is the image of the addOnManager component
	AddOnManagerImage string `json:"addOnManagerImage,omitempty"`
}

type ClusterManagerConfig struct {
	// Create determines if create the clusterManager CR, default is true.
	Create bool `json:"create,omitempty"`
	// InstallMode represents the mode of deploy cluster-manager
	Mode operatorv1.InstallMode `json:"mode,omitempty"`

	// RegistrationConfiguration contains the configuration of registration
	// +optional
	RegistrationConfiguration operatorv1.RegistrationHubConfiguration `json:"registrationConfiguration,omitempty"`

	// WorkConfiguration contains the configuration of work
	// +optional
	WorkConfiguration operatorv1.WorkConfiguration `json:"workConfiguration,omitempty"`

	// AddOnManagerConfiguration contains the configuration of addon manager
	// +optional
	AddOnManagerConfiguration operatorv1.AddOnManagerConfiguration `json:"addOnManagerConfiguration,omitempty"`

	// ResourceRequirement specify QoS classes of deployments managed by clustermanager.
	// It applies to all the containers in the deployments.
	// +optional
	ResourceRequirement operatorv1.ResourceRequirement `json:"resourceRequirement,omitempty"`
}

type KlusterletConfig struct {
	// Create determines if create the klusterlet CR, default is true.
	Create bool `json:"create,omitempty"`
	// InstallMode represents the mode of deploy klusterlet
	Mode        operatorv1.InstallMode `json:"mode,omitempty"`
	Name        string                 `json:"name,omitempty"`
	ClusterName string                 `json:"clusterName,omitempty"`
	Namespace   string                 `json:"namespace,omitempty"`
	// ExternalServerURLs represents a list of apiserver urls and ca bundles that is accessible externally
	// If it is set empty, managed cluster has no externally accessible url that hub cluster can visit.
	// +optional
	ExternalServerURLs []operatorv1.ServerURL `json:"externalServerURLs,omitempty"`

	// NodePlacement enables explicit control over the scheduling of the deployed pods.
	// +optional
	NodePlacement operatorv1.NodePlacement `json:"nodePlacement,omitempty"`

	// RegistrationConfiguration contains the configuration of registration
	// +optional
	RegistrationConfiguration operatorv1.RegistrationConfiguration `json:"registrationConfiguration,omitempty"`

	// WorkConfiguration contains the configuration of work
	// +optional
	WorkConfiguration operatorv1.WorkAgentConfiguration `json:"workConfiguration,omitempty"`

	// ResourceRequirement specify QoS classes of deployments managed by clustermanager.
	// It applies to all the containers in the deployments.
	// +optional
	ResourceRequirement *operatorv1.ResourceRequirement `json:"resourceRequirement,omitempty"`
}
