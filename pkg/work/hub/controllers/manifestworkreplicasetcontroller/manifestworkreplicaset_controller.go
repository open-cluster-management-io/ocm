package manifestworkreplicasetcontroller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

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
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	"open-cluster-management.io/ocm/pkg/common/helpers"
	"open-cluster-management.io/ocm/pkg/common/queue"
)

// maxRequeueTime is the same as the informer resync period
const maxRequeueTime = 30 * time.Minute

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
	workClient workclientset.Interface,
	workApplier *workapplier.WorkApplier,
	manifestWorkReplicaSetInformer workinformerv1alpha1.ManifestWorkReplicaSetInformer,
	manifestWorkInformer workinformerv1.ManifestWorkInformer,
	placementInformer clusterinformerv1beta1.PlacementInformer,
	placeDecisionInformer clusterinformerv1beta1.PlacementDecisionInformer,
) factory.Controller {
	controller := newController(
		workClient,
		workApplier,
		manifestWorkReplicaSetInformer,
		manifestWorkInformer,
		placementInformer,
		placeDecisionInformer,
	)

	err := manifestWorkReplicaSetInformer.Informer().AddIndexers(
		cache.Indexers{
			manifestWorkReplicaSetByPlacement: indexManifestWorkReplicaSetByPlacement,
		})
	if err != nil {
		utilruntime.HandleError(err)
	}

	return factory.New().
		WithInformersQueueKeysFunc(queue.QueueKeyByMetaNamespaceName, manifestWorkReplicaSetInformer.Informer()).
		WithFilteredEventsInformersQueueKeysFunc(func(obj runtime.Object) []string {
			accessor, _ := meta.Accessor(obj)
			labelValue, ok := accessor.GetLabels()[workapiv1alpha1.ManifestWorkReplicaSetControllerNameLabelKey]
			if !ok {
				return []string{}
			}
			keys := strings.Split(labelValue, ".")
			if len(keys) != 2 {
				return []string{}
			}
			return []string{fmt.Sprintf("%s/%s", keys[0], keys[1])}
		},
			queue.FileterByLabel(workapiv1alpha1.ManifestWorkReplicaSetControllerNameLabelKey),
			manifestWorkInformer.Informer()).
		WithInformersQueueKeysFunc(controller.placementDecisionQueueKeysFunc, placeDecisionInformer.Informer()).
		WithInformersQueueKeysFunc(controller.placementQueueKeysFunc, placementInformer.Informer()).
		WithSync(controller.sync).ToController("ManifestWorkReplicaSetController")
}

func newController(
	workClient workclientset.Interface,
	workApplier *workapplier.WorkApplier,
	manifestWorkReplicaSetInformer workinformerv1alpha1.ManifestWorkReplicaSetInformer,
	manifestWorkInformer workinformerv1.ManifestWorkInformer,
	placementInformer clusterinformerv1beta1.PlacementInformer,
	placeDecisionInformer clusterinformerv1beta1.PlacementDecisionInformer,
) *ManifestWorkReplicaSetController {
	return &ManifestWorkReplicaSetController{
		workClient:                    workClient,
		manifestWorkReplicaSetLister:  manifestWorkReplicaSetInformer.Lister(),
		manifestWorkReplicaSetIndexer: manifestWorkReplicaSetInformer.Informer().GetIndexer(),

		reconcilers: []ManifestWorkReplicaSetReconcile{
			&finalizeReconciler{
				workApplier:        workApplier,
				workClient:         workClient,
				manifestWorkLister: manifestWorkInformer.Lister(),
			},
			&addFinalizerReconciler{
				workClient: workClient,
			},
			&deployReconciler{
				workApplier:         workApplier,
				manifestWorkLister:  manifestWorkInformer.Lister(),
				placementLister:     placementInformer.Lister(),
				placeDecisionLister: placeDecisionInformer.Lister(),
			},
			&statusReconciler{manifestWorkLister: manifestWorkInformer.Lister()},
		},
	}
}

// sync is the main reconcile loop for ManifestWorkReplicaSet. It is triggered every 15sec
func (m *ManifestWorkReplicaSetController) sync(ctx context.Context, controllerContext factory.SyncContext, key string) error {
	logger := klog.FromContext(ctx).WithValues("manifestWorkReplicaSet", key)

	logger.V(5).Info("Reconciling ManifestWorkReplicaSet")

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
	req, err := labels.NewRequirement(
		workapiv1alpha1.ManifestWorkReplicaSetControllerNameLabelKey,
		selection.Equals, []string{manifestWorkReplicaSetKey(mwrs)})
	if err != nil {
		return nil, err
	}

	selector := labels.NewSelector().Add(*req)
	return manifestWorkLister.List(selector)
}

func listManifestWorksByMWRSetPlacementRef(mwrs *workapiv1alpha1.ManifestWorkReplicaSet, placementName string,
	manifestWorkLister worklisterv1.ManifestWorkLister) ([]*workapiv1.ManifestWork, error) {
	reqMWRSet, err := labels.NewRequirement(workapiv1alpha1.ManifestWorkReplicaSetControllerNameLabelKey,
		selection.Equals, []string{manifestWorkReplicaSetKey(mwrs)})
	if err != nil {
		return nil, err
	}

	reqPlacementRef, err := labels.NewRequirement(workapiv1alpha1.ManifestWorkReplicaSetPlacementNameLabelKey,
		selection.Equals, []string{placementName})
	if err != nil {
		return nil, err
	}

	selector := labels.NewSelector().Add(*reqMWRSet, *reqPlacementRef)
	return manifestWorkLister.List(selector)
}
