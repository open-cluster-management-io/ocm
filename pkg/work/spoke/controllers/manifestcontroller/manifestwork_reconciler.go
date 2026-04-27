package manifestcontroller

import (
	"context"
	"errors"
	"fmt"
	"sort"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
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

// resourceApplyOrder defines the priority rank for applying resources by kind.
// Lower rank means applied first. Resources of the same rank preserve their spec order
// (stable sort). Kinds not listed here are applied after all listed kinds (rank 1000),
// preserving their relative spec order; nil/unparseable manifests sort last (rank 1001).
//
// Rank 0: Cluster-scoped foundations — no dependencies on other resources
// Rank 1: RBAC — needed before workloads that reference service accounts
// Rank 2: Namespace-scoped policies and identity — must exist before workloads
// Rank 3: Config and storage — referenced by workloads as volumes or env
// Rank 4: Networking — services, ingress, etc.
// Rank 5: Workloads — pods, deployments, jobs, etc.
// Rank 6: Post-workload — webhooks and API aggregation that may depend on running workloads
var resourceApplyOrder = map[string]int{
	// Rank 0: cluster-scoped, no deps
	"PriorityClass":            0,
	"Namespace":                0,
	"CustomResourceDefinition": 0,
	"StorageClass":             0,
	"PersistentVolume":         0,
	"PodSecurityPolicy":        0,

	// Rank 1: RBAC
	"ClusterRole":        1,
	"ClusterRoleBinding": 1,
	"Role":               1,
	"RoleBinding":        1,

	// Rank 2: namespace-scoped policies and identity
	"NetworkPolicy":       2,
	"ResourceQuota":       2,
	"LimitRange":          2,
	"PodDisruptionBudget": 2,
	"ServiceAccount":      2,

	// Rank 3: config and storage
	"Secret":                3,
	"ConfigMap":             3,
	"PersistentVolumeClaim": 3,

	// Rank 4: networking
	"Service": 4,

	// Rank 5: workloads
	"DaemonSet":               5,
	"Pod":                     5,
	"ReplicationController":   5,
	"ReplicaSet":              5,
	"Deployment":              5,
	"HorizontalPodAutoscaler": 5,
	"StatefulSet":             5,
	"Job":                     5,
	"CronJob":                 5,

	// Rank 6: post-workload
	"APIService":                       6,
	"MutatingWebhookConfiguration":     6,
	"ValidatingWebhookConfiguration":   6,
	"ValidatingAdmissionPolicy":        6,
	"ValidatingAdmissionPolicyBinding": 6,
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

// orderedManifest holds a pre-parsed manifest together with its original position in the spec.
// Manifests are sorted by resource kind for apply ordering, while specIndex preserves the
// original position for ordinal tracking and status updates.
type orderedManifest struct {
	specIndex    int
	obj          *unstructured.Unstructured
	gvr          schema.GroupVersionResource
	resourceMeta workapiv1.ManifestResourceMeta
	err          error
}

func (m *manifestworkReconciler) parseAndSortManifests(manifests []workapiv1.Manifest) []orderedManifest {
	ordered := make([]orderedManifest, len(manifests))
	for i, manifest := range manifests {
		obj := &unstructured.Unstructured{}
		if err := obj.UnmarshalJSON(manifest.Raw); err != nil {
			ordered[i] = orderedManifest{specIndex: i, resourceMeta: workapiv1.ManifestResourceMeta{
				Ordinal: int32(i), //nolint:gosec
			}, err: err}
			continue
		}

		resMeta, gvr, err := helper.BuildResourceMeta(i, obj, m.restMapper)
		ordered[i] = orderedManifest{specIndex: i, obj: obj, gvr: gvr, resourceMeta: resMeta, err: err}
	}

	sort.SliceStable(ordered, func(i, j int) bool {
		return kindOrder(ordered[i].obj) < kindOrder(ordered[j].obj)
	})

	return ordered
}

func kindOrder(obj *unstructured.Unstructured) int {
	if obj == nil {
		return 1001
	}
	if order, ok := resourceApplyOrder[obj.GetKind()]; ok {
		return order
	}
	return 1000
}

func (m *manifestworkReconciler) applyManifests(
	ctx context.Context,
	manifests []workapiv1.Manifest,
	workSpec workapiv1.ManifestWorkSpec,
	workStatus workapiv1.ManifestWorkStatus,
	recorder events.Recorder,
	owner metav1.OwnerReference,
	existingResults []applyResult) []applyResult {

	ordered := m.parseAndSortManifests(manifests)

	for _, om := range ordered {
		needsApply := existingResults[om.specIndex].Result == nil || apierrors.IsConflict(existingResults[om.specIndex].Error)
		if !needsApply {
			continue
		}
		if om.err != nil {
			existingResults[om.specIndex] = applyResult{Error: om.err, resourceMeta: om.resourceMeta}
		} else {
			existingResults[om.specIndex] = m.applyOneManifest(ctx, om, workSpec, workStatus, recorder, owner)
		}
	}

	return existingResults
}

func (m *manifestworkReconciler) applyOneManifest(
	ctx context.Context,
	om orderedManifest,
	workSpec workapiv1.ManifestWorkSpec,
	workStatus workapiv1.ManifestWorkStatus,
	recorder events.Recorder,
	owner metav1.OwnerReference) applyResult {
	logger := klog.FromContext(ctx)
	result := applyResult{resourceMeta: om.resourceMeta}

	// ignore the required object UID to avoid UID precondition failed error
	if len(om.obj.GetUID()) != 0 {
		logger.Info("Ignore the UID for the manifest index", "uid", om.obj.GetUID(), "index", om.specIndex)
		om.obj.SetUID("")
	}

	// manifests with the Complete condition are not updated
	manifestCondition := helper.FindManifestCondition(om.resourceMeta, workStatus.ResourceStatus.Manifests)
	if manifestCondition != nil {
		if meta.IsStatusConditionTrue(manifestCondition.Conditions, workapiv1.ManifestComplete) {
			result.Result = om.obj
			return result
		}
	}

	// check if the resource to be applied should be owned by the manifest work
	ownedByTheWork := helper.OwnedByTheWork(om.gvr, om.resourceMeta.Namespace, om.resourceMeta.Name, workSpec.DeleteOption)

	// check the Executor subject permission before applying
	err := m.validator.Validate(ctx, workSpec.Executor, om.gvr, om.resourceMeta.Namespace, om.resourceMeta.Name, ownedByTheWork, om.obj)
	if err != nil {
		result.Error = err
		return result
	}

	// compute required ownerrefs based on delete option
	requiredOwner := manageOwnerRef(ownedByTheWork, owner)

	// find update strategy option.
	option := helper.FindManifestConfiguration(om.resourceMeta, workSpec.ManifestConfigs)
	// strategy is updated by default
	strategy := workapiv1.UpdateStrategy{Type: workapiv1.UpdateStrategyTypeUpdate}
	if option != nil && option.UpdateStrategy != nil {
		strategy = *option.UpdateStrategy
	}

	applier := m.appliers.GetApplier(strategy.Type)
	result.Result, result.Error = applier.Apply(ctx, om.gvr, om.obj, requiredOwner, option, recorder)

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
		// Check if this is an ignoreFields processing error
		reason := workapiv1.AppliedManifestFailed
		message := fmt.Sprintf("Failed to apply manifest: %v", result.Error)

		// Use type-safe error detection for IgnoreFieldError
		var ignoreFieldErr *apply.IgnoreFieldError
		if errors.As(result.Error, &ignoreFieldErr) {
			reason = workapiv1.AppliedManifestSSAIgnoreFieldError
			message = fmt.Sprintf("Failed to process ignoreFields: %v", result.Error)
		}

		return metav1.Condition{
			Type:               workapiv1.ManifestApplied,
			Status:             metav1.ConditionFalse,
			Reason:             reason,
			Message:            message,
			ObservedGeneration: generation,
		}
	}

	return metav1.Condition{
		Type:               workapiv1.ManifestApplied,
		Status:             metav1.ConditionTrue,
		Reason:             workapiv1.AppliedManifestComplete,
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
