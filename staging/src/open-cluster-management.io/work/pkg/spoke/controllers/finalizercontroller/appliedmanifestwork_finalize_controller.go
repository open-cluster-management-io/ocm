package finalizercontroller

import (
	"context"
	"fmt"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	workv1client "open-cluster-management.io/api/client/work/clientset/versioned/typed/work/v1"
	workinformer "open-cluster-management.io/api/client/work/informers/externalversions/work/v1"
	worklister "open-cluster-management.io/api/client/work/listers/work/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/work/pkg/helper"
	"open-cluster-management.io/work/pkg/spoke/controllers"
)

// AppliedManifestWorkFinalizeController handles cleanup of appliedmanifestwork resources before deletion is allowed.
// It should handle all appliedmanifestworks belonging to this agent identified by the agentID.
type AppliedManifestWorkFinalizeController struct {
	appliedManifestWorkClient workv1client.AppliedManifestWorkInterface
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
		appliedManifestWorkClient: appliedManifestWorkClient,
		appliedManifestWorkLister: appliedManifestWorkInformer.Lister(),
		spokeDynamicClient:        spokeDynamicClient,
		rateLimiter:               workqueue.NewItemExponentialFailureRateLimiter(5*time.Millisecond, 1000*time.Second),
	}

	return factory.New().
		WithFilteredEventsInformersQueueKeyFunc(func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			return accessor.GetName()
		}, helper.AppliedManifestworkAgentIDFilter(agentID), appliedManifestWorkInformer.Informer()).
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

// syncAppliedManifestWork ensures that when a appliedmanifestwork has been deleted, everything it created is also deleted.
// Foreground deletion is implemented, which means all resources created will be deleted and finalized
// before removing finalizer from appliedmanifestwork
func (m *AppliedManifestWorkFinalizeController) syncAppliedManifestWork(ctx context.Context, controllerContext factory.SyncContext, originalManifestWork *workapiv1.AppliedManifestWork) error {
	appliedManifestWork := originalManifestWork.DeepCopy()

	// no work to do until we're deleted
	if appliedManifestWork.DeletionTimestamp == nil || appliedManifestWork.DeletionTimestamp.IsZero() {
		return nil
	}

	// don't do work if the finalizer is not present
	found := false
	for i := range appliedManifestWork.Finalizers {
		if appliedManifestWork.Finalizers[i] == controllers.AppliedManifestWorkFinalizer {
			found = true
			break
		}
	}
	if !found {
		return nil
	}

	var err error

	owner := helper.NewAppliedManifestWorkOwner(appliedManifestWork)

	// Work is deleting, we remove its related resources on spoke cluster
	// We still need to run delete for every resource even with ownerref on it, since ownerref does not handle cluster
	// scoped resource correctly.
	reason := fmt.Sprintf("manifestwork %s is terminating", appliedManifestWork.Spec.ManifestWorkName)
	resourcesPendingFinalization, errs := helper.DeleteAppliedResources(
		ctx, appliedManifestWork.Status.AppliedResources, reason, m.spokeDynamicClient, controllerContext.Recorder(), *owner)

	updatedAppliedManifestWork := false
	if len(appliedManifestWork.Status.AppliedResources) != len(resourcesPendingFinalization) {
		// update the status of the manifest work accordingly
		appliedManifestWork.Status.AppliedResources = resourcesPendingFinalization
		appliedManifestWork, err = m.appliedManifestWorkClient.UpdateStatus(ctx, appliedManifestWork, metav1.UpdateOptions{})
		if err != nil {
			errs = append(errs, fmt.Errorf(
				"failed to update status of AppliedManifestWork %s: %w", originalManifestWork.Name, err))
		} else {
			updatedAppliedManifestWork = true
		}
	}
	if len(errs) != 0 {
		return utilerrors.NewAggregate(errs)
	}

	// requeue the work until all applied resources are deleted and finalized if the appliedmanifestwork itself is not updated
	if len(resourcesPendingFinalization) != 0 {
		klog.V(4).Infof("%d resources pending deletions %v", len(resourcesPendingFinalization))
		if !updatedAppliedManifestWork {
			controllerContext.Queue().AddAfter(appliedManifestWork.Name, m.rateLimiter.When(appliedManifestWork.Name))
		}
		return nil
	}

	// reset the rate limiter for the appliedmanifestwork
	m.rateLimiter.Forget(appliedManifestWork.Name)

	helper.RemoveFinalizer(appliedManifestWork, controllers.AppliedManifestWorkFinalizer)
	_, err = m.appliedManifestWorkClient.Update(ctx, appliedManifestWork, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to remove finalizer from AppliedManifestWork %s: %w", appliedManifestWork.Name, err)
	}
	return nil
}
