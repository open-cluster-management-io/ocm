package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Available",type=string,JSONPath=`.status.conditions[?(@.type=="Available")].status`
// +kubebuilder:printcolumn:name="Degraded",type=string,JSONPath=`.status.conditions[?(@.type=="Degraded")].status`
// +kubebuilder:printcolumn:name="Progressing",type=string,JSONPath=`.status.conditions[?(@.type=="Progressing")].status`

// ManagedClusterAddOn is the Custom Resource object which holds the current state
// of an add-on. This object is used by add-on operators to convey their state.
// This resource should be created in the ManagedCluster namespace.
type ManagedClusterAddOn struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	// spec holds configuration that could apply to any operator.
	// +kubebuilder:validation:Required
	// +required
	Spec ManagedClusterAddOnSpec `json:"spec"`

	// status holds the information about the state of an operator.  It is consistent with status information across
	// the Kubernetes ecosystem.
	// +optional
	Status ManagedClusterAddOnStatus `json:"status"`
}

// ManagedClusterAddOnSpec defines the install configuration of
// an addon agent on managed cluster.
type ManagedClusterAddOnSpec struct {
	// installNamespace is the namespace on the managed cluster to install the addon agent.
	// If it is not set, open-cluster-management-agent-addon namespace is used to install the addon agent.
	// +optional
	// +kubebuilder:default=open-cluster-management-agent-addon
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=^[a-z0-9]([-a-z0-9]*[a-z0-9])?$
	InstallNamespace string `json:"installNamespace,omitempty"`

	// configs is a list of add-on configurations.
	// In scenario where the current add-on has its own configurations.
	// An empty list means there are no defautl configurations for add-on.
	// The default is an empty list
	// +optional
	Configs []AddOnConfig `json:"configs,omitempty"`
}

// RegistrationConfig defines the configuration of the addon agent to register to hub. The Klusterlet agent will
// create a csr for the addon agent with the registrationConfig.
type RegistrationConfig struct {
	// signerName is the name of signer that addon agent will use to create csr.
	// +required
	// +kubebuilder:validation:MaxLength=571
	// +kubebuilder:validation:MinLength=5
	SignerName string `json:"signerName"`

	// subject is the user subject of the addon agent to be registered to the hub.
	// If it is not set, the addon agent will have the default subject
	// "subject": {
	//	"user": "system:open-cluster-management:addon:{addonName}:{clusterName}:{agentName}",
	//	"groups: ["system:open-cluster-management:addon", "system:open-cluster-management:addon:{addonName}", "system:authenticated"]
	// }
	//
	// +optional
	Subject Subject `json:"subject,omitempty"`
}

type AddOnConfig struct {
	// group and resource of add-on configuration.
	ConfigGroupResource `json:",inline"`

	// name and namespace of add-on configuration.
	ConfigReferent `json:",inline"`
}

// Subject is the user subject of the addon agent to be registered to the hub.
type Subject struct {
	// user is the user name of the addon agent.
	User string `json:"user"`

	// groups is the user group of the addon agent.
	// +optional
	Groups []string `json:"groups,omitempty"`

	// organizationUnit is the ou of the addon agent
	// +optional
	OrganizationUnits []string `json:"organizationUnit,omitempty"`
}

