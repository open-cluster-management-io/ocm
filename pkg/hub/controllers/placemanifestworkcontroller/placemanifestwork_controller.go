package placemanifestworkcontroller

import (
	"context"
	"encoding/json"
	"fmt"
	jsonpatch "github.com/evanphx/json-patch"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	clusterinformerv1beta1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1beta1"
	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	workinformerv1 "open-cluster-management.io/api/client/work/informers/externalversions/work/v1"
	workinformerv1alpha1 "open-cluster-management.io/api/client/work/informers/externalversions/work/v1alpha1"
	worklisterv1alpha1 "open-cluster-management.io/api/client/work/listers/work/v1alpha1"
	workapiv1alpha1 "open-cluster-management.io/api/work/v1alpha1"
)

const (
	// PlaceManifestWorkControllerNameLabelKey is the label key on manifestwork to ref to the placemanifestwork
	// that owns this manifestwork
	// TODO move this to the api repo
	PlaceManifestWorkControllerNameLabelKey = "work.open-cluster-management.io/placemanifestwork"

	// PlaceManifestWorkFinalizer is the name of the finalizer added to placeManifestWork. It is used to ensure
	// related manifestworks is deleted
	PlaceManifestWorkFinalizer = "work.open-cluster-management.io/manifest-work-cleanup"
)

type PlaceManifestWorkController struct {
	workClient               workclientset.Interface
	placeManifestWorkLister  worklisterv1alpha1.PlaceManifestWorkLister
	placeManifestWorkIndexer cache.Indexer

	reconcilers []placeManifestWorkReconcile
}

// placeManifestWorkReconcile is a interface for reconcile logic. It returns an updated placeManifestWork and whether further
// reconcile needs to proceed.
type placeManifestWorkReconcile interface {
	reconcile(ctx context.Context, pw *workapiv1alpha1.PlaceManifestWork) (*workapiv1alpha1.PlaceManifestWork, reconcileState, error)
}

type reconcileState int64

const (
	reconcileStop reconcileState = iota
	reconcileContinue
)

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

		reconcilers: []placeManifestWorkReconcile{
			&finalizeReconciler{workClient: workClient, manifestWorkLister: manifestWorkInformer.Lister()},
			&addFinalizerReconciler{workClient: workClient},
			&deployReconciler{workClient: workClient, manifestWorkLister: manifestWorkInformer.Lister(), placementLister: placementInformer.Lister(), placeDecisionLister: placeDecisionInformer.Lister()},
			&statusReconciler{workClient: workClient, manifestWorkLister: manifestWorkInformer.Lister(), placeDecisionLister: placeDecisionInformer.Lister()},
		},
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
	key := controllerContext.QueueKey()
	klog.V(4).Infof("Reconciling placeManifestWork %q", key)

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		// ignore placement whose key is not in format: namespace/name
		utilruntime.HandleError(err)
		return nil
	}

	placementManifestWork, err := m.placeManifestWorkLister.PlaceManifestWorks(namespace).Get(name)
	switch {
	case errors.IsNotFound(err):
		return nil
	case err != nil:
		return err
	}

	oldPlacementManifestWork := placementManifestWork
	placementManifestWork = placementManifestWork.DeepCopy()

	var state reconcileState
	var errs []error
	for _, reconciler := range m.reconcilers {
		placementManifestWork, state, err = reconciler.reconcile(ctx, placementManifestWork)
		if err != nil {
			errs = append(errs, err)
		}
		if state == reconcileStop {
			break
		}
	}

	// Patch status
	if err := m.patchPlaceManifestStatus(ctx, oldPlacementManifestWork, placementManifestWork); err != nil {
		errs = append(errs, err)
	}

	return utilerrors.NewAggregate(errs)
}

func (m *PlaceManifestWorkController) patchPlaceManifestStatus(ctx context.Context, old, new *workapiv1alpha1.PlaceManifestWork) error {
	if apiequality.Semantic.DeepEqual(old.Status, new.Status) {
		return nil
	}

	oldData, err := json.Marshal(workapiv1alpha1.PlaceManifestWork{
		Status: old.Status,
	})
	if err != nil {
		return fmt.Errorf("failed to Marshal old data for placeManifestWork status %s: %w", old.Name, err)
	}
	newData, err := json.Marshal(workapiv1alpha1.PlaceManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			UID:             old.UID,
			ResourceVersion: old.ResourceVersion,
		}, // to ensure they appear in the patch as preconditions
		Status: new.Status,
	})
	if err != nil {
		return fmt.Errorf("failed to Marshal new data for work status %s: %w", old.Name, err)
	}

	patchBytes, err := jsonpatch.CreateMergePatch(oldData, newData)
	if err != nil {
		return fmt.Errorf("failed to create patch for work %s: %w", old.Name, err)
	}

	_, err = m.workClient.WorkV1alpha1().PlaceManifestWorks(old.Namespace).Patch(ctx, old.Name, types.MergePatchType, patchBytes, metav1.PatchOptions{}, "status")
	return err
}
