package chart

import (
	"embed"

	corev1 "k8s.io/api/core/v1"

	operatorv1 "open-cluster-management.io/api/operator/v1"
)

//go:embed klusterlet
//go:embed klusterlet/crds
//go:embed klusterlet/templates
//go:embed klusterlet/templates/_helpers.tpl
var ChartFiles embed.FS

const ChartName = "klusterlet"

type ChartConfig struct {
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
	NodeSelector corev1.NodeSelector `json:"nodeSelector,omitempty"`
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

	// ExternalManagedKubeConfig should be the kubeConfig file of the managed cluster via setting --set-file=<the kubeConfig file of managed cluster>
	// only need to set in the hosted mode. optional
	ExternalManagedKubeConfig string `json:"externalManagedKubeConfig,omitempty"`

	// NoOperator is to only deploy the klusterlet CR if set true.
	NoOperator bool `json:"noOperator,omitempty"`
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
}

type ImageCredentials struct {
	CreateImageCredentials bool   `json:"createImageCredentials,omitempty"`
	UserName               string `json:"userName,omitempty"`
	Password               string `json:"password,omitempty"`
}

type KlusterletConfig struct {
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
	ResourceRequirement operatorv1.ResourceRequirement `json:"resourceRequirement,omitempty"`
}
