package statuscontroller

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"

	workv1client "open-cluster-management.io/api/client/work/clientset/versioned/typed/work/v1"
	workinformer "open-cluster-management.io/api/client/work/informers/externalversions/work/v1"
	worklister "open-cluster-management.io/api/client/work/listers/work/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"
	"open-cluster-management.io/sdk-go/pkg/logging"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	commonhelper "open-cluster-management.io/ocm/pkg/common/helpers"
	"open-cluster-management.io/ocm/pkg/common/queue"
	"open-cluster-management.io/ocm/pkg/work/helper"
	"open-cluster-management.io/ocm/pkg/work/spoke/conditions"
	"open-cluster-management.io/ocm/pkg/work/spoke/objectreader"
	"open-cluster-management.io/ocm/pkg/work/spoke/statusfeedback"
)

const (
	statusFeedbackConditionType = "StatusFeedbackSynced"

	controllerName = "AvailableStatusController"
)

// AvailableStatusController is to update the available status conditions of both manifests and manifestworks.
// It is also used to get the status value based on status feedback configuration and get condition values
// based on condition rules in manifestwork. The functions are logically disinct, however, they are put in the same
// control loop to reduce live get call to spoke apiserver and status update call to hub apiserver.
type AvailableStatusController struct {
	patcher            patcher.Patcher[*workapiv1.ManifestWork, workapiv1.ManifestWorkSpec, workapiv1.ManifestWorkStatus]
	manifestWorkLister worklister.ManifestWorkNamespaceLister
	objectReader       objectreader.ObjectReader
	statusReader       *statusfeedback.StatusReader
	conditionReader    *conditions.ConditionReader
	syncInterval       time.Duration
}

// NewAvailableStatusController returns a AvailableStatusController
func NewAvailableStatusController(
	manifestWorkClient workv1client.ManifestWorkInterface,
	manifestWorkInformer workinformer.ManifestWorkInformer,
	manifestWorkLister worklister.ManifestWorkNamespaceLister,
	conditionReader *conditions.ConditionReader,
	objectReader objectreader.ObjectReader,
	maxJSONRawLength int32,
	syncInterval time.Duration,
) factory.Controller {
	controller := &AvailableStatusController{
		patcher: patcher.NewPatcher[
			*workapiv1.ManifestWork, workapiv1.ManifestWorkSpec, workapiv1.ManifestWorkStatus](
			manifestWorkClient),
		manifestWorkLister: manifestWorkLister,
		syncInterval:       syncInterval,
		objectReader:       objectReader,
		statusReader:       statusfeedback.NewStatusReader().WithMaxJsonRawLength(maxJSONRawLength),
		conditionReader:    conditionReader,
	}

	return factory.New().
		WithInformersQueueKeysFunc(queue.QueueKeyByMetaName, manifestWorkInformer.Informer()).
		WithSync(controller.sync).ToController(controllerName)
}

func (c *AvailableStatusController) sync(ctx context.Context, controllerContext factory.SyncContext, manifestWorkName string) error {
	logger := klog.FromContext(ctx).WithValues("manifestWorkName", manifestWorkName)
	logger.V(4).Info("Reconciling ManifestWork")

	// sync a particular manifestwork
	manifestWork, err := c.manifestWorkLister.Get(manifestWorkName)
	if errors.IsNotFound(err) {
		// work not found, could have been deleted, do nothing.
		return nil
	}
	if err != nil {
		return fmt.Errorf("unable to fetch manifestwork %q: %w", manifestWorkName, err)
	}

	// set tracing key from work if there is any
	logger = logging.SetLogTracingByObject(logger, manifestWork)
	ctx = klog.NewContext(ctx, logger)

	err = c.syncManifestWork(ctx, controllerContext, manifestWork)
	if err != nil {
		return fmt.Errorf("unable to sync manifestwork %q: %w", manifestWork.Name, err)
	}

	// requeue with a certain jitter
	controllerContext.Queue().AddAfter(manifestWorkName, wait.Jitter(c.syncInterval, 0.9))
	return nil
}

