package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope="Namespaced",shortName={"mca","mcas"}
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
	// An empty list means there are no default configurations for add-on.
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
	// +kubebuilder:validation:Pattern=^([a-z0-9][a-z0-9-]*[a-z0-9]\.)+[a-z]+\/[a-z0-9-\.]+$
	SignerName string `json:"signerName"`

	// subject is the user subject of the addon agent to be registered to the hub.
	// If it is not set, the addon agent will have the default subject
	// "subject": {
	//   "user": "system:open-cluster-management:cluster:{clusterName}:addon:{addonName}:agent:{agentName}",
	//   "groups: ["system:open-cluster-management:cluster:{clusterName}:addon:{addonName}",
	//             "system:open-cluster-management:addon:{addonName}", "system:authenticated"]
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

	// Deprecated: Use configReferences instead.
	// addOnConfiguration is a reference to configuration information for the add-on.
	// This resource is used to locate the configuration resource for the add-on.
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
	// required when registration is set or healthcheck mode is Lease.
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// registrations is the configurations for the addon agent to register to hub. It should be set by each addon controller
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

	// Deprecated: Use DesiredConfig instead
	// This field is synced from ClusterManagementAddOn defaultConfig and ManagedClusterAddOn config fields.
	// If both of them are defined, the ManagedClusterAddOn configs will overwrite the ClusterManagementAddOn
	// defaultConfigs.
	ConfigReferent `json:",inline"`

	// Deprecated: Use LastAppliedConfig instead
	// lastObservedGeneration is the observed generation of the add-on configuration.
	LastObservedGeneration int64 `json:"lastObservedGeneration"`

	// desiredConfig record the desired config spec hash.
	// +optional
	DesiredConfig *ConfigSpecHash `json:"desiredConfig"`

	// lastAppliedConfig record the config spec hash when the corresponding ManifestWork is applied successfully.
	// +optional
	LastAppliedConfig *ConfigSpecHash `json:"lastAppliedConfig"`
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
	// HostedManifestLocationNoneValue is a value of the annotation HostedManifestLocationAnnotationKey,
	// indicates the manifest will not be deployed in Hosted mode.
	HostedManifestLocationNoneValue = "none"

	// Finalizers on the managedClusterAddon.

	// AddonDeprecatedPreDeleteHookFinalizer is the finalizer for an addon which has deployed hook objects.
	// Deprecated: please use the AddonPreDeleteHookFinalizer instead.
	AddonDeprecatedPreDeleteHookFinalizer = "cluster.open-cluster-management.io/addon-pre-delete"
	// AddonDeprecatedHostingPreDeleteHookFinalizer is the finalizer for an addon which has deployed hook objects
	// on hosting cluster.
	// Deprecated: please use the AddonHostingPreDeleteHookFinalizer instead.
	AddonDeprecatedHostingPreDeleteHookFinalizer = "cluster.open-cluster-management.io/hosting-addon-pre-delete"
	// AddonDeprecatedHostingManifestFinalizer is the finalizer for an addon which has deployed manifests on the external
	// hosting cluster in Hosted mode.
	// Deprecated: please use the AddonHostingPreDeleteHookFinalizer instead.
	AddonDeprecatedHostingManifestFinalizer = "cluster.open-cluster-management.io/hosting-manifests-cleanup"

	// AddonPreDeleteHookFinalizer is the finalizer for an addon which has deployed hook objects.
	AddonPreDeleteHookFinalizer = "addon.open-cluster-management.io/addon-pre-delete"
	// AddonHostingPreDeleteHookFinalizer is the finalizer for an addon which has deployed hook objects
	// on hosting cluster.
	AddonHostingPreDeleteHookFinalizer = "addon.open-cluster-management.io/hosting-addon-pre-delete"
	// AddonHostingManifestFinalizer is the finalizer for an addon which has deployed manifests on the external
	// hosting cluster in Hosted mode.
	AddonHostingManifestFinalizer = "addon.open-cluster-management.io/hosting-manifests-cleanup"
)

// addon status condition types
const (
	// ManagedClusterAddOnConditionAvailable represents that the addon agent is running on the managed cluster
	ManagedClusterAddOnConditionAvailable string = "Available"

	// ManagedClusterAddOnConditionDegraded represents that the addon agent is providing degraded service on
	// the managed cluster.
	ManagedClusterAddOnConditionDegraded string = "Degraded"

	// Deprecated: Use ManagedClusterAddOnConditionProgressing instead
	// ManagedClusterAddOnConditionConfigured represents that the addon agent is configured with its configuration
	ManagedClusterAddOnConditionConfigured string = "Configured"

	// ManagedClusterAddOnConditionProgressing represents that the addon agent is applying configurations.
	ManagedClusterAddOnConditionProgressing string = "Progressing"

	// ManagedClusterAddOnManifestApplied is a condition type representing whether the manifest of an addon is
	// applied correctly.
	ManagedClusterAddOnManifestApplied = "ManifestApplied"

	// ManagedClusterAddOnHookManifestCompleted is a condition type representing whether the addon hook is completed.
	ManagedClusterAddOnHookManifestCompleted = "HookManifestCompleted"

	// ManagedClusterAddOnHostingManifestApplied is a condition type representing whether the manifest of an addon
	// is applied on the hosting cluster correctly.
	ManagedClusterAddOnHostingManifestApplied = "HostingManifestApplied"

	// ManagedClusterAddOnHostingClusterValidity is a condition type representing whether the hosting cluster is
	// valid in Hosted mode.
	ManagedClusterAddOnHostingClusterValidity = "HostingClusterValidity"

	// ManagedClusterAddOnRegistrationApplied is a condition type representing whether the registration of
	// the addon agent is configured.
	ManagedClusterAddOnRegistrationApplied = "RegistrationApplied"
)

