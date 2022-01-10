package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:subresource:status

// ManifestWork represents a manifests workload that hub wants to deploy on the managed cluster.
// A manifest workload is defined as a set of Kubernetes resources.
// ManifestWork must be created in the cluster namespace on the hub, so that agent on the
// corresponding managed cluster can access this resource and deploy on the managed
// cluster.
type ManifestWork struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec represents a desired configuration of work to be deployed on the managed cluster.
	Spec ManifestWorkSpec `json:"spec"`

	// Status represents the current status of work.
	// +optional
	Status ManifestWorkStatus `json:"status,omitempty"`
}

// ManifestWorkSpec represents a desired configuration of manifests to be deployed on the managed cluster.
type ManifestWorkSpec struct {
	// Workload represents the manifest workload to be deployed on a managed cluster.
	Workload ManifestsTemplate `json:"workload,omitempty"`

	// DeleteOption represents deletion strategy when the manifestwork is deleted.
	// Foreground deletion strategy is applied to all the resource in this manifestwork if it is not set.
	// +optional
	DeleteOption *DeleteOption `json:"deleteOption,omitempty"`

	// ManifestConfigs represents the configurations of manifests defined in workload field.
	// +optional
	ManifestConfigs []ManifestConfigOption `json:"manifestConfigs,omitempty"`
}

// Manifest represents a resource to be deployed on managed cluster.
type Manifest struct {
	// +kubebuilder:validation:EmbeddedResource
	// +kubebuilder:pruning:PreserveUnknownFields
	runtime.RawExtension `json:",inline"`
}

// ManifestsTemplate represents the manifest workload to be deployed on a managed cluster.
type ManifestsTemplate struct {
	// Manifests represents a list of kuberenetes resources to be deployed on a managed cluster.
	// +optional
	Manifests []Manifest `json:"manifests,omitempty"`
}

type DeleteOption struct {
	// propagationPolicy can be Foreground, Orphan or SelectivelyOrphan
	// SelectivelyOrphan should be rarely used.  It is provided for cases where particular resources is transfering
	// ownership from one ManifestWork to another or another management unit.
	// Setting this value will allow a flow like
	// 1. create manifestwork/2 to manage foo
	// 2. update manifestwork/1 to selectively orphan foo
	// 3. remove foo from manifestwork/1 without impacting continuity because manifestwork/2 adopts it.
	// +kubebuilder:default=Foreground
	PropagationPolicy DeletePropagationPolicyType `json:"propagationPolicy"`

	// selectivelyOrphan represents a list of resources following orphan deletion stratecy
	SelectivelyOrphan *SelectivelyOrphan `json:"selectivelyOrphans,omitempty"`
}

// ManifestConfigOption represents the configurations of a manifest defined in workload field.
type ManifestConfigOption struct {
	// ResourceIdentifier represents the group, resource, name and namespace of a resoure.
	// iff this refers to a resource not created by this manifest work, the related rules will not be executed.
	// +kubebuilder:validation:Required
	// +required
	ResourceIdentifier ResourceIdentifier `json:"resourceIdentifier"`

	// FeedbackRules defines what resource status field should be returned.
	// +kubebuilder:validation:Required
	// +required
	FeedbackRules []FeedbackRule `json:"feedbackRules"`
}

type FeedbackRule struct {
	// Type defines the option of how status can be returned.
	// It can be jsonPaths or wellKnownStatus.
	// If the type is JSONPaths, user should specify the jsonPaths field
	// If the type is WellKnownStatus, certain common fields of status defined by a rule only
	// for types in in k8s.io/api and open-cluster-management/api will be reported,
	// If these status fields do not exist, no values will be reported.
	// +kubebuilder:validation:Required
	// +required
	Type FeedBackType `json:"type"`

	// JsonPaths defines the json path under status field to be synced.
	// +optional
	JsonPaths []JsonPath `json:"jsonPaths,omitempty"`
}

// +kubebuilder:validation:Enum=WellKnownStatus;JSONPaths
type FeedBackType string

