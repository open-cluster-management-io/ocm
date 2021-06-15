package manifestcontroller

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	workv1client "open-cluster-management.io/api/client/work/clientset/versioned/typed/work/v1"
	workinformer "open-cluster-management.io/api/client/work/informers/externalversions/work/v1"
	worklister "open-cluster-management.io/api/client/work/listers/work/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"

	"open-cluster-management.io/work/pkg/helper"
	"open-cluster-management.io/work/pkg/spoke/controllers"
)

var ResyncInterval = 5 * time.Minute

// ManifestWorkController is to reconcile the workload resources
// fetched from hub cluster on spoke cluster.
type ManifestWorkController struct {
	manifestWorkClient        workv1client.ManifestWorkInterface
	manifestWorkLister        worklister.ManifestWorkNamespaceLister
	appliedManifestWorkClient workv1client.AppliedManifestWorkInterface
	appliedManifestWorkLister worklister.AppliedManifestWorkLister
	spokeDynamicClient        dynamic.Interface
	spokeKubeclient           kubernetes.Interface
	spokeAPIExtensionClient   apiextensionsclient.Interface
	hubHash                   string
	restMapper                meta.RESTMapper
}

// NewManifestWorkController returns a ManifestWorkController
func NewManifestWorkController(
	ctx context.Context,
	recorder events.Recorder,
	spokeDynamicClient dynamic.Interface,
	spokeKubeClient kubernetes.Interface,
	spokeAPIExtensionClient apiextensionsclient.Interface,
	manifestWorkClient workv1client.ManifestWorkInterface,
	manifestWorkInformer workinformer.ManifestWorkInformer,
	manifestWorkLister worklister.ManifestWorkNamespaceLister,
	appliedManifestWorkClient workv1client.AppliedManifestWorkInterface,
	appliedManifestWorkInformer workinformer.AppliedManifestWorkInformer,
	hubHash string,
	restMapper meta.RESTMapper) factory.Controller {

	controller := &ManifestWorkController{
		manifestWorkClient:        manifestWorkClient,
		manifestWorkLister:        manifestWorkLister,
		appliedManifestWorkClient: appliedManifestWorkClient,
		appliedManifestWorkLister: appliedManifestWorkInformer.Lister(),
		spokeDynamicClient:        spokeDynamicClient,
		spokeKubeclient:           spokeKubeClient,
		spokeAPIExtensionClient:   spokeAPIExtensionClient,
		hubHash:                   hubHash,
		restMapper:                restMapper,
	}

	return factory.New().
		WithInformersQueueKeyFunc(func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			return accessor.GetName()
		}, manifestWorkInformer.Informer()).
		WithInformersQueueKeyFunc(helper.AppliedManifestworkQueueKeyFunc(hubHash), appliedManifestWorkInformer.Informer()).
		WithSync(controller.sync).ResyncEvery(ResyncInterval).ToController("ManifestWorkAgent", recorder)
}

