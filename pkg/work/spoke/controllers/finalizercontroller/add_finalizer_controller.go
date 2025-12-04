package finalizercontroller

import (
	"context"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"

	workv1client "open-cluster-management.io/api/client/work/clientset/versioned/typed/work/v1"
	workinformer "open-cluster-management.io/api/client/work/informers/externalversions/work/v1"
	worklister "open-cluster-management.io/api/client/work/listers/work/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"
	"open-cluster-management.io/sdk-go/pkg/logging"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	"open-cluster-management.io/ocm/pkg/common/queue"
)

const manifestWorkAddFinalizerController = "ManifestWorkAddFinalizerController"

// AddFinalizerController is to add the cluster.open-cluster-management.io/manifest-work-cleanup finalizer to manifestworks.
type AddFinalizerController struct {
	patcher            patcher.Patcher[*workapiv1.ManifestWork, workapiv1.ManifestWorkSpec, workapiv1.ManifestWorkStatus]
	manifestWorkLister worklister.ManifestWorkNamespaceLister
}

// NewAddFinalizerController returns a ManifestWorkController
func NewAddFinalizerController(
	manifestWorkClient workv1client.ManifestWorkInterface,
	manifestWorkInformer workinformer.ManifestWorkInformer,
	manifestWorkLister worklister.ManifestWorkNamespaceLister,
) factory.Controller {

	controller := &AddFinalizerController{
		patcher: patcher.NewPatcher[
			*workapiv1.ManifestWork, workapiv1.ManifestWorkSpec, workapiv1.ManifestWorkStatus](
			manifestWorkClient),
		manifestWorkLister: manifestWorkLister,
	}

	return factory.New().
		WithInformersQueueKeysFunc(queue.QueueKeyByMetaName, manifestWorkInformer.Informer()).
		WithSync(controller.sync).ToController(manifestWorkAddFinalizerController)
}

func (m *AddFinalizerController) sync(ctx context.Context, _ factory.SyncContext, manifestWorkName string) error {
	logger := klog.FromContext(ctx).WithValues("manifestWorkName", manifestWorkName)
	logger.V(5).Info("Reconciling ManifestWork")

	manifestWork, err := m.manifestWorkLister.Get(manifestWorkName)
	if errors.IsNotFound(err) {
		// work not found, could have been deleted, do nothing.
		return nil
	}
	if err != nil {
		return err
	}

	// set tracing key from work if there is any
	logger = logging.SetLogTracingByObject(logger, manifestWork)
	ctx = klog.NewContext(ctx, logger)

	return m.syncManifestWork(ctx, manifestWork)
}

func (m *AddFinalizerController) syncManifestWork(ctx context.Context, originalManifestWork *workapiv1.ManifestWork) error {
	manifestWork := originalManifestWork.DeepCopy()

	// don't add finalizers to instances that are deleted
	if !manifestWork.DeletionTimestamp.IsZero() {
		return nil
	}

	// if this conflicts, we'll simply try again later
	_, err := m.patcher.AddFinalizer(ctx, manifestWork, workapiv1.ManifestWorkFinalizer)

	return err
}