const (
	// WellKnownStatusType represents that values of some common status fields will be returned, which
	// is reflected with a hardcoded rule only for types in k8s.io/api and open-cluster-management/api.
	WellKnownStatusType FeedBackType = "WellKnownStatus"

	// JSONPathsType represents that values of status fields with certain json paths specified will be
	// returned
	JSONPathsType FeedBackType = "JSONPaths"
)

type JsonPath struct {
	// Name represents the alias name for this field
	// +kubebuilder:validation:Required
	// +required
	Name string `json:"name"`

	// Version is the version of the Kubernetes resource.
	// If it is not specified, the resource with the semantically latest version is
	// used to resolve the path.
	// +optional
	Version string `json:"version,omitempty"`

	// Path represents the json path of the field under status.
	// The path must point to a field with single value in the type of integer, bool or string.
	// If the path points to a non-existing field, no value will be returned.
	// If the path points to a structure, map or slice, no value will be returned and the status conddition
	// of StatusFeedBackSynced will be set as false.
	// Ref to https://kubernetes.io/docs/reference/kubectl/jsonpath/ on how to write a jsonPath.
	// +kubebuilder:validation:Required
	// +required
	Path string `json:"path"`
}

// +kubebuilder:validation:Enum=Foreground;Orphan;SelectivelyOrphan
type DeletePropagationPolicyType string

const (
	// DeletePropagationPolicyTypeForeground represents that all the resources in the manifestwork is should
	// be fourground deleted.
	DeletePropagationPolicyTypeForeground DeletePropagationPolicyType = "Foreground"
	// DeletePropagationPolicyTypeOrphan represents that all the resources in the manifestwork is orphaned
	// when the manifestwork is deleted.
	DeletePropagationPolicyTypeOrphan DeletePropagationPolicyType = "Orphan"
	// DeletePropagationPolicyTypeSelectivelyOrphan represents that only selected resources in the manifestwork
	// is orphaned when the manifestwork is deleted.
	DeletePropagationPolicyTypeSelectivelyOrphan DeletePropagationPolicyType = "SelectivelyOrphan"
)

// SelectivelyOrphan represents a list of resources following orphan deletion stratecy
type SelectivelyOrphan struct {
	// orphaningRules defines a slice of orphaningrule.
	// Each orphaningrule identifies a single resource included in this manifestwork
	// +optional
	OrphaningRules []OrphaningRule `json:"orphaningRules,omitempty"`
}

// ResourceIdentifier identifies a single resource included in this manifestwork
type ResourceIdentifier struct {
	// Group is the API Group of the Kubernetes resource,
	// empty string indicates it is in core group.
	// +optional
	Group string `json:"group"`

	// Resource is the resource name of the Kubernetes resource.
	// +kubebuilder:validation:Required
	// +required
	Resource string `json:"resource"`

	// Name is the name of the Kubernetes resource.
	// +kubebuilder:validation:Required
	// +required
	Name string `json:"name"`

	// Name is the namespace of the Kubernetes resource, empty string indicates
	// it is a cluster scoped resource.
	// +optional
	Namespace string `json:"namespace"`
}

// OrphaningRule identifies a single resource included in this manifestwork to be orphaned
type OrphaningRule ResourceIdentifier

// ManifestResourceMeta represents the group, version, kind, as well as the group, version, resource, name and namespace of a resoure.
type ManifestResourceMeta struct {
	// Ordinal represents the index of the manifest on spec.
	// +required
	Ordinal int32 `json:"ordinal"`

	// Group is the API Group of the Kubernetes resource.
	// +optional
	Group string `json:"group"`

	// Version is the version of the Kubernetes resource.
	// +optional
	Version string `json:"version"`

	// Kind is the kind of the Kubernetes resource.
	// +optional
	Kind string `json:"kind"`

	// Resource is the resource name of the Kubernetes resource.
	// +optional
	Resource string `json:"resource"`

	// Name is the name of the Kubernetes resource.
	// +optional
	Name string `json:"name"`

	// Name is the namespace of the Kubernetes resource.
	// +optional
	Namespace string `json:"namespace"`
}

