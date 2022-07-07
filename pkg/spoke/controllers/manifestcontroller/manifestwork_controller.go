package manifestcontroller

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"k8s.io/utils/pointer"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/resource/resourcemerge"
	"github.com/pkg/errors"
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
	staticResourceCache       resourceapply.ResourceCache
}

type applyResult struct {
	resourceapply.ApplyResult

	resourceMeta workapiv1.ManifestResourceMeta
}

type serverSideApplyConflictError struct {
	ssaErr error
}

func (e *serverSideApplyConflictError) Error() string {
	return e.ssaErr.Error()
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
		// TODO we did not gc resources in cache, which may cause more memory usage. It
		// should be refactored using own cache implementation in the future.
		staticResourceCache: resourceapply.NewResourceCache(),
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
	if apierrors.IsNotFound(err) {
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
	case apierrors.IsNotFound(err):
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

	// We creat a ownerref instead of controller ref since multiple controller can declare the ownership of a manifests
	owner := helper.NewAppliedManifestWorkOwner(appliedManifestWork)

	errs := []error{}
	// Apply resources on spoke cluster.
	resourceResults := make([]applyResult, len(manifestWork.Spec.Workload.Manifests))
	err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		resourceResults = m.applyManifests(
			ctx, manifestWork.Spec.Workload.Manifests, manifestWork.Spec, controllerContext.Recorder(), *owner, resourceResults)

		for _, result := range resourceResults {
			if apierrors.IsConflict(result.Error) {
				return result.Error
			}
		}

		return nil
	})
	if err != nil {
		klog.Errorf("failed to apply resource with error %v", err)
	}

	newManifestConditions := []workapiv1.ManifestCondition{}
	for _, result := range resourceResults {
		// ignore server side apply conflict error since it cannot be resolved by error fallback.
		var ssaConflict *serverSideApplyConflictError
		if result.Error != nil && !errors.As(result.Error, &ssaConflict) {
			errs = append(errs, result.Error)
		}

		manifestCondition := workapiv1.ManifestCondition{
			ResourceMeta: result.resourceMeta,
			Conditions:   []metav1.Condition{},
		}

		// Add applied status condition
		manifestCondition.Conditions = append(manifestCondition.Conditions, buildAppliedStatusCondition(result))

		newManifestConditions = append(newManifestConditions, manifestCondition)
	}

	// Update work status
	_, _, err = helper.UpdateManifestWorkStatus(
		ctx, m.manifestWorkClient, manifestWork, m.generateUpdateStatusFunc(manifestWork.Generation, newManifestConditions))
	if err != nil {
		errs = append(errs, fmt.Errorf("failed to update work status with err %w", err))
	}
	if len(errs) > 0 {
		err = utilerrors.NewAggregate(errs)
		klog.Errorf("Reconcile work %s fails with err: %v", manifestWorkName, err)
	}
	return err
}

func (m *ManifestWorkController) applyManifests(
	ctx context.Context,
	manifests []workapiv1.Manifest,
	workSpec workapiv1.ManifestWorkSpec,
	recorder events.Recorder,
	owner metav1.OwnerReference,
	existingResults []applyResult) []applyResult {

	for index, manifest := range manifests {
		switch {
		case existingResults[index].Result == nil:
			// Apply if there is not result.
			existingResults[index] = m.applyOneManifest(ctx, index, manifest, workSpec, recorder, owner)
		case apierrors.IsConflict(existingResults[index].Error):
			// Apply if there is a resource confilct error.
			existingResults[index] = m.applyOneManifest(ctx, index, manifest, workSpec, recorder, owner)
		}
	}

	return existingResults
}

