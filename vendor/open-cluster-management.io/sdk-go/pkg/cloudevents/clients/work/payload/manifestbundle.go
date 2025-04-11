package payload

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	workv1 "open-cluster-management.io/api/work/v1"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
)

var ManifestBundleEventDataType = types.CloudEventsDataType{
	Group:    "io.open-cluster-management.works",
	Version:  "v1alpha1",
	Resource: "manifestbundles",
}

// ManifestBundle represents the data in a cloudevent, it contains a bundle of manifests.
type ManifestBundle struct {
	// Manifests represents a list of Kuberenetes resources to be deployed on a managed cluster.
	Manifests []workv1.Manifest `json:"manifests"`

	// DeleteOption represents deletion strategy when the manifests are deleted.
	DeleteOption *workv1.DeleteOption `json:"deleteOption,omitempty"`

	// ManifestConfigs represents the configurations of manifests.
	ManifestConfigs []workv1.ManifestConfigOption `json:"manifestConfigs,omitempty"`
}

// ManifestBundleStatus represents the data in a cloudevent, it contains the status of a ManifestBundle on a managed
// cluster.
type ManifestBundleStatus struct {
	// ManifestBundle represents the specific of this status.
	// This is an optional field, it can be used by a source work client without local cache to
	// rebuild a whole work when received the work's status update.
	ManifestBundle *ManifestBundle `json:"manifestBundle,omitempty"`

	// Conditions contains the different condition statuses for a ManifestBundle on managed cluster.
	// Valid condition types are:
	// 1. Applied represents the manifests in a ManifestBundle are applied successfully on a managed cluster.
	// 2. Progressing represents the manifests in a ManifestBundle are being applied on a managed cluster.
	// 3. Available represents the manifests in a ManifestBundle exist on a managed cluster.
	// 4. Degraded represents the current state of manifests in a ManifestBundle do not match the desired state for a
	//    certain period.
	// 5. Deleted represents the manifests in a ManifestBundle are deleted from a managed cluster.
	Conditions []metav1.Condition `json:"conditions"`

	// ManifestResourceStatus represents the status of each resource in manifest work deployed on managed cluster.
	ResourceStatus []workv1.ManifestCondition `json:"resourceStatus,omitempty"`
}
