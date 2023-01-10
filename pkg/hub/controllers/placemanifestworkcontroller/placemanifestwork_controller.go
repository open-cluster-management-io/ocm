package placemanifestworkcontroller

import (
	"context"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	clusterlister "open-cluster-management.io/api/client/cluster/listers/cluster/v1beta1"
	workv1alpha1client "open-cluster-management.io/api/client/work/clientset/versioned/typed/work/v1alpha1"
	workinformerv1alpha1 "open-cluster-management.io/api/client/work/informers/externalversions/work/v1alpha1"
	worklisterv1 "open-cluster-management.io/api/client/work/listers/work/v1"
	worklisterv1alpha1 "open-cluster-management.io/api/client/work/listers/work/v1alpha1"
	workapiv1alpha1 "open-cluster-management.io/api/work/v1alpha1"
)

var (
	ResyncInterval = 15 * time.Second
)

type PlaceManifestWorkController struct {
	placeManifestWorkClient workv1alpha1client.PlaceManifestWorkInterface
	placeManifestWorkLister worklisterv1alpha1.PlaceManifestWorkNamespaceLister
	placementLister         clusterlister.PlacementLister
	placeDecisionLister     clusterlister.PlacementDecisionLister
	manifestWorkLister      worklisterv1.ManifestWorkLister
}

func NewPlaceManifestWorkController(
	ctx context.Context,
	recorder events.Recorder,
	placeManifestWorkClient workv1alpha1client.PlaceManifestWorkInterface,
	placeManifestWorkLister worklisterv1alpha1.PlaceManifestWorkNamespaceLister,
	placeManifestWorkInformer workinformerv1alpha1.PlaceManifestWorkInformer,
	manifestWorkLister worklisterv1.ManifestWorkLister,
	placementLister clusterlister.PlacementLister,
	placeDecisionLister clusterlister.PlacementDecisionLister) factory.Controller {

	controller := &PlaceManifestWorkController{
		placeManifestWorkClient: placeManifestWorkClient,
		placeManifestWorkLister: placeManifestWorkLister,
		placementLister:         placementLister,
		placeDecisionLister:     placeDecisionLister,
		manifestWorkLister:      manifestWorkLister,
	}

	return factory.New().
		WithInformersQueueKeyFunc(func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			return accessor.GetName()
		}, placeManifestWorkInformer.Informer()).
		WithSync(controller.sync).ResyncEvery(ResyncInterval).ToController("PlaceManifestWorkController", recorder)
}

// sync is the main reconcile loop for placeManifest work. It is triggered every 15sec
func (m *PlaceManifestWorkController) sync(ctx context.Context, controllerContext factory.SyncContext) error {
	placeManifestWorkName := controllerContext.QueueKey()
	klog.V(4).Infof("Reconciling placeManifestWork %q", placeManifestWorkName)
	// TODO: add watcher for manifest and placement
	// TODO: add annotation to determine the placeManifestwork owner
	// TODO: add the finalizer for PlaceManifestWork
	return nil
}

func (m *PlaceManifestWorkController) finalizePlaceManifestWork(placeManifestWork workapiv1alpha1.PlaceManifestWork) error {
	// TODO: Delete all ManifestWork owned by the given placeManifestWork
	return nil
}