// ManagedClusterAddOnStatus provides information about the status of the operator.
// +k8s:deepcopy-gen=true
type ManagedClusterAddOnStatus struct {
	// conditions describe the state of the managed and monitored components for the operator.
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"  patchStrategy:"merge" patchMergeKey:"type"`

	// relatedObjects is a list of objects that are "interesting" or related to this operator. Common uses are:
	// 1. the detailed resource driving the operator
	// 2. operator namespaces
	// 3. operand namespaces
	// 4. related ClusterManagementAddon resource
	// +optional
	RelatedObjects []ObjectReference `json:"relatedObjects,omitempty"`

	// addOnMeta is a reference to the metadata information for the add-on.
	// This should be same as the addOnMeta for the corresponding ClusterManagementAddOn resource.
	// +optional
	AddOnMeta AddOnMeta `json:"addOnMeta,omitempty"`

	// Deprecated: Use configReference instead
	// addOnConfiguration is a reference to configuration information for the add-on.
	// This resource is use to locate the configuration resource for the add-on.
	// +optional
	AddOnConfiguration ConfigCoordinates `json:"addOnConfiguration,omitempty"`

	// SupportedConfigs is a list of configuration types that are allowed to override the add-on configurations defined
	// in ClusterManagementAddOn spec.
	// The default is an empty list, which means the add-on configurations can not be overridden.
	// +optional
	// +listType=map
	// +listMapKey=group
	// +listMapKey=resource
	SupportedConfigs []ConfigGroupResource `json:"supportedConfigs,omitempty"`

	// configReferences is a list of current add-on configuration references.
	// This will be overridden by the clustermanagementaddon configuration references.
	// +optional
	ConfigReferences []ConfigReference `json:"configReferences,omitempty"`

	// namespace is the namespace on the managedcluster to put registration secret or lease for the addon. It is
	// required when registrtion is set or healthcheck mode is Lease.
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// registrations is the conifigurations for the addon agent to register to hub. It should be set by each addon controller
	// on hub to define how the addon agent on managedcluster is registered. With the registration defined,
	// The addon agent can access to kube apiserver with kube style API or other endpoints on hub cluster with client
	// certificate authentication. A csr will be created per registration configuration. If more than one
	// registrationConfig is defined, a csr will be created for each registration configuration. It is not allowed that
	// multiple registrationConfigs have the same signer name. After the csr is approved on the hub cluster, the klusterlet
	// agent will create a secret in the installNamespace for the registrationConfig. If the signerName is
	// "kubernetes.io/kube-apiserver-client", the secret name will be "{addon name}-hub-kubeconfig" whose contents includes
	// key/cert and kubeconfig. Otherwise, the secret name will be "{addon name}-{signer name}-client-cert" whose contents includes key/cert.
	// +optional
	Registrations []RegistrationConfig `json:"registrations,omitempty"`

	// healthCheck indicates how to check the healthiness status of the current addon. It should be
	// set by each addon implementation, by default, the lease mode will be used.
	// +optional
	HealthCheck HealthCheck `json:"healthCheck,omitempty"`
}

const (
	// ManagedClusterAddOnConditionAvailable represents that the addon agent is running on the managed cluster
	ManagedClusterAddOnConditionAvailable string = "Available"

	// ManagedClusterAddOnConditionDegraded represents that the addon agent is providing degraded service on
	// the managed cluster.
	ManagedClusterAddOnConditionDegraded string = "Degraded"

	// ManagedClusterAddOnCondtionConfigured represents that the addon agent is configured with its configuration
	ManagedClusterAddOnCondtionConfigured string = "Configured"
)

// ObjectReference contains enough information to let you inspect or modify the referred object.
type ObjectReference struct {
	// group of the referent.
	// +kubebuilder:validation:Required
	// +required
	Group string `json:"group"`
	// resource of the referent.
	// +kubebuilder:validation:Required
	// +required
	Resource string `json:"resource"`
	// namespace of the referent.
	// +optional
	Namespace string `json:"namespace,omitempty"`
	// name of the referent.
	// +kubebuilder:validation:Required
	// +required
	Name string `json:"name"`
}

// ConfigReference is a reference to the current add-on configuration.
// This resource is used to locate the configuration resource for the current add-on.
type ConfigReference struct {
	// This field is synced from ClusterManagementAddOn configGroupResource field.
	ConfigGroupResource `json:",inline"`

	// This field is synced from ClusterManagementAddOn defaultConfig and ManagedClusterAddOn config fields.
	// If both of them are defined, the ManagedClusterAddOn configs will overwrite the ClusterManagementAddOn
	// defaultConfigs.
	ConfigReferent `json:",inline"`

	// lastObservedGeneration is the observed generation of the add-on configuration.
	LastObservedGeneration int64 `json:"lastObservedGeneration"`
}

// HealthCheckMode indicates the mode for the addon to check its healthiness status
// +kubebuilder:validation:Enum=Lease;Customized
type HealthCheckMode string

