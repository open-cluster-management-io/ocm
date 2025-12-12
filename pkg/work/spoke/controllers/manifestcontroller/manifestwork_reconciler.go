package manifestcontroller

import (
	"context"
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"

	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/events"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"

	commonhelper "open-cluster-management.io/ocm/pkg/common/helpers"
	"open-cluster-management.io/ocm/pkg/work/helper"
	"open-cluster-management.io/ocm/pkg/work/spoke/apply"
	"open-cluster-management.io/ocm/pkg/work/spoke/auth"
	"open-cluster-management.io/ocm/pkg/work/spoke/auth/basic"
)

type applyResult struct {
	Result runtime.Object
	Error  error

	resourceMeta workapiv1.ManifestResourceMeta
}

type manifestworkReconciler struct {
	restMapper meta.RESTMapper
	appliers   *apply.Appliers
	validator  auth.ExecutorValidator
}

func (m *manifestworkReconciler) reconcile(
	ctx context.Context,
	controllerContext factory.SyncContext,
	manifestWork *workapiv1.ManifestWork,
	appliedManifestWork *workapiv1.AppliedManifestWork,
	_ []applyResult) (*workapiv1.ManifestWork, *workapiv1.AppliedManifestWork, []applyResult, error) {
	logger := klog.FromContext(ctx)
	// We creat a ownerref instead of controller ref since multiple controller can declare the ownership of a manifests
	owner := helper.NewAppliedManifestWorkOwner(appliedManifestWork)

	var errs []error
	// Apply resources on spoke cluster.
	resourceResults := make([]applyResult, len(manifestWork.Spec.Workload.Manifests))
	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		resourceResults = m.applyManifests(
			ctx, manifestWork.Spec.Workload.Manifests, manifestWork.Spec, manifestWork.Status, controllerContext.Recorder(), *owner, resourceResults)

		for _, result := range resourceResults {
			if apierrors.IsConflict(result.Error) {
				return result.Error
			}
		}

		return nil
	})
	if err != nil {
		logger.Error(err, "failed to apply resource")
	}

	var newManifestConditions []workapiv1.ManifestCondition
	var requeueTime = ResyncInterval
	for _, result := range resourceResults {
		manifestCondition := workapiv1.ManifestCondition{
			ResourceMeta: result.resourceMeta,
			Conditions:   []metav1.Condition{},
		}

		// Add applied status condition
		manifestCondition.Conditions = append(manifestCondition.Conditions, buildAppliedStatusCondition(result, manifestWork.Generation))

		newManifestConditions = append(newManifestConditions, manifestCondition)

		// If it is a forbidden error, after the condition is constructed, we set the error to nil
		// and requeue the item
		var authError *basic.NotAllowedError
		if errors.As(result.Error, &authError) {
			logger.V(2).Info("apply work failed", "error", result.Error)
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

	if len(errs) > 0 {
		err = utilerrors.NewAggregate(errs)
	} else if requeueTime != ResyncInterval {
		err = commonhelper.NewRequeueError(
			fmt.Sprintf("requeue work %s due to authorization error", manifestWork.Name),
			requeueTime,
		)
	}

	return manifestWork, appliedManifestWork, resourceResults, err
}

func (m *manifestworkReconciler) applyManifests(
	ctx context.Context,
	manifests []workapiv1.Manifest,
	workSpec workapiv1.ManifestWorkSpec,
	workStatus workapiv1.ManifestWorkStatus,
	recorder events.Recorder,
	owner metav1.OwnerReference,
	existingResults []applyResult) []applyResult {

	for index, manifest := range manifests {
		switch {
		case existingResults[index].Result == nil:
			// Apply if there is no result.
			existingResults[index] = m.applyOneManifest(ctx, index, manifest, workSpec, workStatus, recorder, owner)
		case apierrors.IsConflict(existingResults[index].Error):
			// Apply if there is a resource conflict error.
			existingResults[index] = m.applyOneManifest(ctx, index, manifest, workSpec, workStatus, recorder, owner)
		}
	}

	return existingResults
}

func (m *manifestworkReconciler) applyOneManifest(
	ctx context.Context,
	index int,
	manifest workapiv1.Manifest,
	workSpec workapiv1.ManifestWorkSpec,
	workStatus workapiv1.ManifestWorkStatus,
	recorder events.Recorder,
	owner metav1.OwnerReference) applyResult {
	logger := klog.FromContext(ctx)
	result := applyResult{}

	// parse the required and set resource meta
	required := &unstructured.Unstructured{}
	if err := required.UnmarshalJSON(manifest.Raw); err != nil {
		result.Error = err
		return result
	}

	// ignore the required object UID to avoid UID precondition failed error
	if len(required.GetUID()) != 0 {
		logger.Info("Ignore the UID for the manifest index", "uid", required.GetUID(), "index", index)
		required.SetUID("")
	}

	resMeta, gvr, err := helper.BuildResourceMeta(index, required, m.restMapper)
	result.resourceMeta = resMeta
	if err != nil {
		result.Error = err
		return result
	}

	// manifests with the Complete condition are not updated
	manifestCondition := helper.FindManifestCondition(resMeta, workStatus.ResourceStatus.Manifests)
	if manifestCondition != nil {
		if meta.IsStatusConditionTrue(manifestCondition.Conditions, workapiv1.ManifestComplete) {
			result.Result = required
			return result
		}
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
	option := helper.FindManifestConfiguration(resMeta, workSpec.ManifestConfigs)
	// strategy is updated by default
	strategy := workapiv1.UpdateStrategy{Type: workapiv1.UpdateStrategyTypeUpdate}
	if option != nil && option.UpdateStrategy != nil {
		strategy = *option.UpdateStrategy
	}

	applier := m.appliers.GetApplier(strategy.Type)
	result.Result, result.Error = applier.Apply(ctx, gvr, required, requiredOwner, option, recorder)

	return result
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

func buildAppliedStatusCondition(result applyResult, generation int64) metav1.Condition {
	if result.Error != nil {
		return metav1.Condition{
			Type:               workapiv1.ManifestApplied,
			Status:             metav1.ConditionFalse,
			Reason:             "AppliedManifestFailed",
			Message:            fmt.Sprintf("Failed to apply manifest: %v", result.Error),
			ObservedGeneration: generation,
		}
	}

	return metav1.Condition{
		Type:               workapiv1.ManifestApplied,
		Status:             metav1.ConditionTrue,
		Reason:             "AppliedManifestComplete",
		Message:            "Apply manifest complete",
		ObservedGeneration: generation,
	}
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