// sync is the main reconcile loop for manifest work. It is triggered in two scenarios
// 1. ManifestWork API changes
// 2. Resources defined in manifest changed on spoke
func (m *ManifestWorkController) sync(ctx context.Context, controllerContext factory.SyncContext) error {
	manifestWorkName := controllerContext.QueueKey()
	klog.V(4).Infof("Reconciling ManifestWork %q", manifestWorkName)

	manifestWork, err := m.manifestWorkLister.Get(manifestWorkName)
	if errors.IsNotFound(err) {
		// work not found, could have been deleted, do nothing.
		return nil
	}
	if err != nil {
		return err
	}
	manifestWork = manifestWork.DeepCopy()

	// no work to do if we're deleted
	if !manifestWork.DeletionTimestamp.IsZero() {
		return nil
	}

	// don't do work if the finalizer is not present
	// it ensures all maintained resources will be cleaned once manifestwork is deleted
	found := false
	for i := range manifestWork.Finalizers {
		if manifestWork.Finalizers[i] == controllers.ManifestWorkFinalizer {
			found = true
			break
		}
	}
	if !found {
		return nil
	}
	// Apply appliedManifestWork
	appliedManifestWorkName := fmt.Sprintf("%s-%s", m.hubHash, manifestWork.Name)
	appliedManifestWork, err := m.appliedManifestWorkLister.Get(appliedManifestWorkName)
	switch {
	case errors.IsNotFound(err):
		appliedManifestWork = &workapiv1.AppliedManifestWork{
			ObjectMeta: metav1.ObjectMeta{
				Name:       appliedManifestWorkName,
				Finalizers: []string{controllers.AppliedManifestWorkFinalizer},
			},
			Spec: workapiv1.AppliedManifestWorkSpec{
				HubHash:          m.hubHash,
				ManifestWorkName: manifestWorkName,
			},
		}
		appliedManifestWork, err = m.appliedManifestWorkClient.Create(ctx, appliedManifestWork, metav1.CreateOptions{})
		if err != nil {
			return err
		}
	case err != nil:
		return err
	}
	owner := metav1.NewControllerRef(appliedManifestWork, workapiv1.GroupVersion.WithKind("AppliedManifestWork"))

	errs := []error{}
	// Apply resources on spoke cluster.
	resourceResults := m.applyManifest(manifestWork.Spec.Workload.Manifests, controllerContext.Recorder(), *owner)
	newManifestConditions := []workapiv1.ManifestCondition{}
	for index, result := range resourceResults {
		if result.Error != nil {
			errs = append(errs, result.Error)
		}

		resourceMeta, err := buildManifestResourceMeta(index, result.Result, manifestWork.Spec.Workload.Manifests[index], m.restMapper)
		if err != nil {
			errs = append(errs, err)
		}

		manifestCondition := workapiv1.ManifestCondition{
			ResourceMeta: resourceMeta,
			Conditions:   []metav1.Condition{},
		}

		// Add applied status condition
		manifestCondition.Conditions = append(manifestCondition.Conditions, buildAppliedStatusCondition(result.Error))

		newManifestConditions = append(newManifestConditions, manifestCondition)
	}

	// Update work status
	_, _, err = helper.UpdateManifestWorkStatus(
		ctx, m.manifestWorkClient, manifestWork.Name, m.generateUpdateStatusFunc(newManifestConditions))
	if err != nil {
		errs = append(errs, fmt.Errorf("Failed to update work status with err %w", err))
	}
	if len(errs) > 0 {
		err = utilerrors.NewAggregate(errs)
		klog.Errorf("Reconcile work %s fails with err: %v", manifestWorkName, err)
	}
	return err
}

func (m *ManifestWorkController) applyManifest(manifests []workapiv1.Manifest, recorder events.Recorder, owner metav1.OwnerReference) []resourceapply.ApplyResult {
	clientHolder := resourceapply.NewClientHolder().
		WithAPIExtensionsClient(m.spokeAPIExtensionClient).
		WithKubernetes(m.spokeKubeclient).
		WithDynamicClient(m.spokeDynamicClient)

	// Using index as the file name and apply manifests
	indexstrings := []string{}
	for i := 0; i < len(manifests); i++ {
		indexstrings = append(indexstrings, strconv.Itoa(i))
	}
	results := resourceapply.ApplyDirectly(clientHolder, recorder, func(name string) ([]byte, error) {
		index, _ := strconv.ParseInt(name, 10, 32)
		unstructuredObj := &unstructured.Unstructured{}
		err := unstructuredObj.UnmarshalJSON(manifests[index].Raw)
		if err != nil {
			return nil, err
		}
		unstructuredObj.SetOwnerReferences([]metav1.OwnerReference{owner})
		return unstructuredObj.MarshalJSON()
	}, indexstrings...)

	// Try apply with dynamic client if the manifest cannot be decoded by scheme or typed client is not found
	// TODO we should check the certain error.
	for index, result := range results {
		// Use dynamic client when scheme cannot decode manifest or typed client cannot handle the object
		if isDecodeError(result.Error) || isUnhandledError(result.Error) || isUnsupportedError(result.Error) {
			results[index].Result, results[index].Changed, results[index].Error = m.applyUnstructrued(manifests[index].Raw, owner, recorder)
		}
	}
	return results
}

