package utils

import (
	"encoding/json"
	"fmt"

	"github.com/bwmarrin/snowflake"
	jsonpatch "github.com/evanphx/json-patch"
	"github.com/google/uuid"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/validation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metav1validation "k8s.io/apimachinery/pkg/apis/meta/v1/validation"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/common"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic"
)

// Patch applies the given patch to a `generic.ResourceObject` using the specified patch type.
//
// Parameters:
// - patchType: The type of patch to apply (JSONPatchType, MergePatchType and StrategicMergePatchType are supported).
// - original: The resource object to be patched.
// - patchData: The raw patch data.
//
// Returns:
// - The patched resource object.
// - An error if the patching fails at any step.
//
// Notes on StrategicMergePatch:
//   - Strategic Merge Patch (SMP) is a Kubernetes-specific patch type.
//   - It relies on **struct tags** (e.g., `patchStrategy` and `patchMergeKey`) defined in the Go types
//     of Kubernetes API objects to determine how to merge lists and maps (e.g., merge by key instead of replacing).
//   - SMP **only works** on known Kubernetes built-in API types (e.g., corev1.Pod) that have these metadata tags.
//   - It will **fail or behave incorrectly** if used on CRDs or custom types that don’t have the necessary tags.
func Patch[T generic.ResourceObject](patchType types.PatchType, original T, patchData []byte) (resource T, err error) {
	originalData, err := json.Marshal(original)
	if err != nil {
		return resource, err
	}

	var patchedData []byte
	switch patchType {
	case types.JSONPatchType:
		var patchObj jsonpatch.Patch
		patchObj, err = jsonpatch.DecodePatch(patchData)
		if err != nil {
			return resource, err
		}
		patchedData, err = patchObj.Apply(originalData)
		if err != nil {
			return resource, err
		}
	case types.MergePatchType:
		patchedData, err = jsonpatch.MergePatch(originalData, patchData)
		if err != nil {
			return resource, err
		}
	case types.StrategicMergePatchType:
		patchedData, err = strategicpatch.StrategicMergePatch(originalData, patchData, original)
		if err != nil {
			return resource, err
		}
	default:
		return resource, fmt.Errorf("unsupported patch type: %s", patchType)
	}

	patchedResource := new(T)
	if err := json.Unmarshal(patchedData, patchedResource); err != nil {
		return resource, err
	}

	return *patchedResource, nil
}

// ListResourcesWithOptions retrieves the resources from store which matches the options.
func ListResourcesWithOptions[T generic.ResourceObject](store cache.Store, namespace string, opts metav1.ListOptions) ([]T, error) {
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

	resources := []T{}
	// list with labels
	if err := cache.ListAll(store, labelSelector, func(obj any) {
		resourceMeta, ok := obj.(metav1.Object)
		if !ok {
			klog.Warningf("the object in store %T is not a meta object", obj)
			return
		}

		if namespace != metav1.NamespaceAll && resourceMeta.GetNamespace() != namespace {
			return
		}

		workFieldSet := fields.Set{
			"metadata.name":      resourceMeta.GetName(),
			"metadata.namespace": resourceMeta.GetNamespace(),
		}

		if !fieldSelector.Matches(workFieldSet) {
			return
		}

		resource, ok := obj.(T)
		if !ok {
			return
		}

		resources = append(resources, resource)
	}); err != nil {
		return nil, err
	}

	return resources, nil
}

// ValidateResourceMetadata validates the metadata of the given resource
func ValidateResourceMetadata[T generic.ResourceObject](resource T) field.ErrorList {
	errs := field.ErrorList{}
	fldPath := field.NewPath("metadata")

	obj, err := meta.Accessor(resource)
	if err != nil {
		errs = append(errs, field.TypeInvalid(fldPath, resource, err.Error()))
		return errs
	}

	if obj.GetUID() == "" {
		errs = append(errs, field.Required(fldPath.Child("uid"), "field not set"))
		return errs
	}

	if obj.GetResourceVersion() == "" {
		errs = append(errs, field.Required(fldPath.Child("resourceVersion"), "field not set"))
		return errs
	}

	if obj.GetName() == "" {
		errs = append(errs, field.Required(fldPath.Child("name"), "field not set"))
		return errs
	}

	for _, msg := range validation.ValidateNamespaceName(obj.GetName(), false) {
		errs = append(errs, field.Invalid(fldPath.Child("name"), obj.GetName(), msg))
	}

	if obj.GetNamespace() != "" {
		for _, msg := range validation.ValidateNamespaceName(obj.GetNamespace(), false) {
			errs = append(errs, field.Invalid(fldPath.Child("namespace"), obj.GetNamespace(), msg))
		}
	}

	errs = append(errs, metav1validation.ValidateLabels(obj.GetLabels(), fldPath.Child("labels"))...)
	errs = append(errs, validation.ValidateAnnotations(obj.GetAnnotations(), fldPath.Child("annotations"))...)
	errs = append(errs, validation.ValidateFinalizers(obj.GetFinalizers(), fldPath.Child("finalizers"))...)
	return errs
}

// ToRuntimeObject convert a resource to a runtime Object
func ToRuntimeObject[T generic.ResourceObject](resource T) (runtime.Object, error) {
	obj, ok := any(resource).(runtime.Object)
	if !ok {
		return nil, fmt.Errorf("object %T does not implement the runtime Object interfaces", resource)
	}

	return obj.DeepCopyObject(), nil
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

// UID returns a v5 UUID based on sourceID, groupResource, namespace and name to make sure it is consistent
func UID(sourceID, groupResource, namespace, name string) string {
	id := fmt.Sprintf("%s-%s-%s-%s", sourceID, groupResource, namespace, name)
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(id)).String()
}

// EnsureResourceFinalizer ensures the resource finalizer in the given finalizers
func EnsureResourceFinalizer(finalizers []string) []string {
	has := false
	for _, f := range finalizers {
		if f == common.ResourceFinalizer {
			has = true
			break
		}
	}

	if !has {
		finalizers = append(finalizers, common.ResourceFinalizer)
	}

	return finalizers
}

func IsStatusPatch(subresources []string) bool {
	if len(subresources) == 0 {
		return false
	}

	if len(subresources) == 1 && subresources[0] == "status" {
		return true
	}

	return false
}
