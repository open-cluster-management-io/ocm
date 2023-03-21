package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// AddOnDeploymentConfig represents a deployment configuration for an add-on.
type AddOnDeploymentConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec represents a desired configuration for an add-on.
	// +required
	Spec AddOnDeploymentConfigSpec `json:"spec"`
}

type AddOnDeploymentConfigSpec struct {
	// CustomizedVariables is a list of name-value variables for the current add-on deployment.
	// The add-on implementation can use these variables to render its add-on deployment.
	// The default is an empty list.
	// +optional
	// +listType=map
	// +listMapKey=name
	CustomizedVariables []CustomizedVariable `json:"customizedVariables,omitempty"`

	// NodePlacement enables explicit control over the scheduling of the add-on agents on the
	// managed cluster.
	// All add-on agent pods are expected to comply with this node placement.
	// If the placement is nil, the placement is not specified, it will be omitted.
	// If the placement is an empty object, the placement will match all nodes and tolerate nothing.
	// +optional
	NodePlacement *NodePlacement `json:"nodePlacement,omitempty"`
}

// CustomizedVariable represents a customized variable for add-on deployment.
type CustomizedVariable struct {
	// Name of this variable.
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MaxLength=255
	// +kubebuilder:validation:Pattern=^[a-zA-Z_][_a-zA-Z0-9]*$
	Name string `json:"name"`

	// Value of this variable.
	// +kubebuilder:validation:MaxLength=1024
	Value string `json:"value"`
}

// NodePlacement describes node scheduling configuration for the pods.
type NodePlacement struct {
	// NodeSelector defines which Nodes the Pods are scheduled on.
	// If the selector is an empty list, it will match all nodes.
	// The default is an empty list.
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Tolerations is attached by pods to tolerate any taint that matches
	// the triple <key,value,effect> using the matching operator <operator>.
	// If the tolerations is an empty list, it will tolerate nothing.
	// The default is an empty list.
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// AddOnDeploymentConfigList is a collection of add-on deployment config.
type AddOnDeploymentConfigList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata.
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#types-kinds
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	// Items is a list of add-on deployment config.
	Items []AddOnDeploymentConfig `json:"items"`
}