func (m *ManifestWorkController) decodeUnstructured(data []byte) (schema.GroupVersionResource, *unstructured.Unstructured, error) {
	unstructuredObj := &unstructured.Unstructured{}
	err := unstructuredObj.UnmarshalJSON(data)
	if err != nil {
		return schema.GroupVersionResource{}, nil, fmt.Errorf("Failed to decode object: %w", err)
	}
	mapping, err := m.restMapper.RESTMapping(unstructuredObj.GroupVersionKind().GroupKind(), unstructuredObj.GroupVersionKind().Version)
	if err != nil {
		return schema.GroupVersionResource{}, nil, fmt.Errorf("Failed to find gvr from restmapping: %w", err)
	}

	return mapping.Resource, unstructuredObj, nil
}

func (m *ManifestWorkController) applyUnstructrued(data []byte, owner metav1.OwnerReference, recorder events.Recorder) (*unstructured.Unstructured, bool, error) {
	gvr, required, err := m.decodeUnstructured(data)
	if err != nil {
		return nil, false, err
	}

	required.SetOwnerReferences([]metav1.OwnerReference{owner})
	existing, err := m.spokeDynamicClient.
		Resource(gvr).
		Namespace(required.GetNamespace()).
		Get(context.TODO(), required.GetName(), metav1.GetOptions{})
	if errors.IsNotFound(err) {
		actual, err := m.spokeDynamicClient.Resource(gvr).Namespace(required.GetNamespace()).Create(
			context.TODO(), required, metav1.CreateOptions{})
		recorder.Eventf(fmt.Sprintf(
			"%s Created", required.GetKind()), "Created %s/%s because it was missing", required.GetNamespace(), required.GetName())
		return actual, true, err
	}
	if err != nil {
		return nil, false, err
	}

	// Compare and update the unstrcuctured.
	if isSameUnstructured(required, existing) {
		return existing, false, nil
	}
	required.SetResourceVersion(existing.GetResourceVersion())
	actual, err := m.spokeDynamicClient.Resource(gvr).Namespace(required.GetNamespace()).Update(
		context.TODO(), required, metav1.UpdateOptions{})
	recorder.Eventf(fmt.Sprintf(
		"%s Updated", required.GetKind()), "Updated %s/%s", required.GetNamespace(), required.GetName())
	return actual, true, err
}

// generateUpdateStatusFunc returns a function which aggregates manifest conditions and generates work conditions.
// Rules to generate work status conditions from manifest conditions
// #1: Applied - work status condition (with type Applied) is applied if all manifest conditions (with type Applied) are applied
// TODO: add rules for other condition types, like Progressing, Available, Degraded
func (m *ManifestWorkController) generateUpdateStatusFunc(newManifestConditions []workapiv1.ManifestCondition) helper.UpdateManifestWorkStatusFunc {
	return func(oldStatus *workapiv1.ManifestWorkStatus) error {
		// merge the new manifest conditions with the existing manifest conditions
		oldStatus.ResourceStatus.Manifests = helper.MergeManifestConditions(oldStatus.ResourceStatus.Manifests, newManifestConditions)

		// aggregate manifest condition to generate work condition
		newConditions := []metav1.Condition{}

		// handle condition type Applied
		if inCondition, exists := allInCondition(string(workapiv1.ManifestApplied), newManifestConditions); exists {
			appliedCondition := metav1.Condition{
				Type: workapiv1.WorkApplied,
			}
			if inCondition {
				appliedCondition.Status = metav1.ConditionTrue
				appliedCondition.Reason = "AppliedManifestWorkComplete"
				appliedCondition.Message = "Apply manifest work complete"
			} else {
				appliedCondition.Status = metav1.ConditionFalse
				appliedCondition.Reason = "AppliedManifestWorkFailed"
				appliedCondition.Message = "Failed to apply manifest work"
			}
			newConditions = append(newConditions, appliedCondition)
		}

		oldStatus.Conditions = helper.MergeStatusConditions(oldStatus.Conditions, newConditions)
		return nil
	}
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

	// Compare label and annotations
	if !equality.Semantic.DeepEqual(obj1Copy.GetLabels(), obj2Copy.GetLabels()) {
		return false
	}
	if !equality.Semantic.DeepEqual(obj1Copy.GetAnnotations(), obj2Copy.GetAnnotations()) {
		return false
	}

	// Compare semantically after removing metadata and status field
	delete(obj1Copy.Object, "metadata")
	delete(obj2Copy.Object, "metadata")
	delete(obj1Copy.Object, "status")
	delete(obj2Copy.Object, "status")

	return equality.Semantic.DeepEqual(obj1Copy.Object, obj2Copy.Object)
}

