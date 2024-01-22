package manifestworkreplicasetcontroller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	clusterinformerv1beta1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1beta1"
	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	workinformerv1 "open-cluster-management.io/api/client/work/informers/externalversions/work/v1"
	workinformerv1alpha1 "open-cluster-management.io/api/client/work/informers/externalversions/work/v1alpha1"
	worklisterv1 "open-cluster-management.io/api/client/work/listers/work/v1"
	worklisterv1alpha1 "open-cluster-management.io/api/client/work/listers/work/v1alpha1"
	workapiv1 "open-cluster-management.io/api/work/v1"
	workapiv1alpha1 "open-cluster-management.io/api/work/v1alpha1"
	workapplier "open-cluster-management.io/sdk-go/pkg/apis/work/v1/applier"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	"open-cluster-management.io/ocm/pkg/common/helpers"
	"open-cluster-management.io/ocm/pkg/common/queue"
)

const (
	// ManifestWorkReplicaSetControllerNameLabelKey is the label key on manifestwork to ref to the ManifestWorkReplicaSet
	// that owns this manifestwork
	// TODO move this to the api repo
	ManifestWorkReplicaSetControllerNameLabelKey = "work.open-cluster-management.io/manifestworkreplicaset"

	// ManifestWorkReplicaSetPlacementNameLabelKey is the label key on manifestwork to ref to the Placement that select
	// the managedCluster on the manifestWorkReplicaSet's PlacementRef.
	ManifestWorkReplicaSetPlacementNameLabelKey = "work.open-cluster-management.io/placementname"

	// ManifestWorkReplicaSetFinalizer is the name of the finalizer added to ManifestWorkReplicaSet. It is used to ensure
	// related manifestworks is deleted
	ManifestWorkReplicaSetFinalizer = "work.open-cluster-management.io/manifest-work-cleanup"

	// maxRequeueTime is the same as the informer resync period
	maxRequeueTime = 30 * time.Minute
)

type ManifestWorkReplicaSetController struct {
	workClient                    workclientset.Interface
	manifestWorkReplicaSetLister  worklisterv1alpha1.ManifestWorkReplicaSetLister
	manifestWorkReplicaSetIndexer cache.Indexer

	reconcilers []ManifestWorkReplicaSetReconcile
}

// ManifestWorkReplicaSetReconcile is a interface for reconcile logic. It returns an updated manifestWorkReplicaSet and whether further
// reconcile needs to proceed.
type ManifestWorkReplicaSetReconcile interface {
	reconcile(ctx context.Context, pw *workapiv1alpha1.ManifestWorkReplicaSet) (*workapiv1alpha1.ManifestWorkReplicaSet, reconcileState, error)
}

type reconcileState int64

const (
	reconcileStop reconcileState = iota
	reconcileContinue
)

func NewManifestWorkReplicaSetController(
	recorder events.Recorder,
	workClient workclientset.Interface,
	manifestWorkReplicaSetInformer workinformerv1alpha1.ManifestWorkReplicaSetInformer,
	manifestWorkInformer workinformerv1.ManifestWorkInformer,
	placementInformer clusterinformerv1beta1.PlacementInformer,
	placeDecisionInformer clusterinformerv1beta1.PlacementDecisionInformer) factory.Controller {

	controller := newController(
		workClient, manifestWorkReplicaSetInformer, manifestWorkInformer, placementInformer, placeDecisionInformer)

	err := manifestWorkReplicaSetInformer.Informer().AddIndexers(
		cache.Indexers{
			manifestWorkReplicaSetByPlacement: indexManifestWorkReplicaSetByPlacement,
		})
	if err != nil {
		utilruntime.HandleError(err)
	}

	return factory.New().
		WithInformersQueueKeysFunc(queue.QueueKeyByMetaNamespaceName, manifestWorkReplicaSetInformer.Informer()).
		WithFilteredEventsInformersQueueKeyFunc(func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			labelValue, ok := accessor.GetLabels()[ManifestWorkReplicaSetControllerNameLabelKey]
			if !ok {
				return ""
			}
			keys := strings.Split(labelValue, ".")
			if len(keys) != 2 {
				return ""
			}
			return fmt.Sprintf("%s/%s", keys[0], keys[1])
		},
			queue.FileterByLabel(ManifestWorkReplicaSetControllerNameLabelKey),
			manifestWorkInformer.Informer()).
		WithInformersQueueKeysFunc(controller.placementDecisionQueueKeysFunc, placeDecisionInformer.Informer()).
		WithInformersQueueKeysFunc(controller.placementQueueKeysFunc, placementInformer.Informer()).
		WithSync(controller.sync).ToController("ManifestWorkReplicaSetController", recorder)
}