// AppliedManifestResourceMeta represents the group, version, resource, name and namespace of a resource.
// Since these resources have been created, they must have valid group, version, resource, namespace, and name.
type AppliedManifestResourceMeta struct {
	ResourceIdentifier `json:",inline"`

	// Version is the version of the Kubernetes resource.
	// +kubebuilder:validation:Required
	// +required
	Version string `json:"version"`

	// UID is set on successful deletion of the Kubernetes resource by controller. The
	// resource might be still visible on the managed cluster after this field is set.
	// It is not directly settable by a client.
	// +optional
	UID string `json:"uid,omitempty"`
}

// ManifestWorkStatus represents the current status of managed cluster ManifestWork.
type ManifestWorkStatus struct {
	// Conditions contains the different condition statuses for this work.
	// Valid condition types are:
	// 1. Applied represents workload in ManifestWork is applied successfully on managed cluster.
	// 2. Progressing represents workload in ManifestWork is being applied on managed cluster.
	// 3. Available represents workload in ManifestWork exists on the managed cluster.
	// 4. Degraded represents the current state of workload does not match the desired
	// state for a certain period.
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ResourceStatus represents the status of each resource in manifestwork deployed on a
	// managed cluster. The Klusterlet agent on managed cluster syncs the condition from the managed cluster to the hub.
	// +optional
	ResourceStatus ManifestResourceStatus `json:"resourceStatus,omitempty"`
}

// ManifestResourceStatus represents the status of each resource in manifest work deployed on
// managed cluster
type ManifestResourceStatus struct {
	// Manifests represents the condition of manifests deployed on managed cluster.
	// Valid condition types are:
	// 1. Progressing represents the resource is being applied on managed cluster.
	// 2. Applied represents the resource is applied successfully on managed cluster.
	// 3. Available represents the resource exists on the managed cluster.
	// 4. Degraded represents the current state of resource does not match the desired
	// state for a certain period.
	Manifests []ManifestCondition `json:"manifests,omitempty"`
}

const (
	// WorkProgressing represents that the work is in the progress to be
	// applied on the managed cluster.
	WorkProgressing string = "Progressing"
	// WorkApplied represents that the workload defined in work is
	// succesfully applied on the managed cluster.
	WorkApplied string = "Applied"
	// WorkAvailable represents that all resources of the work exists on
	// the managed cluster.
	WorkAvailable string = "Available"
	// WorkDegraded represents that the current state of work does not match
	// the desired state for a certain period.
	WorkDegraded string = "Degraded"
)

// ManifestCondition represents the conditions of the resources deployed on a
// managed cluster.
type ManifestCondition struct {
	// ResourceMeta represents the group, version, kind, name and namespace of a resoure.
	// +required
	ResourceMeta ManifestResourceMeta `json:"resourceMeta"`

	// StatusFeedback represents the values of the feild synced back defined in statusFeedbacks
	// +optional
	StatusFeedbacks StatusFeedbackResult `json:"statusFeedback,omitempty"`

	// Conditions represents the conditions of this resource on a managed cluster.
	// +required
	Conditions []metav1.Condition `json:"conditions"`
}

// StatusFeedbackResult represents the values of the feild synced back defined in statusFeedbacks
type StatusFeedbackResult struct {
	// Values represents the synced value of the interested field.
	// +listType:=map
	// +listMapKey:=name
	// +optional
	Values []FeedbackValue `json:"values,omitempty"`
}

type FeedbackValue struct {
	// Name represents the alias name for this field. It is the same as what is specified
	// in StatuFeedbackRule in the spec.
	// +kubebuilder:validation:Required
	// +required
	Name string `json:"name"`

	// Value is the value of the status field.
	// The value of the status field can only be integer, string or boolean.
	// +kubebuilder:validation:Required
	// +required
	Value FieldValue `json:"fieldValue"`
}

