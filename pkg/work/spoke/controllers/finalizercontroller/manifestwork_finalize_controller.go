package finalizercontroller

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	workv1client "open-cluster-management.io/api/client/work/clientset/versioned/typed/work/v1"
	workinformer "open-cluster-management.io/api/client/work/informers/externalversions/work/v1"
	worklister "open-cluster-management.io/api/client/work/listers/work/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"
	"open-cluster-management.io/sdk-go/pkg/logging"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	"open-cluster-management.io/ocm/pkg/common/queue"
	"open-cluster-management.io/ocm/pkg/work/helper"
)

const manifestWorkFinalizer = "ManifestWorkFinalizer"

// ManifestWorkFinalizeController handles cleanup of manifestwork resources before deletion is allowed.
type ManifestWorkFinalizeController struct {
	patcher                   patcher.Patcher[*workapiv1.ManifestWork, workapiv1.ManifestWorkSpec, workapiv1.ManifestWorkStatus]
	manifestWorkLister        worklister.ManifestWorkNamespaceLister
	appliedManifestWorkClient workv1client.AppliedManifestWorkInterface
	appliedManifestWorkLister worklister.AppliedManifestWorkLister
	hubHash                   string
	rateLimiter               workqueue.RateLimiter
}

func NewManifestWorkFinalizeController(
	manifestWorkClient workv1client.ManifestWorkInterface,
	manifestWorkInformer workinformer.ManifestWorkInformer,
	manifestWorkLister worklister.ManifestWorkNamespaceLister,
	appliedManifestWorkClient workv1client.AppliedManifestWorkInterface,
	appliedManifestWorkInformer workinformer.AppliedManifestWorkInformer,
	hubHash string,
) factory.Controller {

	controller := &ManifestWorkFinalizeController{
		patcher: patcher.NewPatcher[
			*workapiv1.ManifestWork, workapiv1.ManifestWorkSpec, workapiv1.ManifestWorkStatus](
			manifestWorkClient),
		manifestWorkLister:        manifestWorkLister,
		appliedManifestWorkClient: appliedManifestWorkClient,
		appliedManifestWorkLister: appliedManifestWorkInformer.Lister(),
		hubHash:                   hubHash,
		rateLimiter:               workqueue.NewItemExponentialFailureRateLimiter(5*time.Millisecond, 1000*time.Second),
	}

	return factory.New().
		WithInformersQueueKeysFunc(queue.QueueKeyByMetaName, manifestWorkInformer.Informer()).
		WithFilteredEventsInformersQueueKeysFunc(
			helper.AppliedManifestworkQueueKeyFunc(hubHash),
			helper.AppliedManifestworkHubHashFilter(hubHash),
			appliedManifestWorkInformer.Informer()).
		WithSync(controller.sync).ToController(manifestWorkFinalizer)
}

func (m *ManifestWorkFinalizeController) sync(ctx context.Context, controllerContext factory.SyncContext, manifestWorkName string) error {
	appliedManifestWorkName := fmt.Sprintf("%s-%s", m.hubHash, manifestWorkName)

	logger := klog.FromContext(ctx).WithName(appliedManifestWorkFinalizer).
		WithValues("appliedManifestWorkName", appliedManifestWorkName, "manifestWorkName", manifestWorkName)

	logger.V(5).Info("Reconciling ManifestWork")

	manifestWork, err := m.manifestWorkLister.Get(manifestWorkName)

	// Delete appliedmanifestwork if relating manfiestwork is being deleted
	switch {
	case errors.IsNotFound(err):
		// the appliedmanifestwork will be evicted if the relating manfiestwork is not found
		return nil
	case err != nil:
		return err
	case !manifestWork.DeletionTimestamp.IsZero():
		// set tracing key from work if there is any
		logger = logging.SetLogTracingByObject(logger, manifestWork)
		ctx = klog.NewContext(ctx, logger)
		err := m.deleteAppliedManifestWork(ctx, manifestWork, appliedManifestWorkName)
		if err != nil {
			return err
		}
	default:
		return nil
	}

	_, err = m.appliedManifestWorkLister.Get(appliedManifestWorkName)
	switch {
	case errors.IsNotFound(err):
		// if the instance is not found, then we simply continue below this block to remove the finalizer
	case err != nil:
		return err
	default:
		// appliedmanifestwork still exists, requeue the manifestwork to check in the next loop.
		controllerContext.Queue().AddAfter(manifestWorkName, m.rateLimiter.When(manifestWorkName))
		return nil

	}

	// Now we can remove manifestwork finalizer
	// Skip if it is an orphaned appliedmanifestwork
	if manifestWork == nil {
		return nil
	}

	m.rateLimiter.Forget(manifestWorkName)
	manifestWork = manifestWork.DeepCopy()
	if err := m.patcher.RemoveFinalizer(ctx, manifestWork, workapiv1.ManifestWorkFinalizer); err != nil {
		return fmt.Errorf("failed to remove finalizer from ManifestWork %s/%s: %w", manifestWork.Namespace, manifestWork.Name, err)
	}

	return nil
}

func (m *ManifestWorkFinalizeController) deleteAppliedManifestWork(ctx context.Context, work *workapiv1.ManifestWork, appliedManifestWorkName string) error {
	appliedManifestWork, err := m.appliedManifestWorkLister.Get(appliedManifestWorkName)
	switch {
	case errors.IsNotFound(err):
		return nil
	case err != nil:
		return err
	case !appliedManifestWork.DeletionTimestamp.IsZero():
		return nil
	}

	workCopy := work.DeepCopy()
	meta.SetStatusCondition(&workCopy.Status.Conditions, metav1.Condition{
		Type:               workapiv1.WorkDeleting,
		Reason:             "WorkDeleting",
		Status:             metav1.ConditionTrue,
		Message:            "ManifestWork is being deleted",
		ObservedGeneration: workCopy.Generation,
	})

	if _, err = m.patcher.PatchStatus(ctx, work, workCopy.Status, work.Status); err != nil {
		return err
	}

	return m.appliedManifestWorkClient.Delete(ctx, appliedManifestWorkName, metav1.DeleteOptions{})
}