func (m *ManifestWorkController) applyOneManifest(
	ctx context.Context,
	index int,
	manifest workapiv1.Manifest,
	workSpec workapiv1.ManifestWorkSpec,
	recorder events.Recorder,
	owner metav1.OwnerReference) applyResult {

	clientHolder := resourceapply.NewClientHolder().
		WithAPIExtensionsClient(m.spokeAPIExtensionClient).
		WithKubernetes(m.spokeKubeclient).
		WithDynamicClient(m.spokeDynamicClient)

	result := applyResult{}

	// parse the required and set resource meta
	required := &unstructured.Unstructured{}
	if err := required.UnmarshalJSON(manifest.Raw); err != nil {
		result.Error = err
		return result
	}

	resMeta, gvr, err := buildResourceMeta(index, required, m.restMapper)
	result.resourceMeta = resMeta
	if err != nil {
		result.Error = err
		return result
	}

	// try to get the existing at first.
	existing, existingErr := m.spokeDynamicClient.
		Resource(gvr).
		Namespace(required.GetNamespace()).
		Get(ctx, required.GetName(), metav1.GetOptions{})
	if existingErr != nil && !apierrors.IsNotFound(existingErr) {
		result.Error = existingErr
		return result
	}

	// compute required ownerrefs based on delete option
	requiredOwner := manageOwnerRef(gvr, resMeta.Namespace, resMeta.Name, workSpec.DeleteOption, owner)

	// find update strategy option.
	option := helper.FindManifestConiguration(resMeta, workSpec.ManifestConfigs)
	// strategy is update by default
	strategy := workapiv1.UpdateStrategy{Type: workapiv1.UpdateStrategyTypeUpdate}
	if option != nil && option.UpdateStrategy != nil {
		strategy = *option.UpdateStrategy
	}

	// apply resource based on update strategy
	switch strategy.Type {
	case workapiv1.UpdateStrategyTypeServerSideApply:
		result.Result, result.Changed, result.Error = m.serverSideApply(ctx, gvr, required, option.UpdateStrategy.ServerSideApply, recorder)
	case workapiv1.UpdateStrategyTypeCreateOnly, workapiv1.UpdateStrategyTypeUpdate:
		if strategy.Type == workapiv1.UpdateStrategyTypeCreateOnly && existingErr == nil {
			result.Result = existing
			break
		}
		required.SetOwnerReferences([]metav1.OwnerReference{requiredOwner})
		results := resourceapply.ApplyDirectly(ctx, clientHolder, recorder, m.staticResourceCache, func(name string) ([]byte, error) {
			return required.MarshalJSON()
		}, "manifest")

		result.Result = results[0].Result
		result.Changed = results[0].Changed
		result.Error = results[0].Error

		// Try apply with dynamic client if the manifest cannot be decoded by scheme or typed client is not found
		// TODO we should check the certain error.
		// Use dynamic client when scheme cannot decode manifest or typed client cannot handle the object
		if isDecodeError(result.Error) || isUnhandledError(result.Error) || isUnsupportedError(result.Error) {
			result.Result, result.Changed, result.Error = m.applyUnstructured(ctx, existing, required, gvr, recorder)
		}
	}

	// patch the ownerref no matter apply fails or not.
	if result.Error == nil {
		result.Error = helper.ApplyOwnerReferences(ctx, m.spokeDynamicClient, gvr, result.Result, requiredOwner)
	}

	return result
}

func (m *ManifestWorkController) serverSideApply(
	ctx context.Context,
	gvr schema.GroupVersionResource,
	required *unstructured.Unstructured,
	config *workapiv1.ServerSideApplyConfig,
	recorder events.Recorder) (*unstructured.Unstructured, bool, error) {
	force := false
	fieldManager := workapiv1.DefaultFieldManager

	if config != nil {
		force = config.Force
		if len(config.FieldManager) > 0 {
			fieldManager = config.FieldManager
		}
	}

	patch, err := json.Marshal(resourcemerge.WithCleanLabelsAndAnnotations(required))
	if err != nil {
		return nil, false, err
	}

	// TODO use Apply method instead when upgrading the client-go to 0.25.x
	actual, err := m.spokeDynamicClient.
		Resource(gvr).
		Namespace(required.GetNamespace()).
		Patch(ctx, required.GetName(), types.ApplyPatchType, patch, metav1.PatchOptions{FieldManager: fieldManager, Force: pointer.Bool(force)})
	resourceKey, _ := cache.MetaNamespaceKeyFunc(required)
	recorder.Eventf(fmt.Sprintf(
		"Server Side Applied %s %s", required.GetKind(), resourceKey), "Patched with field manager %s", fieldManager)

	if apierrors.IsConflict(err) {
		return actual, true, &serverSideApplyConflictError{ssaErr: err}
	}

	return actual, true, err
}

func (m *ManifestWorkController) applyUnstructured(
	ctx context.Context,
	existing, required *unstructured.Unstructured,
	gvr schema.GroupVersionResource,
	recorder events.Recorder) (*unstructured.Unstructured, bool, error) {
	if existing == nil {
		actual, err := m.spokeDynamicClient.Resource(gvr).Namespace(required.GetNamespace()).Create(
			ctx, resourcemerge.WithCleanLabelsAndAnnotations(required).(*unstructured.Unstructured), metav1.CreateOptions{})
		recorder.Eventf(fmt.Sprintf(
			"%s Created", required.GetKind()), "Created %s/%s because it was missing", required.GetNamespace(), required.GetName())
		return actual, true, err
	}

	// Merge OwnerRefs, Labels, and Annotations.
	existingOwners := existing.GetOwnerReferences()
	existingLabels := existing.GetLabels()
	existingAnnotations := existing.GetAnnotations()
	modified := resourcemerge.BoolPtr(false)

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
		return existing, false, nil
	}
	required.SetResourceVersion(existing.GetResourceVersion())
	actual, err := m.spokeDynamicClient.Resource(gvr).Namespace(required.GetNamespace()).Update(
		ctx, required, metav1.UpdateOptions{})
	recorder.Eventf(fmt.Sprintf(
		"%s Updated", required.GetKind()), "Updated %s/%s", required.GetNamespace(), required.GetName())
	return actual, true, err
}

