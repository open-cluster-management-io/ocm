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
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	workv1client "open-cluster-management.io/api/client/work/clientset/versioned/typed/work/v1"
	workinformer "open-cluster-management.io/api/client/work/informers/externalversions/work/v1"
	worklister "open-cluster-management.io/api/client/work/listers/work/v1"
	"open-cluster-management.io/work/pkg/helper"
	"open-cluster-management.io/work/pkg/spoke/controllers"
)

// ManifestWorkFinalizeController handles cleanup of manifestwork resources before deletion is allowed.
type ManifestWorkFinalizeController struct {
	manifestWorkClient        workv1client.ManifestWorkInterface
	manifestWorkLister        worklister.ManifestWorkNamespaceLister
	appliedManifestWorkClient workv1client.AppliedManifestWorkInterface
	appliedManifestWorkLister worklister.AppliedManifestWorkLister
	hubHash                   string
	rateLimiter               workqueue.RateLimiter
}

func NewManifestWorkFinalizeController(
	recorder events.Recorder,
	manifestWorkClient workv1client.ManifestWorkInterface,
	manifestWorkInformer workinformer.ManifestWorkInformer,
	manifestWorkLister worklister.ManifestWorkNamespaceLister,
	appliedManifestWorkClient workv1client.AppliedManifestWorkInterface,
	appliedManifestWorkInformer workinformer.AppliedManifestWorkInformer,
	hubHash string,
) factory.Controller {

	controller := &ManifestWorkFinalizeController{
		manifestWorkClient:        manifestWorkClient,
		manifestWorkLister:        manifestWorkLister,
		appliedManifestWorkClient: appliedManifestWorkClient,
		appliedManifestWorkLister: appliedManifestWorkInformer.Lister(),
		hubHash:                   hubHash,
		rateLimiter:               workqueue.NewItemExponentialFailureRateLimiter(5*time.Millisecond, 1000*time.Second),
	}

	return factory.New().
		WithInformersQueueKeyFunc(func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			return accessor.GetName()
		}, manifestWorkInformer.Informer()).
		WithFilteredEventsInformersQueueKeyFunc(
			helper.AppliedManifestworkQueueKeyFunc(hubHash),
			helper.AppliedManifestworkHubHashFilter(hubHash),
			appliedManifestWorkInformer.Informer()).
		WithSync(controller.sync).ToController("ManifestWorkFinalizer", recorder)
}

func (m *ManifestWorkFinalizeController) sync(ctx context.Context, controllerContext factory.SyncContext) error {
	manifestWorkName := controllerContext.QueueKey()
	appliedManifestWorkName := fmt.Sprintf("%s-%s", m.hubHash, manifestWorkName)
	klog.V(4).Infof("Reconciling ManifestWork %q", manifestWorkName)

	manifestWork, err := m.manifestWorkLister.Get(manifestWorkName)

	// Delete appliedmanifestwork if relating manfiestwork is not found or being deleted
	switch {
	case errors.IsNotFound(err):
		err := m.deleteAppliedManifestWork(ctx, appliedManifestWorkName)
		if err != nil {
			return err
		}
	case err != nil:
		return err
	case !manifestWork.DeletionTimestamp.IsZero():
		err := m.deleteAppliedManifestWork(ctx, appliedManifestWorkName)
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
	helper.RemoveFinalizer(manifestWork, controllers.ManifestWorkFinalizer)
	_, err = m.manifestWorkClient.Update(ctx, manifestWork, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to remove finalizer from ManifestWork %s/%s: %w", manifestWork.Namespace, manifestWork.Name, err)
	}

	return nil
}

func (m *ManifestWorkFinalizeController) deleteAppliedManifestWork(ctx context.Context, appliedManifestWorkName string) error {
	appliedManifestWork, err := m.appliedManifestWorkLister.Get(appliedManifestWorkName)
	switch {
	case errors.IsNotFound(err):
		return nil
	case err != nil:
		return err
	case !appliedManifestWork.DeletionTimestamp.IsZero():
		return nil
	}

	return m.appliedManifestWorkClient.Delete(ctx, appliedManifestWorkName, metav1.DeleteOptions{})
}
