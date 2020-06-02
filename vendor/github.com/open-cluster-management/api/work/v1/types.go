package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// StatusCondition contains condition information for a spoke work.
type StatusCondition struct {
	// Type is the type of the spoke work condition.
	// +required
	Type string `json:"type" protobuf:"bytes,1,opt,name=type"`

	// Status is the status of the condition. One of True, False, Unknown.
	// +required
	Status metav1.ConditionStatus `json:"status" protobuf:"bytes,2,opt,name=status,casttype=k8s.io/apimachinery/pkg/apis/meta/v1.ConditionStatus"`

	// LastTransitionTime is the last time the condition changed from one status to another.
	// +required
	LastTransitionTime metav1.Time `json:"lastTransitionTime" protobuf:"bytes,3,opt,name=lastTransitionTime"`

	// Reason is a (brief) reason for the condition's last status change.
	// +required
	Reason string `json:"reason" protobuf:"bytes,4,opt,name=reason"`

	// Message is a human-readable message indicating details about the last status change.
	// +required
	Message string `json:"message" protobuf:"bytes,5,opt,name=message"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:subresource:status

// ManifestWork represents a manifests workload that hub wants to deploy on the spoke cluster.
// A manifest workload is defined as a set of kubernetes resources.
// ManifestWork must be created in the cluster namespace on the hub, so that agent on the
// corresponding spoke cluster can access this resource and deploy on the spoke
// cluster.
type ManifestWork struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// Spec represents a desired configuration of work to be deployed on the spoke cluster.
	Spec ManifestWorkSpec `json:"spec" protobuf:"bytes,2,opt,name=spec"`

	// Status represents the current status of work
	// +optional
	Status ManifestWorkStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

// ManifestWorkSpec represents a desired configuration of manifests to be deployed on the spoke cluster.
type ManifestWorkSpec struct {
	// Workload represents the manifest workload to be deployed on spoke cluster
	Workload ManifestsTemplate `json:"workload,omitempty" protobuf:"bytes,1,opt,name=workload"`
}

// Manifest represents a resource to be deployed on spoke cluster
type Manifest struct {
	// +kubebuilder:validation:EmbeddedResource
	// +kubebuilder:pruning:PreserveUnknownFields
	runtime.RawExtension `json:",inline" protobuf:"bytes,1,opt,name=rawExtension"`
}

// ManifestsTemplate represents the manifest workload to be deployed on spoke cluster
type ManifestsTemplate struct {
	// Manifests represents a list of kuberenetes resources to be deployed on the spoke cluster.
	// +optional
	Manifests []Manifest `json:"manifests,omitempty" protobuf:"bytes,1,rep,name=manifests"`
}

// ManifestResourceMeta represents the gvk, gvr, name and namespace of a resoure
type ManifestResourceMeta struct {
	// Ordinal represents the index of the manifest on spec
	// +required
	Ordinal int32 `json:"ordinal" protobuf:"varint,1,opt,name=ordinal"`

	// Group is the API Group of the kubernetes resource
	// +optional
	Group string `json:"group" protobuf:"bytes,2,opt,name=group"`

	// Version is the version of the kubernetes resource
	// +optional
	Version string `json:"version" protobuf:"bytes,3,opt,name=version"`

	// Kind is the kind of the kubernetes resource
	// +optional
	Kind string `json:"kind" protobuf:"bytes,4,opt,name=kind"`

	// Resource is the resource name of the kubernetes resource
	// +optional
	Resource string `json:"resource" protobuf:"bytes,5,opt,name=resource"`

	// Name is the name of the kubernetes resource
	// +optional
	Name string `json:"name" protobuf:"bytes,6,opt,name=name"`

	// Name is the namespace of the kubernetes resource
	// +optional
	Namespace string `json:"namespace" protobuf:"bytes,7,opt,name=namespace"`
}

// AppliedManifestResourceMeta represents the gvr, name and namespace of a resource.
// Since these resources have been created, they must have valid group, version, resource, namespace, and name.
type AppliedManifestResourceMeta struct {
	// Group is the API Group of the kubernetes resource
	// +required
	Group string `json:"group" protobuf:"bytes,1,opt,name=group"`

	// Version is the version of the kubernetes resource
	// +required
	Version string `json:"version" protobuf:"bytes,2,opt,name=version"`

	// Resource is the resource name of the kubernetes resource
	// +required
	Resource string `json:"resource" protobuf:"bytes,3,opt,name=resource"`

	// Name is the name of the kubernetes resource
	// +required
	Name string `json:"name" protobuf:"bytes,4,opt,name=name"`

	// Name is the namespace of the kubernetes resource, empty string indicates
	// it is a cluster scoped resource.
	// +required
	Namespace string `json:"namespace" protobuf:"bytes,5,opt,name=namespace"`
}

// ManifestWorkStatus represents the current status of spoke manifest workload
type ManifestWorkStatus struct {
	// Conditions contains the different condition statuses for this work.
	// Valid condition types are:
	// 1. Applied represents workload in ManifestWork is applied successfully on spoke cluster.
	// 2. Progressing represents workload in ManifestWork is being applied on spoke cluster.
	// 3. Available represents workload in ManifestWork exists on the spoke cluster.
	// 4. Degraded represents the current state of workload does not match the desired
	// state for a certain period.
	Conditions []StatusCondition `json:"conditions" protobuf:"bytes,1,rep,name=conditions"`

	// ResourceStatus represents the status of each resource in manifestwork deployed on
	// spoke cluster. The agent on spoke cluster syncs the condition from spoke to the hub.
	// +optional
	ResourceStatus ManifestResourceStatus `json:"resourceStatus,omitempty" protobuf:"bytes,2,rep,name=resourceStatus"`

	// AppliedResources represents a list of resources defined within the manifestwork that are applied.
	// Only resources with valid GroupVersionResource, namespace, and name are suitable.
	// An item in this slice is deleted when there is no mapped manifest in manifestwork.Spec or by finalizer.
	// The resource relating to the item will also be removed from spoke cluster.
	// The deleted resource may still be present until the finalizers for that resource are finished.
	// However, the resource will not be undeleted, so it can be removed from this list and eventual consistency is preserved.
	// +optional
	AppliedResources []AppliedManifestResourceMeta `json:"appliedResources,omitempty" protobuf:"bytes,3,rep,name=appliedResources"`
}

// ManifestResourceStatus represents the status of each resource in manifest work deployed on
// spoke cluster
type ManifestResourceStatus struct {
	// Manifests represents the condition of manifests deployed on spoke cluster.
	// Valid condition types are:
	// 1. Progressing represents the resource is being applied on spoke cluster.
	// 2. Applied represents the resource is applied successfully on spoke cluster.
	// 3. Available represents the resource exists on the spoke cluster.
	// 4. Degraded represents the current state of resource does not match the desired
	// state for a certain period.
	Manifests []ManifestCondition `json:"manifests,omitempty" protobuf:"bytes,2,opt,name=manifests"`
}

// WorkStatusConditionType represents the condition type of the work
type WorkStatusConditionType string

const (
	// WorkProgressing represents that the work is in the progress to be
	// applied on the spoke cluster.
	WorkProgressing WorkStatusConditionType = "Progressing"
	// WorkApplied represents that the workload defined in work is
	// succesfully applied on the spoke cluster.
	WorkApplied WorkStatusConditionType = "Applied"
	// WorkAvailable represents that all resources of the work exists on
	// the spoke cluster.
	WorkAvailable WorkStatusConditionType = "Available"
	// WorkDegraded represents that the current state of work does not match
	// the desired state for a certain period.
	WorkDegraded WorkStatusConditionType = "Degraded"
)

// ManifestCondition represents the conditions of the resources deployed on
// spoke cluster
type ManifestCondition struct {
	// ResourceMeta represents the gvk, name and namespace of a resoure
	// +required
	ResourceMeta ManifestResourceMeta `json:"resourceMeta" protobuf:"bytes,1,opt,name=resourceMeta"`

	// Conditions represents the conditions of this resource on spoke cluster
	// +required
	Conditions []StatusCondition `json:"conditions" protobuf:"bytes,2,rep,name=conditions"`
}

// ManifestConditionType represents the condition type of a single
// resource manifest deployed on the spoke cluster.
type ManifestConditionType string

const (
	// ManifestProgressing represents the resource is being applied on the spoke cluster
	ManifestProgressing ManifestConditionType = "Progressing"
	// ManifestApplied represents that the resource object is applied
	// on the spoke cluster.
	ManifestApplied ManifestConditionType = "Applied"
	// ManifestAvailable represents that the resource object exists
	// on the spoke cluster.
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
	metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// Items is a list of manifestworks.
	Items []ManifestWork `json:"items" protobuf:"bytes,2,rep,name=items"`
}
