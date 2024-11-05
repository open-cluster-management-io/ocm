package apply

import (
	"context"
	"crypto/md5"
	"fmt"
	"io"
	"reflect"
	"strings"

	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/resource/resourcemerge"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/pointer"

	workapiv1 "open-cluster-management.io/api/work/v1"
)

// TODO onlyUpdateFromMW and objHashAnnotation need to be surfaced in the API
var onlyUpdateFromMW = true

const objHashAnnotation = "open-cluster-management.io/object-hash"

type UpdateApply struct {
	dynamicClient       dynamic.Interface
	kubeclient          kubernetes.Interface
	apiExtensionClient  apiextensionsclient.Interface
	staticResourceCache resourceapply.ResourceCache
}

func NewUpdateApply(dynamicClient dynamic.Interface, kubeclient kubernetes.Interface, apiExtensionClient apiextensionsclient.Interface) *UpdateApply {
	return &UpdateApply{
		dynamicClient:      dynamicClient,
		kubeclient:         kubeclient,
		apiExtensionClient: apiExtensionClient,
		// TODO we did not gc resources in cache, which may cause more memory usage. It
		// should be refactored using own cache implementation in the future.
		staticResourceCache: resourceapply.NewResourceCache(),
	}
}

func (c *UpdateApply) Apply(
	ctx context.Context,
	gvr schema.GroupVersionResource,
	required *unstructured.Unstructured,
	owner metav1.OwnerReference,
	_ *workapiv1.ManifestConfigOption,
	recorder events.Recorder) (runtime.Object, error) {

	clientHolder := resourceapply.NewClientHolder().
		WithAPIExtensionsClient(c.apiExtensionClient).
		WithKubernetes(c.kubeclient).
		WithDynamicClient(c.dynamicClient)

	required.SetOwnerReferences([]metav1.OwnerReference{owner})
	cache := c.staticResourceCache
	if onlyUpdateFromMW {
		objHash := hashOfResourceStruct(required)
		annotations := required.GetAnnotations()
		if annotations == nil {
			annotations = map[string]string{}
		}
		annotations["open-cluster-management.io/object-hash"] = objHash
		fmt.Printf("object hash is %v\n", objHash)
		required.SetAnnotations(annotations)
		cache = &objectHashCache{}
	}

	results := resourceapply.ApplyDirectly(ctx, clientHolder, recorder, cache, func(name string) ([]byte, error) {
		return required.MarshalJSON()
	}, "manifest")

	obj, err := results[0].Result, results[0].Error

	// Try apply with dynamic client if the manifest cannot be decoded by scheme or typed client is not found
	// TODO we should check the certain error.
	// Use dynamic client when scheme cannot decode manifest or typed client cannot handle the object
	if isDecodeError(err) || isUnhandledError(err) || isUnsupportedError(err) {
		obj, _, err = c.applyUnstructured(ctx, required, gvr, recorder, cache)
	}

	if err == nil && (!reflect.ValueOf(obj).IsValid() || reflect.ValueOf(obj).IsNil()) {
		// ApplyDirectly may return a nil Result when there is no error, we get the latest object for the Result
		return c.dynamicClient.
			Resource(gvr).
			Namespace(required.GetNamespace()).
			Get(ctx, required.GetName(), metav1.GetOptions{})
	}
	return obj, err
}

