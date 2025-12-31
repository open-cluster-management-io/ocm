package common

import (
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

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
	seen := make(map[string]int) // key -> first occurrence index

	for i, manifest := range manifests {
		err := validateManifest(manifest.Raw)
		if err != nil {
			return err
		}

		// Check for duplicate manifests
		key, err := extractManifestKey(manifest.Raw)
		if err != nil {
			// If we cannot extract the key, skip duplicate check
			// (validateManifest would have caught invalid manifests)
			continue
		}

		if firstIndex, exists := seen[key]; exists {
			return fmt.Errorf("duplicate manifest at index %d: %s (first occurrence at index %d)", i, key, firstIndex)
		}
		seen[key] = i
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

// extractManifestKey extracts a unique identifier from a manifest.
// The key format is: apiVersion/kind/namespace/name
// For cluster-scoped resources, namespace will be empty.
func extractManifestKey(manifest []byte) (string, error) {
	unstructuredObj := &unstructured.Unstructured{}
	if err := unstructuredObj.UnmarshalJSON(manifest); err != nil {
		return "", err
	}

	return fmt.Sprintf("%s/%s/%s/%s",
		unstructuredObj.GetAPIVersion(),
		unstructuredObj.GetKind(),
		unstructuredObj.GetNamespace(),
		unstructuredObj.GetName()), nil
}