// the reasons of condition ManagedClusterAddOnConditionAvailable
const (
	// AddonAvailableReasonWorkNotFound is the reason of condition Available indicating the addon manifestWorks
	// are not found.
	AddonAvailableReasonWorkNotFound = "WorkNotFound"

	// AddonAvailableReasonWorkApplyFailed is the reason of condition Available indicating the addon manifestWorks
	// are failed to apply.
	AddonAvailableReasonWorkApplyFailed = "WorkApplyFailed"

	// AddonAvailableReasonWorkNotApply is the reason of condition Available indicating the addon manifestWorks
	// are not applied.
	AddonAvailableReasonWorkNotApply = "WorkNotApplied"

	// AddonAvailableReasonWorkApply is the reason of condition Available indicating the addon manifestWorks
	// are applied.
	AddonAvailableReasonWorkApply = "WorkApplied"

	// AddonAvailableReasonNoProbeResult is the reason of condition Available indicating no probe result found in
	// the manifestWorks for the health check.
	AddonAvailableReasonNoProbeResult = "NoProbeResult"

	// AddonAvailableReasonProbeUnavailable is the reason of condition Available indicating the probe result found
	// does not meet the health check.
	AddonAvailableReasonProbeUnavailable = "ProbeUnavailable"

	// AddonAvailableReasonProbeAvailable is the reason of condition Available indicating the probe result found
	// meets the health check.
	AddonAvailableReasonProbeAvailable = "ProbeAvailable"

	// AddonAvailableReasonLeaseUpdateStopped is the reason if condition Available indicating the lease stops updating
	// during health check.
	AddonAvailableReasonLeaseUpdateStopped = "ManagedClusterAddOnLeaseUpdateStopped"

	// AddonAvailableReasonLeaseLeaseNotFound is the reason if condition Available indicating the lease is not found
	// during health check.
	AddonAvailableReasonLeaseLeaseNotFound = "ManagedClusterAddOnLeaseNotFound"

	// AddonAvailableReasonLeaseLeaseUpdated is the reason if condition Available indicating the lease is updated
	// during health check.
	AddonAvailableReasonLeaseLeaseUpdated = "ManagedClusterAddOnLeaseUpdated"
)

// the reasons of condition ManagedClusterAddOnManifestApplied
const (
	// AddonManifestAppliedReasonWorkApplyFailed is the reason of condition AddonManifestApplied indicating
	// the failure of apply manifestWork of the manifests.
	AddonManifestAppliedReasonWorkApplyFailed = "ManifestWorkApplyFailed"

	// AddonManifestAppliedReasonManifestsApplied is the reason of condition AddonManifestApplied indicating
	// the manifests is applied on the managedCluster.
	AddonManifestAppliedReasonManifestsApplied = "AddonManifestApplied"

	// AddonManifestAppliedReasonManifestsApplyFailed is the reason of condition AddonManifestApplied indicating
	// the failure to apply manifests on the managedCluster.
	AddonManifestAppliedReasonManifestsApplyFailed = "AddonManifestAppliedFailed"
)

// the reasons of condition ManagedClusterAddOnHostingClusterValidity
const (
	// HostingClusterValidityReasonValid is the reason of condition HostingClusterValidity indicating the hosting
	// cluster is valid.
	HostingClusterValidityReasonValid = "HostingClusterValid"

	// HostingClusterValidityReasonInvalid is the reason of condition HostingClusterValidity indicating the hosting
	// cluster is invalid.
	HostingClusterValidityReasonInvalid = "HostingClusterInvalid"
)

// the reason of condition ManagedClusterAddOnConditionProgressing
const (
	// ProgressingReasonProgressing is the reason of condition Progressing indicating the addon configuration is
	// applying.
	ProgressingReasonProgressing = "Progressing"

	// ProgressingReasonCompleted is the reason of condition Progressing indicating the addon configuration is
	// applied successfully.
	ProgressingReasonCompleted = "Completed"

	// ProgressingReasonFailed is the reason of condition Progressing indicating the addon configuration
	// failed to apply.
	ProgressingReasonFailed = "Failed"

	// ProgressingReasonWaitingForCanary is the reason of condition Progressing indicating the addon configuration
	// upgrade is pending and waiting for canary is done.
	ProgressingReasonWaitingForCanary = "WaitingForCanary"

	// ProgressingReasonConfigurationUnsupported is the reason of condition Progressing indicating the addon configuration
	// is not supported.
	ProgressingReasonConfigurationUnsupported = "ConfigurationUnsupported"
)

// the reasons of condition ManagedClusterAddOnRegistrationApplied
const (
	// RegistrationAppliedNilRegistration is the reason of condition RegistrationApplied indicating that there is no
	// registration option.
	RegistrationAppliedNilRegistration = "NilRegistration"

	// RegistrationAppliedSetPermissionFailed is the reason of condition RegistrationApplied indicating that it is
	// failed to set up rbac for the addon agent.
	RegistrationAppliedSetPermissionFailed = "SetPermissionFailed"

	// RegistrationAppliedSetPermissionApplied is the reason of condition RegistrationApplied indicating that it is
	// successful to set up rbac for the addon agent.
	RegistrationAppliedSetPermissionApplied = "SetPermissionApplied"
)
