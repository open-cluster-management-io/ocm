package payload

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	workv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
)

var ManifestEventDataType = types.CloudEventsDataType{
	Group:    "io.open-cluster-management.works",
	Version:  "v1alpha1",
	Resource: "manifests",
}

// Manifest represents the data in a cloudevent, it contains a single manifest.
type Manifest struct {
	// Manifest represents a resource to be deployed on managed cluster.
	Manifest unstructured.Unstructured `json:"manifest"`

	// DeleteOption represents deletion strategy when this manifest is deleted.
	DeleteOption *workv1.DeleteOption `json:"deleteOption,omitempty"`

	// ConfigOption represents the configuration of this manifest.
	ConfigOption *ManifestConfigOption `json:"configOption,omitempty"`
}

// ManifestStatus represents the data in a cloudevent, it contains the status of a SingleManifest on a managed
// cluster.
type ManifestStatus struct {
	// Conditions contains the different condition statuses for a SingleManifest on a managed cluster.
	// Valid condition types are:
	// 1. Applied represents the manifest of a SingleManifest is applied successfully on a managed cluster.
	// 2. Progressing represents the manifest of a SingleManifest is being applied on a managed cluster.
	// 3. Available represents the manifest of a SingleManifest exists on the managed cluster.
	// 4. Degraded represents the current state of manifest of a SingleManifest does not match the desired state for a
	//    certain period.
	// 5. Deleted represents the manifests of a SingleManifest is deleted from a managed cluster.
	Conditions []metav1.Condition `json:"conditions"`

	// Status represents the conditions of this manifest on a managed cluster.
	Status *workv1.ManifestCondition `json:"status,omitempty"`
}

type ManifestConfigOption struct {
	// FeedbackRules defines what resource status field should be returned.
	// If it is not set or empty, no feedback rules will be honored.
	FeedbackRules []workv1.FeedbackRule `json:"feedbackRules,omitempty"`

	// UpdateStrategy defines the strategy to update this manifest.
	// UpdateStrategy is Update if it is not set.
	UpdateStrategy *workv1.UpdateStrategy `json:"updateStrategy,omitempty"`
}
