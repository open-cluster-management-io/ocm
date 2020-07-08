package finalizercontroller

import (
	"context"
	"fmt"
	"time"

	workv1client "github.com/open-cluster-management/api/client/work/clientset/versioned/typed/work/v1"
	workinformer "github.com/open-cluster-management/api/client/work/informers/externalversions/work/v1"
	worklister "github.com/open-cluster-management/api/client/work/listers/work/v1"
	workapiv1 "github.com/open-cluster-management/api/work/v1"
	"github.com/open-cluster-management/work/pkg/helper"
	"github.com/open-cluster-management/work/pkg/spoke/controllers"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"
)

// FinalizeController handles cleanup of manifestwork resources before deletion is allowed.
type FinalizeController struct {
	manifestWorkClient workv1client.ManifestWorkInterface
	manifestWorkLister worklister.ManifestWorkNamespaceLister
	spokeDynamicClient dynamic.Interface
	rateLimiter        workqueue.RateLimiter
}

func NewFinalizeController(
	recorder events.Recorder,
	spokeDynamicClient dynamic.Interface,
	manifestWorkClient workv1client.ManifestWorkInterface,
	manifestWorkInformer workinformer.ManifestWorkInformer,
	manifestWorkLister worklister.ManifestWorkNamespaceLister,
) factory.Controller {

	controller := &FinalizeController{
		manifestWorkClient: manifestWorkClient,
		manifestWorkLister: manifestWorkLister,
		spokeDynamicClient: spokeDynamicClient,
		rateLimiter:        workqueue.NewItemExponentialFailureRateLimiter(5*time.Millisecond, 1000*time.Second),
	}

	return factory.New().
		WithInformersQueueKeyFunc(func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			return accessor.GetName()
		}, manifestWorkInformer.Informer()).
		WithSync(controller.sync).ToController("ManifestWorkFinalizer", recorder)
}

func (m *FinalizeController) sync(ctx context.Context, controllerContext factory.SyncContext) error {
	manifestWorkName := controllerContext.QueueKey()
	klog.V(4).Infof("Reconciling ManifestWork %q", manifestWorkName)

	manifestWork, err := m.manifestWorkLister.Get(manifestWorkName)
	if errors.IsNotFound(err) {
		// work  not found, could have been deleted, do nothing.
		return nil
	}
	if err != nil {
		return err
	}
	return m.syncManifestWork(ctx, controllerContext, manifestWork)
}

// syncManifestWork ensures that when a manifestwork has been deleted, everything it created is also deleted.
// Foreground deletion is implemented, which means all resources created will be deleted and finalized
// before removing finalizer from manifestwork
func (m *FinalizeController) syncManifestWork(ctx context.Context, controllerContext factory.SyncContext, originalManifestWork *workapiv1.ManifestWork) error {
	manifestWork := originalManifestWork.DeepCopy()

	// no work to do until we're deleted
	if manifestWork.DeletionTimestamp == nil || manifestWork.DeletionTimestamp.IsZero() {
		return nil
	}

	// don't do work if the finalizer is not present
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

	var err error

	// Work is deleting, we remove its related resources on spoke cluster
	resourcesPendingFinalization, errs := helper.DeleteAppliedResources(manifestWork.Status.AppliedResources, m.spokeDynamicClient)
	updatedManifestWork := false
	if len(manifestWork.Status.AppliedResources) != len(resourcesPendingFinalization) {
		// update the status of the manifest work accordingly
		manifestWork.Status.AppliedResources = resourcesPendingFinalization
		manifestWork, err = m.manifestWorkClient.UpdateStatus(ctx, manifestWork, metav1.UpdateOptions{})
		if err != nil {
			errs = append(errs, fmt.Errorf(
				"Failed to update status of ManifestWork %s/%s: %w", manifestWork.Namespace, manifestWork.Name, err))
		} else {
			updatedManifestWork = true
		}
	}
	if len(errs) != 0 {
		return utilerrors.NewAggregate(errs)
	}

	// requeue the work until all applied resources are deleted and finalized if the work itself is not updated
	if len(resourcesPendingFinalization) != 0 {
		if !updatedManifestWork {
			controllerContext.Queue().AddAfter(manifestWork.Name, m.rateLimiter.When(manifestWork.Name))
		}
		return nil
	}

	// reset the rate limiter for the manifest work
	m.rateLimiter.Forget(manifestWork.Name)

	removeFinalizer(manifestWork, controllers.ManifestWorkFinalizer)
	_, err = m.manifestWorkClient.Update(ctx, manifestWork, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("Failed to remove finalizer from ManifestWork %s/%s: %w", manifestWork.Namespace, manifestWork.Name, err)
	}
	return nil
}

// removeFinalizer removes a finalizer from the list.  It mutates its input.
func removeFinalizer(manifestWork *workapiv1.ManifestWork, finalizerName string) {
	newFinalizers := []string{}
	for i := range manifestWork.Finalizers {
		if manifestWork.Finalizers[i] == finalizerName {
			continue
		}
		newFinalizers = append(newFinalizers, manifestWork.Finalizers[i])
	}
	manifestWork.Finalizers = newFinalizers
}