// FieldValue is the value of the status field.
// The value of the status field can only be integer, string or boolean.
type FieldValue struct {
	// Type represents the type of the value, it can be integer, string or boolean.
	// +kubebuilder:validation:Required
	// +required
	Type ValueType `json:"type"`

	// Integer is the integer value when type is integer.
	// +optional
	Integer *int64 `json:"integer,omitempty"`

	// String is the string value when when type is string.
	// +optional
	String *string `json:"string,omitempty"`

	// Boolean is bool value when type is boolean.
	// +optional
	Boolean *bool `json:"boolean,omitempty"`
}

// +kubebuilder:validation:Enum=Integer;String;Boolean
type ValueType string

const (
	Integer ValueType = "Integer"
	String  ValueType = "String"
	Boolean ValueType = "Boolean"
)

// ManifestConditionType represents the condition type of a single
// resource manifest deployed on the managed cluster.
type ManifestConditionType string

const (
	// ManifestProgressing represents that the resource is being applied on the managed cluster
	ManifestProgressing ManifestConditionType = "Progressing"
	// ManifestApplied represents that the resource object is applied
	// on the managed cluster.
	ManifestApplied ManifestConditionType = "Applied"
	// ManifestAvailable represents that the resource object exists
	// on the managed cluster.
	ManifestAvailable ManifestConditionType = "Available"
	// ManifestDegraded represents that the current state of resource object does not
	// match the desired state for a certain period.
	ManifestDegraded ManifestConditionType = "Degraded"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ManifestWorkList is a collection of manifestworks.
type ManifestWorkList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata.
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#types-kinds
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	// Items is a list of manifestworks.
	Items []ManifestWork `json:"items"`
}

// +genclient
// +genclient:nonNamespaced
// +kubebuilder:subresource:status
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// AppliedManifestWork represents an applied manifestwork on managed cluster that is placed
// on a managed cluster. An AppliedManifestWork links to a manifestwork on a hub recording resources
// deployed in the managed cluster.
// When the agent is removed from managed cluster, cluster-admin on managed cluster
// can delete appliedmanifestwork to remove resources deployed by the agent.
// The name of the appliedmanifestwork must be in the format of
// {hash of hub's first kube-apiserver url}-{manifestwork name}
type AppliedManifestWork struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec represents the desired configuration of AppliedManifestWork.
	Spec AppliedManifestWorkSpec `json:"spec,omitempty"`

	// Status represents the current status of AppliedManifestWork.
	// +optional
	Status AppliedManifestWorkStatus `json:"status,omitempty"`
}

// AppliedManifestWorkSpec represents the desired configuration of AppliedManifestWork
type AppliedManifestWorkSpec struct {
	// HubHash represents the hash of the first hub kube apiserver to identify which hub
	// this AppliedManifestWork links to.
	// +required
	HubHash string `json:"hubHash"`

	// ManifestWorkName represents the name of the related manifestwork on the hub.
	// +required
	ManifestWorkName string `json:"manifestWorkName"`
}

// AppliedManifestWorkStatus represents the current status of AppliedManifestWork
type AppliedManifestWorkStatus struct {
	// AppliedResources represents a list of resources defined within the manifestwork that are applied.
	// Only resources with valid GroupVersionResource, namespace, and name are suitable.
	// An item in this slice is deleted when there is no mapped manifest in manifestwork.Spec or by finalizer.
	// The resource relating to the item will also be removed from managed cluster.
	// The deleted resource may still be present until the finalizers for that resource are finished.
	// However, the resource will not be undeleted, so it can be removed from this list and eventual consistency is preserved.
	// +optional
	AppliedResources []AppliedManifestResourceMeta `json:"appliedResources,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// AppliedManifestWorkList is a collection of appliedmanifestworks.
type AppliedManifestWorkList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata.
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#types-kinds
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	// Items is a list of appliedmanifestworks.
	Items []AppliedManifestWork `json:"items"`
}
