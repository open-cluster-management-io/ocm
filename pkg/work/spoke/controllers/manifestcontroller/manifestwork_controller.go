package manifestcontroller

import (
	"context"
	"fmt"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/pkg/errors"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"

	workv1client "open-cluster-management.io/api/client/work/clientset/versioned/typed/work/v1"
	workinformer "open-cluster-management.io/api/client/work/informers/externalversions/work/v1"
	worklister "open-cluster-management.io/api/client/work/listers/work/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	"open-cluster-management.io/ocm/pkg/common/queue"
	"open-cluster-management.io/ocm/pkg/work/helper"
	"open-cluster-management.io/ocm/pkg/work/spoke/apply"
	"open-cluster-management.io/ocm/pkg/work/spoke/auth"
	"open-cluster-management.io/ocm/pkg/work/spoke/auth/basic"
)

var (
	ResyncInterval     = 5 * time.Minute
	MaxRequeueDuration = 24 * time.Hour
)

// ManifestWorkController is to reconcile the workload resources
// fetched from hub cluster on spoke cluster.
type ManifestWorkController struct {
	manifestWorkPatcher        patcher.Patcher[*workapiv1.ManifestWork, workapiv1.ManifestWorkSpec, workapiv1.ManifestWorkStatus]
	manifestWorkLister         worklister.ManifestWorkNamespaceLister
	appliedManifestWorkClient  workv1client.AppliedManifestWorkInterface
	appliedManifestWorkPatcher patcher.Patcher[*workapiv1.AppliedManifestWork, workapiv1.AppliedManifestWorkSpec, workapiv1.AppliedManifestWorkStatus]
	appliedManifestWorkLister  worklister.AppliedManifestWorkLister
	spokeDynamicClient         dynamic.Interface
	hubHash                    string
	agentID                    string
	restMapper                 meta.RESTMapper
	appliers                   *apply.Appliers
	validator                  auth.ExecutorValidator
}

type applyResult struct {
	Result runtime.Object
	Error  error

	resourceMeta workapiv1.ManifestResourceMeta
}

// NewManifestWorkController returns a ManifestWorkController
func NewManifestWorkController(
	recorder events.Recorder,
	spokeDynamicClient dynamic.Interface,
	spokeKubeClient kubernetes.Interface,
	spokeAPIExtensionClient apiextensionsclient.Interface,
	manifestWorkClient workv1client.ManifestWorkInterface,
	manifestWorkInformer workinformer.ManifestWorkInformer,
	manifestWorkLister worklister.ManifestWorkNamespaceLister,
	appliedManifestWorkClient workv1client.AppliedManifestWorkInterface,
	appliedManifestWorkInformer workinformer.AppliedManifestWorkInformer,
	hubHash, agentID string,
	restMapper meta.RESTMapper,
	validator auth.ExecutorValidator) factory.Controller {

	controller := &ManifestWorkController{
		manifestWorkPatcher: patcher.NewPatcher[
			*workapiv1.ManifestWork, workapiv1.ManifestWorkSpec, workapiv1.ManifestWorkStatus](
			manifestWorkClient),
		manifestWorkLister:        manifestWorkLister,
		appliedManifestWorkClient: appliedManifestWorkClient,
		appliedManifestWorkPatcher: patcher.NewPatcher[
			*workapiv1.AppliedManifestWork, workapiv1.AppliedManifestWorkSpec, workapiv1.AppliedManifestWorkStatus](
			appliedManifestWorkClient),
		appliedManifestWorkLister: appliedManifestWorkInformer.Lister(),
		spokeDynamicClient:        spokeDynamicClient,
		hubHash:                   hubHash,
		agentID:                   agentID,
		restMapper:                restMapper,
		appliers:                  apply.NewAppliers(spokeDynamicClient, spokeKubeClient, spokeAPIExtensionClient),
		validator:                 validator,
	}

	return factory.New().
		WithInformersQueueKeysFunc(queue.QueueKeyByMetaName, manifestWorkInformer.Informer()).
		WithFilteredEventsInformersQueueKeyFunc(
			helper.AppliedManifestworkQueueKeyFunc(hubHash),
			helper.AppliedManifestworkHubHashFilter(hubHash),
			appliedManifestWorkInformer.Informer()).
		WithSync(controller.sync).ResyncEvery(ResyncInterval).ToController("ManifestWorkAgent", recorder)
}