func newController(workClient workclientset.Interface,
	manifestWorkReplicaSetInformer workinformerv1alpha1.ManifestWorkReplicaSetInformer,
	manifestWorkInformer workinformerv1.ManifestWorkInformer,
	placementInformer clusterinformerv1beta1.PlacementInformer,
	placeDecisionInformer clusterinformerv1beta1.PlacementDecisionInformer) *ManifestWorkReplicaSetController {
	return &ManifestWorkReplicaSetController{
		workClient:                    workClient,
		manifestWorkReplicaSetLister:  manifestWorkReplicaSetInformer.Lister(),
		manifestWorkReplicaSetIndexer: manifestWorkReplicaSetInformer.Informer().GetIndexer(),

		reconcilers: []ManifestWorkReplicaSetReconcile{
			&finalizeReconciler{workApplier: workapplier.NewWorkApplierWithTypedClient(workClient, manifestWorkInformer.Lister()),
				workClient: workClient, manifestWorkLister: manifestWorkInformer.Lister()},
			&addFinalizerReconciler{workClient: workClient},
			&deployReconciler{workApplier: workapplier.NewWorkApplierWithTypedClient(workClient, manifestWorkInformer.Lister()),
				manifestWorkLister: manifestWorkInformer.Lister(), placementLister: placementInformer.Lister(), placeDecisionLister: placeDecisionInformer.Lister()},
			&statusReconciler{manifestWorkLister: manifestWorkInformer.Lister()},
		},
	}
}

// sync is the main reconcile loop for ManifestWorkReplicaSet. It is triggered every 15sec
func (m *ManifestWorkReplicaSetController) sync(ctx context.Context, controllerContext factory.SyncContext) error {
	key := controllerContext.QueueKey()
	klog.V(4).Infof("Reconciling ManifestWorkReplicaSet %q", key)

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		// ignore placement whose key is not in format: namespace/name
		utilruntime.HandleError(err)
		return nil
	}

	oldManifestWorkReplicaSet, err := m.manifestWorkReplicaSetLister.ManifestWorkReplicaSets(namespace).Get(name)
	switch {
	case apierrors.IsNotFound(err):
		return nil
	case err != nil:
		return err
	}

	manifestWorkReplicaSet := oldManifestWorkReplicaSet.DeepCopy()

	var state reconcileState
	var errs []error
	minRequeue := maxRequeueTime
	for _, reconciler := range m.reconcilers {
		manifestWorkReplicaSet, state, err = reconciler.reconcile(ctx, manifestWorkReplicaSet)
		var rqe helpers.RequeueError
		if err != nil && errors.As(err, &rqe) {
			if minRequeue > rqe.RequeueTime {
				minRequeue = rqe.RequeueTime
			}
		} else if err != nil {
			errs = append(errs, err)
		}
		if state == reconcileStop {
			break
		}
	}

	workSetPatcher := patcher.NewPatcher[
		*workapiv1alpha1.ManifestWorkReplicaSet, workapiv1alpha1.ManifestWorkReplicaSetSpec, workapiv1alpha1.ManifestWorkReplicaSetStatus](
		m.workClient.WorkV1alpha1().ManifestWorkReplicaSets(namespace))

	// Patch status
	if _, err := workSetPatcher.PatchStatus(ctx, manifestWorkReplicaSet, manifestWorkReplicaSet.Status, oldManifestWorkReplicaSet.Status); err != nil {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return utilerrors.NewAggregate(errs)
	}
	if minRequeue < maxRequeueTime {
		controllerContext.Queue().AddAfter(key, minRequeue)
	}

	return nil
}

func listManifestWorksByManifestWorkReplicaSet(mwrs *workapiv1alpha1.ManifestWorkReplicaSet,
	manifestWorkLister worklisterv1.ManifestWorkLister) ([]*workapiv1.ManifestWork, error) {
	req, err := labels.NewRequirement(ManifestWorkReplicaSetControllerNameLabelKey, selection.Equals, []string{manifestWorkReplicaSetKey(mwrs)})
	if err != nil {
		return nil, err
	}

	selector := labels.NewSelector().Add(*req)
	return manifestWorkLister.List(selector)
}

func listManifestWorksByMWRSetPlacementRef(mwrs *workapiv1alpha1.ManifestWorkReplicaSet, placementName string,
	manifestWorkLister worklisterv1.ManifestWorkLister) ([]*workapiv1.ManifestWork, error) {
	reqMWRSet, err := labels.NewRequirement(ManifestWorkReplicaSetControllerNameLabelKey, selection.Equals, []string{manifestWorkReplicaSetKey(mwrs)})
	if err != nil {
		return nil, err
	}

	reqPlacementRef, err := labels.NewRequirement(ManifestWorkReplicaSetPlacementNameLabelKey, selection.Equals, []string{placementName})
	if err != nil {
		return nil, err
	}

	selector := labels.NewSelector().Add(*reqMWRSet, *reqPlacementRef)
	return manifestWorkLister.List(selector)
}