// manageOwnerRef return a ownerref based on the resource and the deleteOption indicating whether the owneref
// should be removed or added. If the resource is orphaned, the owner's UID is updated for removal.
func manageOwnerRef(
	gvr schema.GroupVersionResource,
	namespace, name string,
	deleteOption *workapiv1.DeleteOption,
	myOwner metav1.OwnerReference) metav1.OwnerReference {

	// Be default, it is forgound deletion.
	if deleteOption == nil {
		return myOwner
	}

	removalKey := fmt.Sprintf("%s-", myOwner.UID)
	ownerCopy := myOwner.DeepCopy()

	switch deleteOption.PropagationPolicy {
	case workapiv1.DeletePropagationPolicyTypeForeground:
		return myOwner
	case workapiv1.DeletePropagationPolicyTypeOrphan:
		ownerCopy.UID = types.UID(removalKey)
		return *ownerCopy
	}

	// If there is none specified selectivelyOrphan, none of the manifests should be orphaned
	if deleteOption.SelectivelyOrphan == nil {
		return myOwner
	}

	for _, o := range deleteOption.SelectivelyOrphan.OrphaningRules {
		if o.Group != gvr.Group {
			continue
		}

		if o.Resource != gvr.Resource {
			continue
		}

		if o.Name != name {
			continue
		}

		if o.Namespace != namespace {
			continue
		}

		ownerCopy.UID = types.UID(removalKey)
		return *ownerCopy
	}

	return myOwner
}

// generateUpdateStatusFunc returns a function which aggregates manifest conditions and generates work conditions.
// Rules to generate work status conditions from manifest conditions
// #1: Applied - work status condition (with type Applied) is applied if all manifest conditions (with type Applied) are applied
// TODO: add rules for other condition types, like Progressing, Available, Degraded
func (m *ManifestWorkController) generateUpdateStatusFunc(generation int64, newManifestConditions []workapiv1.ManifestCondition) helper.UpdateManifestWorkStatusFunc {
	return func(oldStatus *workapiv1.ManifestWorkStatus) error {
		// merge the new manifest conditions with the existing manifest conditions
		oldStatus.ResourceStatus.Manifests = helper.MergeManifestConditions(oldStatus.ResourceStatus.Manifests, newManifestConditions)

		// aggregate manifest condition to generate work condition
		newConditions := []metav1.Condition{}

		// handle condition type Applied
		if inCondition, exists := allInCondition(string(workapiv1.ManifestApplied), newManifestConditions); exists {
			appliedCondition := metav1.Condition{
				Type:               workapiv1.WorkApplied,
				ObservedGeneration: generation,
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

func buildAppliedStatusCondition(result applyResult) metav1.Condition {
	if result.Error != nil {
		return metav1.Condition{
			Type:    string(workapiv1.ManifestApplied),
			Status:  metav1.ConditionFalse,
			Reason:  "AppliedManifestFailed",
			Message: fmt.Sprintf("Failed to apply manifest: %v", result.Error),
		}
	}

	return metav1.Condition{
		Type:    string(workapiv1.ManifestApplied),
		Status:  metav1.ConditionTrue,
		Reason:  "AppliedManifestComplete",
		Message: "Apply manifest complete",
	}
}

func buildResourceMeta(
	index int,
	object runtime.Object,
	restMapper meta.RESTMapper) (workapiv1.ManifestResourceMeta, schema.GroupVersionResource, error) {
	resourceMeta := workapiv1.ManifestResourceMeta{
		Ordinal: int32(index),
	}

	if object == nil || reflect.ValueOf(object).IsNil() {
		return resourceMeta, schema.GroupVersionResource{}, nil
	}

	// set gvk
	gvk, err := helper.GuessObjectGroupVersionKind(object)
	if err != nil {
		return resourceMeta, schema.GroupVersionResource{}, err
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
		return resourceMeta, schema.GroupVersionResource{}, err
	}
	mapping, err := restMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return resourceMeta, schema.GroupVersionResource{}, fmt.Errorf("the server doesn't have a resource type %q", gvk.Kind)
	}

	resourceMeta.Resource = mapping.Resource.Resource
	return resourceMeta, mapping.Resource, err
}
