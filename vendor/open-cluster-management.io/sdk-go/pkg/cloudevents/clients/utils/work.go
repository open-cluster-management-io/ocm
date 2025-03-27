package utils

import (
	"bytes"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/validation/field"

	workv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/apis/work/v1/validator"
)

func ValidateWork(work *workv1.ManifestWork) field.ErrorList {
	errs := field.ErrorList{}

	if work.Namespace == "" {
		errs = append(errs, field.Required(field.NewPath("metadata").Child("namespace"), "field not set"))
	}

	if metaErrs := ValidateResourceMetadata(work); len(metaErrs) != 0 {
		errs = append(errs, metaErrs...)
	}

	if err := validator.ManifestValidator.ValidateManifests(work.Spec.Workload.Manifests); err != nil {
		errs = append(errs, field.Invalid(field.NewPath("spec"), "spec", err.Error()))
	}
	return errs
}

// encode ensures the given work's manifests are encoded
func EncodeManifests(work *workv1.ManifestWork) error {
	for index, manifest := range work.Spec.Workload.Manifests {
		if manifest.Raw == nil {
			if manifest.Object == nil {
				return fmt.Errorf("the Object and Raw of the manifest[%d] for the work (%s/%s) are both `nil`",
					index, work.Namespace, work.Name)
			}

			var buf bytes.Buffer
			if err := unstructured.UnstructuredJSONScheme.Encode(manifest.Object, &buf); err != nil {
				return err
			}

			work.Spec.Workload.Manifests[index].Raw = buf.Bytes()
		}
	}

	return nil
}
