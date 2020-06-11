package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// StatusCondition contains condition information for a ManifestWork applied to a managed cluster.
type StatusCondition struct {
	// Type is the type of the ManifestWork condition.
	// +required
	Type string `json:"type"`

	// Status is the status of the condition. One of True, False, Unknown.
	// +required
	Status metav1.ConditionStatus `json:"status"`

	// LastTransitionTime is the last time the condition changed from one status to another.
	// +required
	LastTransitionTime metav1.Time `json:"lastTransitionTime"`

	// Reason is a (brief) reason for the condition's last status change.
	// +required
	Reason string `json:"reason"`

	// Message is a human-readable message indicating details about the last status change.
	// +required
	Message string `json:"message"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:subresource:status

// ManifestWork represents a manifests workload that hub wants to deploy on the managed cluster.
// A manifest workload is defined as a set of kubernetes resources.
// ManifestWork must be created in the cluster namespace on the hub, so that agent on the
// corresponding managed cluster can access this resource and deploy on the managed
// cluster.
type ManifestWork struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec represents a desired configuration of work to be deployed on the managed cluster.
	Spec ManifestWorkSpec `json:"spec"`

	// Status represents the current status of work
	// +optional
	Status ManifestWorkStatus `json:"status,omitempty"`
}

// ManifestWorkSpec represents a desired configuration of manifests to be deployed on the managed cluster.
type ManifestWorkSpec struct {
	// Workload represents the manifest workload to be deployed on managed cluster
	Workload ManifestsTemplate `json:"workload,omitempty"`
}

// Manifest represents a resource to be deployed on managed cluster
type Manifest struct {
	// +kubebuilder:validation:EmbeddedResource
	// +kubebuilder:pruning:PreserveUnknownFields
	runtime.RawExtension `json:",inline"`
}

// ManifestsTemplate represents the manifest workload to be deployed on managed cluster
type ManifestsTemplate struct {
	// Manifests represents a list of kuberenetes resources to be deployed on the managed cluster.
	// +optional
	Manifests []Manifest `json:"manifests,omitempty"`
}

// ManifestResourceMeta represents the gvk, gvr, name and namespace of a resoure
type ManifestResourceMeta struct {
	// Ordinal represents the index of the manifest on spec
	// +required
	Ordinal int32 `json:"ordinal"`

	// Group is the API Group of the kubernetes resource
	// +optional
	Group string `json:"group"`

	// Version is the version of the kubernetes resource
	// +optional
	Version string `json:"version"`

	// Kind is the kind of the kubernetes resource
	// +optional
	Kind string `json:"kind"`

	// Resource is the resource name of the kubernetes resource
	// +optional
	Resource string `json:"resource"`

	// Name is the name of the kubernetes resource
	// +optional
	Name string `json:"name"`

	// Name is the namespace of the kubernetes resource
	// +optional
	Namespace string `json:"namespace"`
}

// AppliedManifestResourceMeta represents the gvr, name and namespace of a resource.
// Since these resources have been created, they must have valid group, version, resource, namespace, and name.
type AppliedManifestResourceMeta struct {
	// Group is the API Group of the kubernetes resource
	// +required
	Group string `json:"group"`

	// Version is the version of the kubernetes resource
	// +required
	Version string `json:"version"`

	// Resource is the resource name of the kubernetes resource
	// +required
	Resource string `json:"resource"`

	// Name is the name of the kubernetes resource
	// +required
	Name string `json:"name"`

	// Name is the namespace of the kubernetes resource, empty string indicates
	// it is a cluster scoped resource.
	// +required
	Namespace string `json:"namespace"`
}

// ManifestWorkStatus represents the current status of managed cluster ManifestWork
type ManifestWorkStatus struct {
	// Conditions contains the different condition statuses for this work.
	// Valid condition types are:
	// 1. Applied represents workload in ManifestWork is applied successfully on managed cluster.
	// 2. Progressing represents workload in ManifestWork is being applied on managed cluster.
	// 3. Available represents workload in ManifestWork exists on the managed cluster.
	// 4. Degraded represents the current state of workload does not match the desired
	// state for a certain period.
	Conditions []StatusCondition `json:"conditions"`

	// ResourceStatus represents the status of each resource in manifestwork deployed on
	// managed cluster. The Klusterlet agent on managed cluster syncs the condition from managed to the hub.
	// +optional
	ResourceStatus ManifestResourceStatus `json:"resourceStatus,omitempty"`

	// AppliedResources represents a list of resources defined within the manifestwork that are applied.
	// Only resources with valid GroupVersionResource, namespace, and name are suitable.
	// An item in this slice is deleted when there is no mapped manifest in manifestwork.Spec or by finalizer.
	// The resource relating to the item will also be removed from managed cluster.
	// The deleted resource may still be present until the finalizers for that resource are finished.
	// However, the resource will not be undeleted, so it can be removed from this list and eventual consistency is preserved.
	// +optional
	AppliedResources []AppliedManifestResourceMeta `json:"appliedResources,omitempty"`
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

// WorkStatusConditionType represents the condition type of the work
type WorkStatusConditionType string

const (
	// WorkProgressing represents that the work is in the progress to be
	// applied on the managed cluster.
	WorkProgressing WorkStatusConditionType = "Progressing"
	// WorkApplied represents that the workload defined in work is
	// succesfully applied on the managed cluster.
	WorkApplied WorkStatusConditionType = "Applied"
	// WorkAvailable represents that all resources of the work exists on
	// the managed cluster.
	WorkAvailable WorkStatusConditionType = "Available"
	// WorkDegraded represents that the current state of work does not match
	// the desired state for a certain period.
	WorkDegraded WorkStatusConditionType = "Degraded"
)

// ManifestCondition represents the conditions of the resources deployed on
// managed cluster
type ManifestCondition struct {
	// ResourceMeta represents the gvk, name and namespace of a resoure
	// +required
	ResourceMeta ManifestResourceMeta `json:"resourceMeta"`

	// Conditions represents the conditions of this resource on managed cluster
	// +required
	Conditions []StatusCondition `json:"conditions"`
}

// ManifestConditionType represents the condition type of a single
// resource manifest deployed on the managed cluster.
type ManifestConditionType string

const (
	// ManifestProgressing represents the resource is being applied on the managed cluster
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
