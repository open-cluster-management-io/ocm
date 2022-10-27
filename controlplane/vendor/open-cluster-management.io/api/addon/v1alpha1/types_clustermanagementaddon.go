package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope="Cluster"
// +kubebuilder:printcolumn:name="DISPLAY NAME",type=string,JSONPath=`.spec.addOnMeta.displayName`
// +kubebuilder:printcolumn:name="CRD NAME",type=string,JSONPath=`.spec.addOnConfiguration.crdName`

// ClusterManagementAddOn represents the registration of an add-on to the cluster manager.
// This resource allows the user to discover which add-on is available for the cluster manager and
// also provides metadata information about the add-on.
// This resource also provides a linkage to ManagedClusterAddOn, the name of the ClusterManagementAddOn
// resource will be used for the namespace-scoped ManagedClusterAddOn resource.
// ClusterManagementAddOn is a cluster-scoped resource.
type ClusterManagementAddOn struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec represents a desired configuration for the agent on the cluster management add-on.
	// +required
	Spec ClusterManagementAddOnSpec `json:"spec"`

	// status represents the current status of cluster management add-on.
	// +optional
	Status ClusterManagementAddOnStatus `json:"status,omitempty"`
}

// ClusterManagementAddOnSpec provides information for the add-on.
type ClusterManagementAddOnSpec struct {
	// addOnMeta is a reference to the metadata information for the add-on.
	// +optional
	AddOnMeta AddOnMeta `json:"addOnMeta"`

	// addOnConfiguration is a reference to configuration information for the add-on.
	// In scenario where a multiple add-ons share the same add-on CRD,
	// multiple ClusterManagementAddOn resources need to be created and reference the same AddOnConfiguration.
	// +optional
	AddOnConfiguration ConfigCoordinates `json:"addOnConfiguration"`
}

// AddOnMeta represents a collection of metadata information for the add-on.
type AddOnMeta struct {
	// displayName represents the name of add-on that will be displayed.
	// +optional
	DisplayName string `json:"displayName"`

	// description represents the detailed description of the add-on.
	// +optional
	Description string `json:"description"`
}

// ConfigCoordinates represents the information for locating the CRD and CR that configures the add-on.
type ConfigCoordinates struct {
	// crdName is the name of the CRD used to configure instances of the managed add-on.
	// This field should be configured if the add-on have a CRD that controls the configuration of the add-on.
	// +optional
	CRDName string `json:"crdName"`

	// crName is the name of the CR used to configure instances of the managed add-on.
	// This field should be configured if add-on CR have a consistent name across the all of the ManagedCluster instaces.
	// +optional
	CRName string `json:"crName"`

	// lastObservedGeneration is the observed generation of the custom resource for the configuration of the addon.
	// +optional
	LastObservedGeneration int64 `json:"lastObservedGeneration,omitempty"`
}

// ClusterManagementAddOnStatus represents the current status of cluster management add-on.
type ClusterManagementAddOnStatus struct {
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// ClusterManagementAddOnList is a collection of cluster management add-ons.
type ClusterManagementAddOnList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata.
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#types-kinds
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	// Items is a list of cluster management add-ons.
	Items []ClusterManagementAddOn `json:"items"`
}
