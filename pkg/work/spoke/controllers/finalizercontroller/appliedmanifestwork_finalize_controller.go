package finalizercontroller

import (
	"context"
	"fmt"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/api/errors"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	workv1client "open-cluster-management.io/api/client/work/clientset/versioned/typed/work/v1"
	workinformer "open-cluster-management.io/api/client/work/informers/externalversions/work/v1"
	worklister "open-cluster-management.io/api/client/work/listers/work/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	"open-cluster-management.io/ocm/pkg/common/queue"
	"open-cluster-management.io/ocm/pkg/work/helper"
)

// AppliedManifestWorkFinalizeController handles cleanup of appliedmanifestwork resources before deletion is allowed.
// It should handle all appliedmanifestworks belonging to this agent identified by the agentID.
type AppliedManifestWorkFinalizeController struct {
	patcher                   patcher.Patcher[*workapiv1.AppliedManifestWork, workapiv1.AppliedManifestWorkSpec, workapiv1.AppliedManifestWorkStatus]
	appliedManifestWorkLister worklister.AppliedManifestWorkLister
	spokeDynamicClient        dynamic.Interface
	rateLimiter               workqueue.RateLimiter
}

func NewAppliedManifestWorkFinalizeController(
	recorder events.Recorder,
	spokeDynamicClient dynamic.Interface,
	appliedManifestWorkClient workv1client.AppliedManifestWorkInterface,
	appliedManifestWorkInformer workinformer.AppliedManifestWorkInformer,
	agentID string,
) factory.Controller {

	controller := &AppliedManifestWorkFinalizeController{
		patcher: patcher.NewPatcher[
			*workapiv1.AppliedManifestWork, workapiv1.AppliedManifestWorkSpec, workapiv1.AppliedManifestWorkStatus](
			appliedManifestWorkClient),
		appliedManifestWorkLister: appliedManifestWorkInformer.Lister(),
		spokeDynamicClient:        spokeDynamicClient,
		rateLimiter:               workqueue.NewItemExponentialFailureRateLimiter(5*time.Millisecond, 1000*time.Second),
	}

	return factory.New().
		WithFilteredEventsInformersQueueKeysFunc(queue.QueueKeyByMetaName,
			helper.AppliedManifestworkAgentIDFilter(agentID), appliedManifestWorkInformer.Informer()).
		WithSync(controller.sync).ToController("AppliedManifestWorkFinalizer", recorder)
}

func (m *AppliedManifestWorkFinalizeController) sync(ctx context.Context, controllerContext factory.SyncContext) error {
	appliedManifestWorkName := controllerContext.QueueKey()
	klog.V(4).Infof("Reconciling AppliedManifestWork %q", appliedManifestWorkName)

	appliedManifestWork, err := m.appliedManifestWorkLister.Get(appliedManifestWorkName)
	if errors.IsNotFound(err) {
		// work not found, could have been deleted, do nothing.
		return nil
	}
	if err != nil {
		return err
	}
	return m.syncAppliedManifestWork(ctx, controllerContext, appliedManifestWork)
}

// syncAppliedManifestWork ensures that when an appliedmanifestwork has been deleted, everything it created is also deleted.
// Foreground deletion is implemented, which means all resources created will be deleted and finalized
// before removing finalizer from appliedmanifestwork
func (m *AppliedManifestWorkFinalizeController) syncAppliedManifestWork(ctx context.Context,
	controllerContext factory.SyncContext, originalManifestWork *workapiv1.AppliedManifestWork) error {
	appliedManifestWork := originalManifestWork.DeepCopy()

	// no work to do until we're deleted
	if appliedManifestWork.DeletionTimestamp == nil || appliedManifestWork.DeletionTimestamp.IsZero() {
		return nil
	}

	// don't do work if the finalizer is not present
	if !helper.HasFinalizer(appliedManifestWork.Finalizers, workapiv1.AppliedManifestWorkFinalizer) {
		return nil
	}

	owner := helper.NewAppliedManifestWorkOwner(appliedManifestWork)

	// Work is deleting, we remove its related resources on spoke cluster
	// We still need to run delete for every resource even with ownerref on it, since ownerref does not handle cluster
	// scoped resource correctly.
	reason := fmt.Sprintf("manifestwork %s is terminating", appliedManifestWork.Spec.ManifestWorkName)
	resourcesPendingFinalization, errs := helper.DeleteAppliedResources(
		ctx, appliedManifestWork.Status.AppliedResources, reason, m.spokeDynamicClient, controllerContext.Recorder(), *owner)
	appliedManifestWork.Status.AppliedResources = resourcesPendingFinalization
	updatedAppliedManifestWork, err := m.patcher.PatchStatus(ctx, appliedManifestWork, appliedManifestWork.Status, originalManifestWork.Status)
	if err != nil {
		errs = append(errs, fmt.Errorf(
			"failed to update status of AppliedManifestWork %s: %w", originalManifestWork.Name, err))
	}

	// return quickly when there is update event or err
	if updatedAppliedManifestWork || len(errs) != 0 {
		return utilerrors.NewAggregate(errs)
	}

	// requeue the work until all applied resources are deleted and finalized if the appliedmanifestwork itself is not updated
	if len(resourcesPendingFinalization) != 0 {
		klog.V(4).Infof("%d resources pending deletions", len(resourcesPendingFinalization))
		controllerContext.Queue().AddAfter(appliedManifestWork.Name, m.rateLimiter.When(appliedManifestWork.Name))
		return nil
	}

	// reset the rate limiter for the appliedmanifestwork
	m.rateLimiter.Forget(appliedManifestWork.Name)

	if err := m.patcher.RemoveFinalizer(ctx, appliedManifestWork, workapiv1.AppliedManifestWorkFinalizer); err != nil {
		return fmt.Errorf("failed to remove finalizer from AppliedManifestWork %s: %w", appliedManifestWork.Name, err)
	}
	return nil
}
