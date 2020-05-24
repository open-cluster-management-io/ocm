package finalizercontroller

import (
	"context"

	workapiv1 "github.com/open-cluster-management/api/work/v1"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog"

	workv1client "github.com/open-cluster-management/api/client/work/clientset/versioned/typed/work/v1"
	workinformer "github.com/open-cluster-management/api/client/work/informers/externalversions/work/v1"
	worklister "github.com/open-cluster-management/api/client/work/listers/work/v1"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
)

// AddFinalizerController is to add the cluster.open-cluster-management.io/manifest-work-cleanup finalizer to manifestworks.
type AddFinalizerController struct {
	manifestWorkClient workv1client.ManifestWorkInterface
	manifestWorkLister worklister.ManifestWorkNamespaceLister
}

const (
	manifestWorkFinalizer = "cluster.open-cluster-management.io/manifest-work-cleanup"
)

// NewAddFinalizerController returns a ManifestWorkController
func NewAddFinalizerController(
	recorder events.Recorder,
	manifestWorkClient workv1client.ManifestWorkInterface,
	manifestWorkInformer workinformer.ManifestWorkInformer,
	manifestWorkLister worklister.ManifestWorkNamespaceLister,
) factory.Controller {

	controller := &AddFinalizerController{
		manifestWorkClient: manifestWorkClient,
		manifestWorkLister: manifestWorkLister,
	}

	return factory.New().
		WithInformersQueueKeyFunc(func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			return accessor.GetName()
		}, manifestWorkInformer.Informer()).
		WithSync(controller.sync).ToController("ManifestWorkAddFinalizerController", recorder)
}

func (m *AddFinalizerController) sync(ctx context.Context, controllerContext factory.SyncContext) error {
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

func (m *AddFinalizerController) syncManifestWork(ctx context.Context, originalManifestWork *workapiv1.ManifestWork) error {
	manifestWork := originalManifestWork.DeepCopy()

	// don't add finalizers to instances that are deleted
	if !manifestWork.DeletionTimestamp.IsZero() {
		return nil
	}

	// don't add finalizer to instances that already have it
	for i := range manifestWork.Finalizers {
		if manifestWork.Finalizers[i] == manifestWorkFinalizer {
			return nil
		}
	}
	// if this conflicts, we'll simply try again later
	manifestWork.Finalizers = append(manifestWork.Finalizers, manifestWorkFinalizer)
	_, err := m.manifestWorkClient.Update(ctx, manifestWork, metav1.UpdateOptions{})
	return err
}