func (c *AvailableStatusController) syncManifestWork(ctx context.Context, controllerContext factory.SyncContext, originalManifestWork *workapiv1.ManifestWork) error {
	manifestWork := originalManifestWork.DeepCopy()

	// do nothing when finalizer is not added.
	if !commonhelper.HasFinalizer(manifestWork.Finalizers, workapiv1.ManifestWorkFinalizer) {
		return nil
	}

	// wait until work has the applied condition.
	if cond := meta.FindStatusCondition(manifestWork.Status.Conditions, workapiv1.WorkApplied); cond == nil {
		return nil
	}

	// handle status condition of manifests
	// TODO revist this controller since this might bring races when user change the manifests in spec.
	for index, manifest := range manifestWork.Status.ResourceStatus.Manifests {
		obj, availableStatusCondition, err := c.objectReader.Get(ctx, manifest.ResourceMeta)
		manifestConditions := &manifestWork.Status.ResourceStatus.Manifests[index].Conditions
		meta.SetStatusCondition(manifestConditions, availableStatusCondition)
		if err != nil {
			// skip getting status values if resource is not available.
			continue
		}

		option := helper.FindManifestConfiguration(manifest.ResourceMeta, manifestWork.Spec.ManifestConfigs)
		if option != nil && option.FeedbackScrapeType == workapiv1.FeedbackWatchType {
			if err := c.objectReader.RegisterInformer(ctx, manifestWork.Name, manifest.ResourceMeta, controllerContext.Queue()); err != nil {
				utilruntime.HandleErrorWithContext(ctx, err, "failed to register informer")
			}
		} else {
			if err := c.objectReader.UnRegisterInformer(manifestWork.Name, manifest.ResourceMeta); err != nil {
				utilruntime.HandleErrorWithContext(ctx, err, "failed to unregister informer")
			}
		}

		// Read status of the resource according to feedback rules.
		values, statusFeedbackCondition := c.getFeedbackValues(obj, option)
		valuesChanged := !equality.Semantic.DeepEqual(manifest.StatusFeedbacks.Values, values)
		if valuesChanged {
			meta.RemoveStatusCondition(manifestConditions, statusFeedbackCondition.Type)
		}
		meta.SetStatusCondition(manifestConditions, statusFeedbackCondition)
		manifestWork.Status.ResourceStatus.Manifests[index].StatusFeedbacks.Values = values

		// Update manifest conditions according to condition rules
		c.evaluateConditionRules(ctx, manifestConditions, obj, option, manifestWork.Generation)
	}

	// aggregate condtions generated by rules and update work conditions
	aggregatedConditions := conditions.AggregateManifestConditions(manifestWork.Generation, manifestWork.Status.ResourceStatus.Manifests)
	for _, condition := range aggregatedConditions {
		meta.SetStatusCondition(&manifestWork.Status.Conditions, condition)
	}
	conditions.PruneConditionsGeneratedByConditionRules(&manifestWork.Status.Conditions, manifestWork.Generation)

	// no work if the status of manifestwork does not change
	if equality.Semantic.DeepEqual(originalManifestWork.Status.ResourceStatus, manifestWork.Status.ResourceStatus) &&
		equality.Semantic.DeepEqual(originalManifestWork.Status.Conditions, manifestWork.Status.Conditions) {
		return nil
	}

	// update status of manifestwork. if this conflicts, try again later
	_, err := c.patcher.PatchStatus(ctx, manifestWork, manifestWork.Status, originalManifestWork.Status)
	return err
}

func (c *AvailableStatusController) getFeedbackValues(
	obj *unstructured.Unstructured,
	option *workapiv1.ManifestConfigOption) ([]workapiv1.FeedbackValue, metav1.Condition) {
	var errs []error
	var values []workapiv1.FeedbackValue

	if option == nil || len(option.FeedbackRules) == 0 {
		return values, metav1.Condition{
			Type:   statusFeedbackConditionType,
			Reason: "NoStatusFeedbackSynced",
			Status: metav1.ConditionTrue,
		}
	}

	for _, rule := range option.FeedbackRules {
		valuesByRule, err := c.statusReader.GetValuesByRule(obj, rule)
		if err != nil {
			errs = append(errs, err)
		}
		if len(valuesByRule) > 0 {
			values = append(values, valuesByRule...)
		}
	}

	err := utilerrors.NewAggregate(errs)

	if err != nil {
		return values, metav1.Condition{
			Type:    statusFeedbackConditionType,
			Reason:  "StatusFeedbackSyncFailed",
			Status:  metav1.ConditionFalse,
			Message: fmt.Sprintf("Sync status feedback failed with error %v", err),
		}
	}

	return values, metav1.Condition{
		Type:   statusFeedbackConditionType,
		Reason: "StatusFeedbackSynced",
		Status: metav1.ConditionTrue,
	}
}

// evaluateConditionRules updates manifestConditions based on configured condition rules for the manifest
func (c *AvailableStatusController) evaluateConditionRules(ctx context.Context,
	manifestConditions *[]metav1.Condition, obj *unstructured.Unstructured, option *workapiv1.ManifestConfigOption, generation int64,
) {
	// Evaluate rules
	var newConditions []metav1.Condition
	if option != nil && len(option.ConditionRules) > 0 {
		newConditions = c.conditionReader.EvaluateConditions(ctx, obj, option.ConditionRules)
	}

	// Update manifest conditions with latest condition rule results and observed generation
	for _, condition := range newConditions {
		condition.ObservedGeneration = generation
		meta.SetStatusCondition(manifestConditions, condition)
	}

	// Remove conditions set by old rules that no longer exist
	// Conditions are merged into the existing slice because they are managed in multiple controllers
	// (e.g. manifestwork_reconciler adds the "Applied" condition), so we must explicitly
	// delete conditions that were created by rules which no longer exist.
	conditions.PruneConditionsGeneratedByConditionRules(manifestConditions, generation)
}
