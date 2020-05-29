package finalizercontroller

import (
	"context"
	"fmt"

	workv1client "github.com/open-cluster-management/api/client/work/clientset/versioned/typed/work/v1"
	workinformer "github.com/open-cluster-management/api/client/work/informers/externalversions/work/v1"
	worklister "github.com/open-cluster-management/api/client/work/listers/work/v1"
	workapiv1 "github.com/open-cluster-management/api/work/v1"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog"
)

// FinalizeController handles cleanup of manifestwork resources before deletion is allowed.
type FinalizeController struct {
	manifestWorkClient workv1client.ManifestWorkInterface
	manifestWorkLister worklister.ManifestWorkNamespaceLister
	spokeDynamicClient dynamic.Interface
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
	return m.syncManifestWork(ctx, manifestWork)
}

// syncManifestWork ensures that when a manifestwork has been deleted, everything it created is also deleted before removing
// the finalizer
func (m *FinalizeController) syncManifestWork(ctx context.Context, originalManifestWork *workapiv1.ManifestWork) error {
	manifestWork := originalManifestWork.DeepCopy()

	// no work to do until we're deleted
	if manifestWork.DeletionTimestamp.IsZero() {
		return nil
	}

	// don't do work if the finalizer is not present
	found := false
	for i := range manifestWork.Finalizers {
		if manifestWork.Finalizers[i] == manifestWorkFinalizer {
			found = true
			break
		}
	}
	if !found {
		return nil
	}

	var err error

	// Work is deleting, we remove its related resources on spoke cluster
	remaining, errs := m.cleanupResourceOfWork(manifestWork)
	if len(manifestWork.Status.AppliedResources) != len(remaining) {
		// update the status of the manifest work accordingly
		manifestWork.Status.AppliedResources = remaining

		manifestWork, err = m.manifestWorkClient.UpdateStatus(ctx, manifestWork, metav1.UpdateOptions{})
		if err != nil {
			errs = append(errs, fmt.Errorf(
				"Failed to update status of ManifestWork %s/%s: %w", manifestWork.Namespace, manifestWork.Name, err))
		}
	}
	if len(errs) != 0 {
		return utilerrors.NewAggregate(errs)
	}

	// We consider the case of deletion of created resources, with finalization still to come, as sufficient to remove the finalizer
	// We do this because resources cannot be un-deleted.  This means that deletion is inevitable.
	// Also, since we don't track UIDs, we have no reliable way of know when "this" particular resource has been removed as
	// compared with a case where this controller deletes it and another controller (or manifestwork) creates it.

	removeFinalizer(manifestWork, manifestWorkFinalizer)
	_, err = m.manifestWorkClient.Update(ctx, manifestWork, metav1.UpdateOptions{})
	return err
}

func (m *FinalizeController) cleanupResourceOfWork(work *workapiv1.ManifestWork) ([]workapiv1.AppliedManifestResourceMeta, []error) {
	klog.V(4).Infof("cleaning up %q", work.Name)

	var remaining []workapiv1.AppliedManifestResourceMeta
	errs := []error{}

	// delete all applied resources which are still tracked by the manifest work
	for _, appliedResource := range work.Status.AppliedResources {
		gvr := schema.GroupVersionResource{Group: appliedResource.Group, Version: appliedResource.Version, Resource: appliedResource.Resource}

		err := m.spokeDynamicClient.
			Resource(gvr).
			Namespace(appliedResource.Namespace).
			Delete(context.TODO(), appliedResource.Name, metav1.DeleteOptions{})
		switch {
		case errors.IsNotFound(err):
			// no-oop
		case err != nil:
			remaining = append(remaining, appliedResource)
			errs = append(errs, fmt.Errorf(
				"Failed to delete resource %v with key %s/%s: %w",
				gvr, appliedResource.Namespace, appliedResource.Name, err))
			continue
		}
		klog.Infof("Successfully delete resource %v with key %s/%s", gvr, appliedResource.Namespace, appliedResource.Name)
	}

	return remaining, errs
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