func (c *UpdateApply) applyUnstructured(
	ctx context.Context,
	required *unstructured.Unstructured,
	gvr schema.GroupVersionResource,
	recorder events.Recorder,
	cache resourceapply.ResourceCache) (*unstructured.Unstructured, bool, error) {
	existing, err := c.dynamicClient.
		Resource(gvr).
		Namespace(required.GetNamespace()).
		Get(ctx, required.GetName(), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		actual, err := c.dynamicClient.Resource(gvr).Namespace(required.GetNamespace()).Create(
			ctx, resourcemerge.WithCleanLabelsAndAnnotations(required).(*unstructured.Unstructured), metav1.CreateOptions{})
		recorder.Eventf(fmt.Sprintf(
			"%s Created", required.GetKind()), "Created %s/%s because it was missing", required.GetNamespace(), required.GetName())
		return actual, true, err
	}

	if err != nil {
		return nil, false, err
	}

	requiredCopy := required.DeepCopy()
	if cache.SafeToSkipApply(requiredCopy, existing) {
		return existing, false, nil
	}

	// Merge OwnerRefs, Labels, and Annotations.
	existingOwners := existing.GetOwnerReferences()
	existingLabels := existing.GetLabels()
	existingAnnotations := existing.GetAnnotations()
	modified := pointer.Bool(false)

	resourcemerge.MergeMap(modified, &existingLabels, required.GetLabels())
	resourcemerge.MergeMap(modified, &existingAnnotations, required.GetAnnotations())
	resourcemerge.MergeOwnerRefs(modified, &existingOwners, required.GetOwnerReferences())

	// Always overwrite required from existing, since required has been merged to existing
	required.SetOwnerReferences(existingOwners)
	required.SetLabels(existingLabels)
	required.SetAnnotations(existingAnnotations)

	// Keep the finalizers unchanged
	required.SetFinalizers(existing.GetFinalizers())

	// Compare and update the unstrcuctured.
	if !*modified && isSameUnstructured(required, existing) {
		cache.UpdateCachedResourceMetadata(requiredCopy, existing)
		return existing, false, nil
	}
	required.SetResourceVersion(existing.GetResourceVersion())
	actual, err := c.dynamicClient.Resource(gvr).Namespace(required.GetNamespace()).Update(
		ctx, required, metav1.UpdateOptions{})
	recorder.Eventf(fmt.Sprintf(
		"%s Updated", required.GetKind()), "Updated %s/%s", required.GetNamespace(), required.GetName())
	cache.UpdateCachedResourceMetadata(requiredCopy, actual)
	return actual, true, err
}

// isDecodeError is to check if the error returned from resourceapply is due to that the object cannot
// be decoded or no typed client can handle the object.
func isDecodeError(err error) bool {
	return err != nil && strings.HasPrefix(err.Error(), "cannot decode")
}

// isUnhandledError is to check if the error returned from resourceapply is due to that no typed
// client can handle the object
func isUnhandledError(err error) bool {
	return err != nil && strings.HasPrefix(err.Error(), "unhandled type")
}

// isUnsupportedError is to check if the error returned from resourceapply is due to
// the PR https://github.com/openshift/library-go/pull/1042
func isUnsupportedError(err error) bool {
	return err != nil && strings.HasPrefix(err.Error(), "unsupported object type")
}

// isSameUnstructured compares the two unstructured object.
// The comparison ignores the metadata and status field, and check if the two objects are semantically equal.
func isSameUnstructured(obj1, obj2 *unstructured.Unstructured) bool {
	obj1Copy := obj1.DeepCopy()
	obj2Copy := obj2.DeepCopy()

	// Compare gvk, name, namespace at first
	if obj1Copy.GroupVersionKind() != obj2Copy.GroupVersionKind() {
		return false
	}
	if obj1Copy.GetName() != obj2Copy.GetName() {
		return false
	}
	if obj1Copy.GetNamespace() != obj2Copy.GetNamespace() {
		return false
	}

	// Compare semantically after removing metadata and status field
	delete(obj1Copy.Object, "metadata")
	delete(obj2Copy.Object, "metadata")
	delete(obj1Copy.Object, "status")
	delete(obj2Copy.Object, "status")

	return equality.Semantic.DeepEqual(obj1Copy.Object, obj2Copy.Object)
}

// detect changes in a resource by caching a hash of the string representation of the resource
// note: some changes in a resource e.g. nil vs empty, will not be detected this way
func hashOfResourceStruct(o interface{}) string {
	oString := fmt.Sprintf("%v", o)
	h := md5.New()
	io.WriteString(h, oString)
	rval := fmt.Sprintf("%x", h.Sum(nil))
	return rval
}

type objectHashCache struct{}

func (c *objectHashCache) UpdateCachedResourceMetadata(_ runtime.Object, _ runtime.Object) {
	return
}

func (c *objectHashCache) SafeToSkipApply(required runtime.Object, existing runtime.Object) bool {
	requiredAccessor, err := meta.Accessor(required)
	if err != nil {
		return true
	}
	// existing could be nil in some cases.
	if existing == nil {
		return false
	}
	existingAccessor, err := meta.Accessor(existing)
	if err != nil {
		return true
	}
	fmt.Printf("2 existing object is %v\n", required)
	if len(requiredAccessor.GetAnnotations()) == 0 {
		return true
	}
	requiredObjHash := requiredAccessor.GetAnnotations()[objHashAnnotation]
	fmt.Printf("existing object is %v\n", existing)
	if len(existingAccessor.GetAnnotations()) == 0 {
		// Always update since existing does not have the annotation yet
		return false
	}
	existingObjHash := existingAccessor.GetAnnotations()[objHashAnnotation]
	return requiredObjHash == existingObjHash
}
