package common

import (
	"encoding/json"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/sets"

	workv1 "open-cluster-management.io/api/work/v1"
)

type Validator struct {
	limit int
}

var ManifestValidator = &Validator{limit: 500 * 1024} // the default manifest limit is 500k.

func (m *Validator) WithLimit(limit int) {
	m.limit = limit
}

func (m *Validator) ValidateManifests(manifests []workv1.Manifest) error {
	if len(manifests) == 0 {
		return apierrors.NewBadRequest("Workload manifests should not be empty")
	}

	totalSize := 0
	for _, manifest := range manifests {
		totalSize += manifest.Size()
	}

	if totalSize > m.limit {
		return fmt.Errorf("the size of manifests is %v bytes which exceeds the %v limit", totalSize, m.limit)
	}

	// Track seen manifests to detect duplicates
	seen := sets.New[string]()

	for i, manifest := range manifests {
		err := validateManifest(manifest.Raw)
		if err != nil {
			return err
		}

		// Check for duplicate manifests
		info, err := extractManifestInfo(manifest.Raw)
		if err != nil {
			return fmt.Errorf("failed to extract metadata from manifest at index %d: %w", i, err)
		}

		if seen.Has(info.key) {
			return fmt.Errorf("duplicate manifest for resource %s/%s with resource type %s", info.namespace, info.name, info.gvk)
		}
		seen.Insert(info.key)
	}

	return nil
}

func validateManifest(manifest []byte) error {
	// If the manifest cannot be decoded, return err
	unstructuredObj := &unstructured.Unstructured{}
	err := unstructuredObj.UnmarshalJSON(manifest)
	if err != nil {
		return err
	}

	// The object must have name specified, generateName is not allowed in manifestwork
	if unstructuredObj.GetName() == "" {
		return fmt.Errorf("name must be set in manifest")
	}

	if unstructuredObj.GetGenerateName() != "" {
		return fmt.Errorf("generateName must not be set in manifest")
	}

	return nil
}

// manifestInfo contains the metadata needed for duplicate detection and error messages.
type manifestInfo struct {
	key       string // unique key for duplicate detection: apiVersion/kind/namespace/name
	name      string
	namespace string
	gvk       string // apiVersion.kind format for error messages
}

// extractManifestInfo extracts metadata from a manifest for duplicate detection.
func extractManifestInfo(manifest []byte) (*manifestInfo, error) {
	meta := &metav1.PartialObjectMetadata{}
	if err := json.Unmarshal(manifest, meta); err != nil {
		return nil, err
	}

	return &manifestInfo{
		key:       fmt.Sprintf("%s/%s/%s/%s", meta.APIVersion, meta.Kind, meta.Namespace, meta.Name),
		name:      meta.Name,
		namespace: meta.Namespace,
		gvk:       fmt.Sprintf("%s.%s", meta.APIVersion, meta.Kind),
	}, nil
}
