// Copyright Contributors to the Open Cluster Management project
package v1

import corev1 "k8s.io/api/core/v1"

type ResourceRequirementAcquirer interface {
	GetResourceRequirement() *ResourceRequirement
}

// ResourceRequirement allow user override the default pod QoS classes
type ResourceRequirement struct {
	// +kubebuilder:validation:Enum=Default;BestEffort;ResourceRequirement
	// +kubebuilder:default:=Default
	Type ResourceQosClass `json:"type"`
	// ResourceRequirements defines resource requests and limits when Type is ResourceQosClassResourceRequirement
	// +optional
	ResourceRequirements *corev1.ResourceRequirements `json:"resourceRequirements,omitempty"`
}

type ResourceQosClass string

const (
	// Default use resource setting in the template file (with requests but no limits in the resources)
	ResourceQosClassDefault ResourceQosClass = "Default"
	// If all containers in the pod don't set resource request and limits, the pod is treated as BestEffort.
	ResourceQosClassBestEffort ResourceQosClass = "BestEffort"
	// Configurable resource requirements with requests and limits
	ResourceQosClassResourceRequirement ResourceQosClass = "ResourceRequirement"
)
