// Copyright Contributors to the Open Cluster Management project
package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
)

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope="Cluster",shortName={"cma","cmas"}
// +kubebuilder:printcolumn:name="DISPLAY NAME",type=string,JSONPath=`.spec.addOnMeta.displayName`

// ClusterManagementAddOn represents the registration of an add-on to the cluster manager.
// This resource allows you to discover which add-ons are available for the cluster manager
// and provides metadata information about the add-ons. The ClusterManagementAddOn name is used
// for the namespace-scoped ManagedClusterAddOn resource.
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
	AddOnMeta AddOnMeta `json:"addOnMeta,omitempty"`

	// defaultConfigs is a list of default configuration types supported by add-on.
	// +optional
	// +listType=map
	// +listMapKey=group
	// +listMapKey=resource
	DefaultConfigs []AddOnConfig `json:"defaultConfigs,omitempty"`

	// installStrategy represents that related ManagedClusterAddOns should be installed
	// on certain clusters.
	// +optional
	// +kubebuilder:default={type: Manual}
	InstallStrategy InstallStrategy `json:"installStrategy,omitempty"`
}

// AddOnMeta represents a collection of metadata information for the add-on.
type AddOnMeta struct {
	// displayName represents the name of add-on that will be displayed.
	// +optional
	DisplayName string `json:"displayName,omitempty"`

	// description represents the detailed description of the add-on.
	// +optional
	Description string `json:"description,omitempty"`
}

// ConfigGroupResource represents the GroupResource of the add-on configuration
type ConfigGroupResource struct {
	// group of the add-on configuration.
	// +optional
	// +kubebuilder:default=""
	Group string `json:"group"`

	// resource of the add-on configuration.
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Resource string `json:"resource"`
}

// ConfigReferent represents the namespace and name for an add-on configuration.
type ConfigReferent struct {
	// namespace of the add-on configuration.
	// If this field is not set, the configuration is in the cluster scope.
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// name of the add-on configuration.
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// ConfigSpecHash represents the namespace,name and spec hash for an add-on configuration.
type ConfigSpecHash struct {
	// namespace and name for an add-on configuration.
	ConfigReferent `json:",inline"`

	// spec hash for an add-on configuration.
	SpecHash string `json:"specHash"`
}

// InstallStrategy represents that related ManagedClusterAddOns should be installed
// on certain clusters.
type InstallStrategy struct {
	// type is the type of the install strategy, it can be:
	// - Manual: no automatic install
	// - Placements: install to clusters selected by placements.
	// +kubebuilder:validation:Enum=Manual;Placements
	// +kubebuilder:default:=Manual
	// +optional
	Type string `json:"type"`
	// placements is a list of placement references honored when install strategy type is
	// Placements. All clusters selected by these placements will install the addon
	// If one cluster belongs to multiple placements, it will only apply the strategy defined
	// later in the order. That is to say, The latter strategy overrides the previous one.
	// +optional
	// +listType=map
	// +listMapKey=namespace
	// +listMapKey=name
	Placements []PlacementStrategy `json:"placements,omitempty"`
}

const (
	// AddonInstallStrategyManual is the addon install strategy representing no automatic addon installation
	AddonInstallStrategyManual string = "Manual"
	// AddonInstallStrategyPlacements is the addon install strategy representing the addon installation
	// is based on placement decisions.
	AddonInstallStrategyPlacements string = "Placements"
)

type PlacementRef struct {
	// namespace is the namespace of the placement
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Namespace string `json:"namespace"`
	// name is the name of the placement
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

type PlacementStrategy struct {
	PlacementRef `json:",inline"`
	// configs is the configuration of managedClusterAddon during installation.
	// User can override the configuration by updating the managedClusterAddon directly.
	// +optional
	Configs []AddOnConfig `json:"configs,omitempty"`
	// rolloutStrategy is the strategy to apply addon configurations change.
	// The rollout strategy only watches the addon configurations defined in ClusterManagementAddOn.
	// +kubebuilder:default={type: All}
	// +optional
	RolloutStrategy clusterv1alpha1.RolloutStrategy `json:"rolloutStrategy,omitempty"`
}

// ClusterManagementAddOnStatus represents the current status of cluster management add-on.
type ClusterManagementAddOnStatus struct {
	// defaultConfigReferences is a list of current add-on default configuration references.
	// +optional
	DefaultConfigReferences []DefaultConfigReference `json:"defaultConfigReferences,omitempty"`
	// installProgressions is a list of current add-on configuration references per placement.
	// +optional
	InstallProgressions []InstallProgression `json:"installProgressions,omitempty"`
}

type InstallProgression struct {
	PlacementRef `json:",inline"`

	// configReferences is a list of current add-on configuration references.
	// +optional
	ConfigReferences []InstallConfigReference `json:"configReferences,omitempty"`

	// conditions describe the state of the managed and monitored components for the operator.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"  patchStrategy:"merge" patchMergeKey:"type"`
}

// DefaultConfigReference is a reference to the current add-on configuration.
// This resource is used to record the configuration resource for the current add-on.
type DefaultConfigReference struct {
	// This field is synced from ClusterManagementAddOn Configurations.
	ConfigGroupResource `json:",inline"`

	// desiredConfig record the desired config spec hash.
	DesiredConfig *ConfigSpecHash `json:"desiredConfig"`
}

// InstallConfigReference is a reference to the current add-on configuration.
// This resource is used to record the configuration resource for the current add-on.
type InstallConfigReference struct {
	// This field is synced from ClusterManagementAddOn Configurations.
	ConfigGroupResource `json:",inline"`

	// desiredConfig record the desired config name and spec hash.
	DesiredConfig *ConfigSpecHash `json:"desiredConfig"`

	// lastKnownGoodConfig records the last known good config spec hash.
	// For fresh install or rollout with type UpdateAll or RollingUpdate, the
	// lastKnownGoodConfig is the same as lastAppliedConfig.
	// For rollout with type RollingUpdateWithCanary, the lastKnownGoodConfig
	// is the last successfully applied config spec hash of the canary placement.
	LastKnownGoodConfig *ConfigSpecHash `json:"lastKnownGoodConfig"`

	// lastAppliedConfig records the config spec hash when the all the corresponding
	// ManagedClusterAddOn are applied successfully.
	LastAppliedConfig *ConfigSpecHash `json:"lastAppliedConfig"`
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
