package chart

import (
	"embed"

	corev1 "k8s.io/api/core/v1"

	operatorv1 "open-cluster-management.io/api/operator/v1"
)

//go:embed cluster-manager
//go:embed cluster-manager/crds
//go:embed cluster-manager/templates
//go:embed cluster-manager/templates/_helpers.tpl
var ChartFiles embed.FS

const ChartName = "cluster-manager"

type ChartConfig struct {
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
	NodeSelector corev1.NodeSelector `json:"nodeSelector,omitempty"`
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

type ClusterManagerConfig struct {
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