// allInCondition checks status of conditions with a particular type in ManifestCondition array.
// Return true only if conditions with the condition type exist and they are all in condition.
func allInCondition(conditionType string, manifests []workapiv1.ManifestCondition) (inCondition bool, exists bool) {
	for _, manifest := range manifests {
		for _, condition := range manifest.Conditions {
			if condition.Type == conditionType {
				exists = true
			}

			if condition.Type == conditionType && condition.Status == metav1.ConditionFalse {
				return false, true
			}
		}
	}

	return exists, exists
}

func buildAppliedStatusCondition(err error) metav1.Condition {
	if err != nil {
		return metav1.Condition{
			Type:    string(workapiv1.ManifestApplied),
			Status:  metav1.ConditionFalse,
			Reason:  "AppliedManifestFailed",
			Message: fmt.Sprintf("Failed to apply manifest: %v", err),
		}
	}

	return metav1.Condition{
		Type:    string(workapiv1.ManifestApplied),
		Status:  metav1.ConditionTrue,
		Reason:  "AppliedManifestComplete",
		Message: "Apply manifest complete",
	}
}

// buildManifestResourceMeta returns resource meta for manifest. It tries to get the resource
// meta from the result object in ApplyResult struct. If the resource meta is incompleted, fall
// back to manifest template for the meta info.
func buildManifestResourceMeta(index int, object runtime.Object, manifest workapiv1.Manifest, restMapper meta.RESTMapper) (resourceMeta workapiv1.ManifestResourceMeta, err error) {
	errs := []error{}

	resourceMeta, err = buildResourceMeta(index, object, restMapper)
	if err != nil {
		errs = append(errs, err)
	} else if len(resourceMeta.Kind) > 0 && len(resourceMeta.Version) > 0 && len(resourceMeta.Name) > 0 {
		return resourceMeta, nil
	}

	// try to get resource meta from manifest if the one got from apply result is incompleted
	switch {
	case manifest.Object != nil:
		object = manifest.Object
	default:
		unstructuredObj := &unstructured.Unstructured{}
		if err = unstructuredObj.UnmarshalJSON(manifest.Raw); err != nil {
			errs = append(errs, err)
			return resourceMeta, utilerrors.NewAggregate(errs)
		}
		object = unstructuredObj
	}
	resourceMeta, err = buildResourceMeta(index, object, restMapper)
	if err == nil {
		return resourceMeta, nil
	}

	return resourceMeta, utilerrors.NewAggregate(errs)
}

func buildResourceMeta(index int, object runtime.Object, restMapper meta.RESTMapper) (resourceMeta workapiv1.ManifestResourceMeta, err error) {
	resourceMeta = workapiv1.ManifestResourceMeta{
		Ordinal: int32(index),
	}

	if object == nil || reflect.ValueOf(object).IsNil() {
		return resourceMeta, err
	}

	// set gvk
	gvk, err := helper.GuessObjectGroupVersionKind(object)
	if err != nil {
		return resourceMeta, err
	}
	resourceMeta.Group = gvk.Group
	resourceMeta.Version = gvk.Version
	resourceMeta.Kind = gvk.Kind

	// set namespace/name
	if accessor, e := meta.Accessor(object); e != nil {
		err = fmt.Errorf("cannot access metadata of %v: %w", object, e)
	} else {
		resourceMeta.Namespace = accessor.GetNamespace()
		resourceMeta.Name = accessor.GetName()
	}

	// set resource
	if restMapper == nil {
		return resourceMeta, err
	}
	mapping, err := restMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return resourceMeta, fmt.Errorf("the server doesn't have a resource type %q", gvk.Kind)
	}

	resourceMeta.Resource = mapping.Resource.Resource
	return resourceMeta, err
}
