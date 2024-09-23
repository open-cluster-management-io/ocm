package utils

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/bwmarrin/snowflake"
	jsonpatch "github.com/evanphx/json-patch"
	"github.com/google/uuid"

	"k8s.io/apimachinery/pkg/api/validation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	metav1validation "k8s.io/apimachinery/pkg/apis/meta/v1/validation"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/client-go/tools/cache"

	workv1 "open-cluster-management.io/api/work/v1"

	"open-cluster-management.io/sdk-go/pkg/apis/work/v1/validator"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/work/common"
)

// Patch applies the patch to a work with the patch type.
func Patch(patchType types.PatchType, work *workv1.ManifestWork, patchData []byte) (*workv1.ManifestWork, error) {
	workData, err := json.Marshal(work)
	if err != nil {
		return nil, err
	}

	var patchedData []byte
	switch patchType {
	case types.JSONPatchType:
		var patchObj jsonpatch.Patch
		patchObj, err = jsonpatch.DecodePatch(patchData)
		if err != nil {
			return nil, err
		}
		patchedData, err = patchObj.Apply(workData)
		if err != nil {
			return nil, err
		}

	case types.MergePatchType:
		patchedData, err = jsonpatch.MergePatch(workData, patchData)
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported patch type: %s", patchType)
	}

	patchedWork := &workv1.ManifestWork{}
	if err := json.Unmarshal(patchedData, patchedWork); err != nil {
		return nil, err
	}

	return patchedWork, nil
}

// UID returns a v5 UUID based on sourceID, work name and namespace to make sure it is consistent
func UID(sourceID, namespace, name string) string {
	id := fmt.Sprintf("%s-%s-%s-%s", sourceID, common.ManifestWorkGR.String(), namespace, name)
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(id)).String()
}

// ListWorksWithOptions retrieves the manifestworks from store which matches the options.
func ListWorksWithOptions(store cache.Store, namespace string, opts metav1.ListOptions) ([]*workv1.ManifestWork, error) {
	var err error

	labelSelector := labels.Everything()
	fieldSelector := fields.Everything()

	if len(opts.LabelSelector) != 0 {
		labelSelector, err = labels.Parse(opts.LabelSelector)
		if err != nil {
			return nil, fmt.Errorf("invalid labels selector %q: %v", opts.LabelSelector, err)
		}
	}

	if len(opts.FieldSelector) != 0 {
		fieldSelector, err = fields.ParseSelector(opts.FieldSelector)
		if err != nil {
			return nil, fmt.Errorf("invalid fields selector %q: %v", opts.FieldSelector, err)
		}
	}

	works := []*workv1.ManifestWork{}
	// list with labels
	if err := cache.ListAll(store, labelSelector, func(obj interface{}) {
		work, ok := obj.(*workv1.ManifestWork)
		if !ok {
			return
		}

		if namespace != metav1.NamespaceAll && work.Namespace != namespace {
			return
		}

		workFieldSet := fields.Set{
			"metadata.name":      work.Name,
			"metadata.namespace": work.Namespace,
		}

		if !fieldSelector.Matches(workFieldSet) {
			return
		}

		works = append(works, work)
	}); err != nil {
		return nil, err
	}

	return works, nil
}

func Validate(work *workv1.ManifestWork) field.ErrorList {
	fldPath := field.NewPath("metadata")
	errs := field.ErrorList{}

	if work.UID == "" {
		errs = append(errs, field.Required(fldPath.Child("uid"), "field not set"))
	}

	if work.ResourceVersion == "" {
		errs = append(errs, field.Required(fldPath.Child("resourceVersion"), "field not set"))
	}

	if work.Name == "" {
		errs = append(errs, field.Required(fldPath.Child("name"), "field not set"))
	}

	for _, msg := range validation.ValidateNamespaceName(work.Name, false) {
		errs = append(errs, field.Invalid(fldPath.Child("name"), work.Name, msg))
	}

	if work.Namespace == "" {
		errs = append(errs, field.Required(fldPath.Child("namespace"), "field not set"))
	}

	for _, msg := range validation.ValidateNamespaceName(work.Namespace, false) {
		errs = append(errs, field.Invalid(fldPath.Child("namespace"), work.Namespace, msg))
	}

	errs = append(errs, validation.ValidateAnnotations(work.Annotations, fldPath.Child("annotations"))...)
	errs = append(errs, validation.ValidateFinalizers(work.Finalizers, fldPath.Child("finalizers"))...)
	errs = append(errs, metav1validation.ValidateLabels(work.Labels, fldPath.Child("labels"))...)

	if err := validator.ManifestValidator.ValidateManifests(work.Spec.Workload.Manifests); err != nil {
		errs = append(errs, field.Invalid(field.NewPath("spec"), "spec", err.Error()))
	}

	return errs
}

// Encode ensures the given work's manifests are encoded
func Encode(work *workv1.ManifestWork) error {
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

// CompareSnowflakeSequenceIDs compares two snowflake sequence IDs.
// Returns true if the current ID is greater than the last.
// If the last sequence ID is empty, then the current is greater.
func CompareSnowflakeSequenceIDs(last, current string) (bool, error) {
	if current != "" && last == "" {
		return true, nil
	}

	lastSID, err := snowflake.ParseString(last)
	if err != nil {
		return false, fmt.Errorf("unable to parse last sequence ID: %s, %v", last, err)
	}

	currentSID, err := snowflake.ParseString(current)
	if err != nil {
		return false, fmt.Errorf("unable to parse current sequence ID: %s %v", current, err)
	}

	if currentSID.Node() != lastSID.Node() {
		return false, fmt.Errorf("sequence IDs (%s,%s) are not from the same node", last, current)
	}

	if currentSID.Time() != lastSID.Time() {
		return currentSID.Time() > lastSID.Time(), nil
	}

	return currentSID.Step() > lastSID.Step(), nil
}
