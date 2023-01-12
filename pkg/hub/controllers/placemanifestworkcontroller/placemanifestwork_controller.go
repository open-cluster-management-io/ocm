package placemanifestworkcontroller

import (
	"context"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	clusterinformerv1beta1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1beta1"
	clusterlister "open-cluster-management.io/api/client/cluster/listers/cluster/v1beta1"
	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	workinformerv1 "open-cluster-management.io/api/client/work/informers/externalversions/work/v1"
	workinformerv1alpha1 "open-cluster-management.io/api/client/work/informers/externalversions/work/v1alpha1"
	worklisterv1 "open-cluster-management.io/api/client/work/listers/work/v1"
	worklisterv1alpha1 "open-cluster-management.io/api/client/work/listers/work/v1alpha1"
	workapiv1alpha1 "open-cluster-management.io/api/work/v1alpha1"
)

const (
	// PlaceManifestWorkControllerNameLabelKey is the label key on manifestwork to ref to the placemanifestwork
	// that owns this manifestwork
	// TODO move this to the api repo
	PlaceManifestWorkControllerNameLabelKey = "work.open-cluster-management.io/placemanifestwork"
)

type PlaceManifestWorkController struct {
	workClient               workclientset.Interface
	placeManifestWorkLister  worklisterv1alpha1.PlaceManifestWorkLister
	placeManifestWorkIndexer cache.Indexer
	placementLister          clusterlister.PlacementLister
	placeDecisionLister      clusterlister.PlacementDecisionLister
	manifestWorkLister       worklisterv1.ManifestWorkLister
}

func NewPlaceManifestWorkController(
	recorder events.Recorder,
	workClient workclientset.Interface,
	placeManifestWorkInformer workinformerv1alpha1.PlaceManifestWorkInformer,
	manifestWorkInformer workinformerv1.ManifestWorkInformer,
	placementInformer clusterinformerv1beta1.PlacementInformer,
	placeDecisionInformer clusterinformerv1beta1.PlacementDecisionInformer) factory.Controller {

	controller := &PlaceManifestWorkController{
		workClient:               workClient,
		placeManifestWorkLister:  placeManifestWorkInformer.Lister(),
		placeManifestWorkIndexer: placeManifestWorkInformer.Informer().GetIndexer(),
		placementLister:          placementInformer.Lister(),
		placeDecisionLister:      placeDecisionInformer.Lister(),
		manifestWorkLister:       manifestWorkInformer.Lister(),
	}

	err := placeManifestWorkInformer.Informer().AddIndexers(
		cache.Indexers{
			placeManifestWorkByPlacement: indexPlacementManifestWorkByPlacement,
		})
	if err != nil {
		utilruntime.HandleError(err)
	}

	return factory.New().
		WithInformersQueueKeyFunc(func(obj runtime.Object) string {
			key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
			if err != nil {
				utilruntime.HandleError(err)
				return ""
			}
			return key
		}, placeManifestWorkInformer.Informer()).
		WithFilteredEventsInformersQueueKeyFunc(func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			key, ok := accessor.GetLabels()[PlaceManifestWorkControllerNameLabelKey]
			if !ok {
				return ""
			}
			return key
		}, func(obj interface{}) bool {
			accessor, err := meta.Accessor(obj)
			if err != nil {
				return false
			}
			if _, ok := accessor.GetLabels()[PlaceManifestWorkControllerNameLabelKey]; ok {
				return true
			}
			return false
		}, manifestWorkInformer.Informer()).
		WithInformersQueueKeysFunc(controller.placementDecisionQueueKeysFunc, placeDecisionInformer.Informer()).
		WithInformersQueueKeysFunc(controller.placementQueueKeysFunc, placementInformer.Informer()).
		WithSync(controller.sync).ToController("PlaceManifestWorkController", recorder)
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
