package workvalidator

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
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
		return errors.NewBadRequest("Workload manifests should not be empty")
	}

	totalSize := 0
	for _, manifest := range manifests {
		totalSize = totalSize + manifest.Size()
	}

	if totalSize > m.limit {
		return fmt.Errorf("the size of manifests is %v bytes which exceeds the %v limit", totalSize, m.limit)
	}

	for _, manifest := range manifests {
		err := validateManifest(manifest.Raw)
		if err != nil {
			return err
		}
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