// sync is the main reconcile loop for manifest work. It is triggered in two scenarios
// 1. ManifestWork API changes
// 2. Resources defined in manifest changed on spoke
func (m *ManifestWorkController) sync(ctx context.Context, controllerContext factory.SyncContext) error {
	manifestWorkName := controllerContext.QueueKey()
	klog.V(4).Infof("Reconciling ManifestWork %q", manifestWorkName)

	oldManifestWork, err := m.manifestWorkLister.Get(manifestWorkName)
	if apierrors.IsNotFound(err) {
		// work not found, could have been deleted, do nothing.
		return nil
	}
	if err != nil {
		return err
	}
	manifestWork := oldManifestWork.DeepCopy()

	// no work to do if we're deleted
	if !manifestWork.DeletionTimestamp.IsZero() {
		return nil
	}

	// don't do work if the finalizer is not present
	// it ensures all maintained resources will be cleaned once manifestwork is deleted
	if !helper.HasFinalizer(manifestWork.Finalizers, workapiv1.ManifestWorkFinalizer) {
		return nil
	}

	// Apply appliedManifestWork
	appliedManifestWork, err := m.applyAppliedManifestWork(ctx, manifestWork.Name, m.hubHash, m.agentID)
	if err != nil {
		return err
	}

	// We creat a ownerref instead of controller ref since multiple controller can declare the ownership of a manifests
	owner := helper.NewAppliedManifestWorkOwner(appliedManifestWork)

	var errs []error
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

	var newManifestConditions []workapiv1.ManifestCondition
	var requeueTime = MaxRequeueDuration
	for _, result := range resourceResults {
		manifestCondition := workapiv1.ManifestCondition{
			ResourceMeta: result.resourceMeta,
			Conditions:   []metav1.Condition{},
		}

		// Add applied status condition
		manifestCondition.Conditions = append(manifestCondition.Conditions, buildAppliedStatusCondition(result))

		newManifestConditions = append(newManifestConditions, manifestCondition)

		// If it is a forbidden error, after the condition is constructed, we set the error to nil
		// and requeue the item
		var authError *basic.NotAllowedError
		if errors.As(result.Error, &authError) {
			klog.V(2).Infof("apply work %s fails with err: %v", manifestWorkName, result.Error)
			result.Error = nil

			if authError.RequeueTime < requeueTime {
				requeueTime = authError.RequeueTime
			}
		}

		// ignore server side apply conflict error since it cannot be resolved by error fallback.
		var ssaConflict *apply.ServerSideApplyConflictError
		if result.Error != nil && !errors.As(result.Error, &ssaConflict) {
			errs = append(errs, result.Error)
		}
	}
	manifestWork.Status.ResourceStatus.Manifests = helper.MergeManifestConditions(
		manifestWork.Status.ResourceStatus.Manifests, newManifestConditions)
	// handle condition type Applied
	// #1: Applied - work status condition (with type Applied) is applied if all manifest conditions (with type Applied) are applied
	if inCondition, exists := allInCondition(workapiv1.ManifestApplied, newManifestConditions); exists {
		appliedCondition := metav1.Condition{
			Type:               workapiv1.WorkApplied,
			ObservedGeneration: manifestWork.Generation,
			Status:             metav1.ConditionFalse,
			Reason:             "AppliedManifestWorkFailed",
			Message:            "Failed to apply manifest work",
		}
		if inCondition {
			appliedCondition.Status = metav1.ConditionTrue
			appliedCondition.Reason = "AppliedManifestWorkComplete"
			appliedCondition.Message = "Apply manifest work complete"
		}
		meta.SetStatusCondition(&manifestWork.Status.Conditions, appliedCondition)
	}

	// Update work status
	updated, err := m.manifestWorkPatcher.PatchStatus(ctx, manifestWork, manifestWork.Status, oldManifestWork.Status)
	if err != nil {
		errs = append(errs, fmt.Errorf("failed to update work status with err %w", err))
	}

	if !updated && requeueTime < MaxRequeueDuration {
		controllerContext.Queue().AddAfter(manifestWorkName, requeueTime)
	}

	if len(errs) > 0 {
		err = utilerrors.NewAggregate(errs)
		klog.Errorf("Reconcile work %s fails with err: %v", manifestWorkName, err)
	}

	return err
}