const (
	// HealthCheckModeLease, the addon maintains a lease in its installation namespace with
	// its status, the registration agent will check this lease to maintain the addon healthiness
	// status.
	HealthCheckModeLease HealthCheckMode = "Lease"

	// HealthCheckModeCustomized, the addon maintains its healthiness status by itself.
	HealthCheckModeCustomized HealthCheckMode = "Customized"
)

type HealthCheck struct {
	// mode indicates which mode will be used to check the healthiness status of the addon.
	// +optional
	// +kubebuilder:default=Lease
	Mode HealthCheckMode `json:"mode,omitempty"`
}

// ManagedClusterAddOnList is a list of ManagedClusterAddOn resources.
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ManagedClusterAddOnList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []ManagedClusterAddOn `json:"items"`
}

const (
	// Label and annotation keys set on ManagedClusterAddon.

	// AddonLabelKey is the label key to set addon name. It is to set on the resources on the hub relating
	// to an addon
	AddonLabelKey = "open-cluster-management.io/addon-name"

	// DisableAddonAutomaticInstallationAnnotationKey is the annotation key for disabling the functionality of
	// installing addon automatically. it should be set on ManagedClusterAddon resource only.
	DisableAddonAutomaticInstallationAnnotationKey = "addon.open-cluster-management.io/disable-automatic-installation"

	// AddonNamespaceLabelKey is the label key to set namespace of ManagedClusterAddon.
	AddonNamespaceLabelKey = "open-cluster-management.io/addon-namespace"

	// Label and annotation keys set on manifests of addon agent.

	// AddonPreDeleteHookLabelKey is the label key to identify that a resource manifest is used as pre-delete hook for an addon
	// and should be created and deleted before the specified ManagedClusterAddon is deleted.
	// Deprecated, and will be removed in the future release, please use annotation AddonPreDeleteHookAnnotationKey from v0.10.0.
	AddonPreDeleteHookLabelKey = "open-cluster-management.io/addon-pre-delete"

	// AddonPreDeleteHookAnnotationKey is the annotation key to identify that a resource manifest is used as pre-delete hook for an addon
	// and should be created and deleted before the specified ManagedClusterAddon is deleted.
	AddonPreDeleteHookAnnotationKey = "addon.open-cluster-management.io/addon-pre-delete"

	// HostingClusterNameAnnotationKey is the annotation key for indicating the hosting cluster name, it should be set
	// on ManagedClusterAddon resource only.
	HostingClusterNameAnnotationKey = "addon.open-cluster-management.io/hosting-cluster-name"

	// DeletionOrphanAnnotationKey is an annotation for the manifest of addon indicating that it will not be cleaned up
	// after the addon is deleted.
	DeletionOrphanAnnotationKey = "addon.open-cluster-management.io/deletion-orphan"

	// HostedManifestLocationLabelKey is the label key to identify where a resource manifest of addon agent
	// with this label should be deployed in Hosted mode.
	// Deprecated, will be removed in the future release, please use annotation HostedManifestLocationAnnotationKey from v0.10.0.
	HostedManifestLocationLabelKey = "addon.open-cluster-management.io/hosted-manifest-location"

	// HostedManifestLocationAnnotationKey is the annotation key to identify where a resource manifest of addon agent
	// with this annotation should be deployed in Hosted mode.
	HostedManifestLocationAnnotationKey = "addon.open-cluster-management.io/hosted-manifest-location"

	// HostedManifestLocationManagedValue is a value of the annotation HostedManifestLocationAnnotationKey,
	// indicates the manifest will be deployed on the managed cluster in Hosted mode,
	// it is the default value of a manifest in Hosted mode.
	HostedManifestLocationManagedValue = "managed"
	// HostedManifestLocationHostingValue is a value of the annotation HostedManifestLocationAnnotationKey,
	// indicates the manifest will be deployed on the hosting cluster in Hosted mode.
	HostedManifestLocationHostingValue = "hosting"
	// HostedManifestLocationNoneValue is a value of the annotation HostedManifestLocationAnnotationKey,,
	// indicates the manifest will not be deployed in Hosted mode.
	HostedManifestLocationNoneValue = "none"
)