func (m *ManifestWorkController) applyAppliedManifestWork(ctx context.Context, workName, hubHash, agentID string) (*workapiv1.AppliedManifestWork, error) {
	appliedManifestWorkName := fmt.Sprintf("%s-%s", m.hubHash, workName)
	requiredAppliedWork := &workapiv1.AppliedManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:       appliedManifestWorkName,
			Finalizers: []string{workapiv1.AppliedManifestWorkFinalizer},
		},
		Spec: workapiv1.AppliedManifestWorkSpec{
			HubHash:          hubHash,
			ManifestWorkName: workName,
			AgentID:          agentID,
		},
	}

	appliedManifestWork, err := m.appliedManifestWorkLister.Get(appliedManifestWorkName)
	switch {
	case apierrors.IsNotFound(err):
		return m.appliedManifestWorkClient.Create(ctx, requiredAppliedWork, metav1.CreateOptions{})

	case err != nil:
		return nil, err
	}

	_, err = m.appliedManifestWorkPatcher.PatchSpec(ctx, appliedManifestWork, requiredAppliedWork.Spec, appliedManifestWork.Spec)
	return appliedManifestWork, err
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
			// Apply if there is no result.
			existingResults[index] = m.applyOneManifest(ctx, index, manifest, workSpec, recorder, owner)
		case apierrors.IsConflict(existingResults[index].Error):
			// Apply if there is a resource conflict error.
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

	result := applyResult{}

	// parse the required and set resource meta
	required := &unstructured.Unstructured{}
	if err := required.UnmarshalJSON(manifest.Raw); err != nil {
		result.Error = err
		return result
	}

	// ignore the required object UID to avoid UID precondition failed error
	if len(required.GetUID()) != 0 {
		klog.Warningf("Ignore the UID %s for the manifest index %d", required.GetUID(), index)
		required.SetUID("")
	}

	resMeta, gvr, err := helper.BuildResourceMeta(index, required, m.restMapper)
	result.resourceMeta = resMeta
	if err != nil {
		result.Error = err
		return result
	}

	// check if the resource to be applied should be owned by the manifest work
	ownedByTheWork := helper.OwnedByTheWork(gvr, resMeta.Namespace, resMeta.Name, workSpec.DeleteOption)

	// check the Executor subject permission before applying
	err = m.validator.Validate(ctx, workSpec.Executor, gvr, resMeta.Namespace, resMeta.Name, ownedByTheWork, required)
	if err != nil {
		result.Error = err
		return result
	}

	// compute required ownerrefs based on delete option
	requiredOwner := manageOwnerRef(ownedByTheWork, owner)

	// find update strategy option.
	option := helper.FindManifestConiguration(resMeta, workSpec.ManifestConfigs)
	// strategy is update by default
	strategy := workapiv1.UpdateStrategy{Type: workapiv1.UpdateStrategyTypeUpdate}
	if option != nil && option.UpdateStrategy != nil {
		strategy = *option.UpdateStrategy
	}

	applier := m.appliers.GetApplier(strategy.Type)
	result.Result, result.Error = applier.Apply(ctx, gvr, required, requiredOwner, option, recorder)

	// patch the ownerref
	if result.Error == nil {
		result.Error = helper.ApplyOwnerReferences(ctx, m.spokeDynamicClient, gvr, result.Result, requiredOwner)
	}

	return result
}

// manageOwnerRef return a ownerref based on the resource and the ownedByTheWork indicating whether the owneref
// should be removed or added. If the resource is not owned by the work, the owner's UID is updated for removal.
func manageOwnerRef(
	ownedByTheWork bool,
	myOwner metav1.OwnerReference) metav1.OwnerReference {
	if ownedByTheWork {
		return myOwner
	}
	removalKey := fmt.Sprintf("%s-", myOwner.UID)
	ownerCopy := myOwner.DeepCopy()
	ownerCopy.UID = types.UID(removalKey)
	return *ownerCopy
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
			Type:    workapiv1.ManifestApplied,
			Status:  metav1.ConditionFalse,
			Reason:  "AppliedManifestFailed",
			Message: fmt.Sprintf("Failed to apply manifest: %v", result.Error),
		}
	}

	return metav1.Condition{
		Type:    workapiv1.ManifestApplied,
		Status:  metav1.ConditionTrue,
		Reason:  "AppliedManifestComplete",
		Message: "Apply manifest complete",
	}
}
