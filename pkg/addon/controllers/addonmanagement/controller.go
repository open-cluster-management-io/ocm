package addonmanagement

import (
	"context"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/api/errors"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformerv1alpha1 "open-cluster-management.io/api/client/addon/informers/externalversions/addon/v1alpha1"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"
	clusterinformersv1beta1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1beta1"

	addonindex "open-cluster-management.io/ocm/pkg/addon/index"
	"open-cluster-management.io/ocm/pkg/common/queue"
)

type addonManagementController struct {
	addonClient                   addonv1alpha1client.Interface
	clusterManagementAddonLister  addonlisterv1alpha1.ClusterManagementAddOnLister
	clusterManagementAddonIndexer cache.Indexer

	reconcilers []addonManagementReconcile
}

// addonManagementReconcile is a interface for reconcile logic. It creates ManagedClusterAddon based on install strategy
type addonManagementReconcile interface {
	reconcile(ctx context.Context, cma *addonv1alpha1.ClusterManagementAddOn) (*addonv1alpha1.ClusterManagementAddOn, reconcileState, error)
}

type reconcileState int64

const (
	reconcileStop reconcileState = iota
	reconcileContinue
)

func NewAddonManagementController(
	addonClient addonv1alpha1client.Interface,
	addonInformers addoninformerv1alpha1.ManagedClusterAddOnInformer,
	clusterManagementAddonInformers addoninformerv1alpha1.ClusterManagementAddOnInformer,
	placementInformer clusterinformersv1beta1.PlacementInformer,
	placementDecisionInformer clusterinformersv1beta1.PlacementDecisionInformer,
	addonFilterFunc factory.EventFilterFunc,
	recorder events.Recorder,
) factory.Controller {
	c := &addonManagementController{
		addonClient:                   addonClient,
		clusterManagementAddonLister:  clusterManagementAddonInformers.Lister(),
		clusterManagementAddonIndexer: clusterManagementAddonInformers.Informer().GetIndexer(),

		reconcilers: []addonManagementReconcile{
			&managedClusterAddonInstallReconciler{
				addonClient:                addonClient,
				placementDecisionLister:    placementDecisionInformer.Lister(),
				placementLister:            placementInformer.Lister(),
				managedClusterAddonIndexer: addonInformers.Informer().GetIndexer(),
				addonFilterFunc:            addonFilterFunc,
			},
		},
	}

	return factory.New().WithInformersQueueKeysFunc(
		queue.QueueKeyByMetaName,
		addonInformers.Informer(), clusterManagementAddonInformers.Informer()).
		WithInformersQueueKeysFunc(
			addonindex.ClusterManagementAddonByPlacementDecisionQueueKey(
				clusterManagementAddonInformers),
			placementDecisionInformer.Informer()).
		WithInformersQueueKeysFunc(
			addonindex.ClusterManagementAddonByPlacementQueueKey(
				clusterManagementAddonInformers),
			placementInformer.Informer()).
		WithSync(c.sync).ToController("addon-management-controller", recorder)
}

func (c *addonManagementController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	logger := klog.FromContext(ctx)
	addonName := syncCtx.QueueKey()
	logger.V(4).Info("Reconciling addon", "addonName", addonName)

	cma, err := c.clusterManagementAddonLister.Get(addonName)
	switch {
	case errors.IsNotFound(err):
		return nil
	case err != nil:
		return err
	}

	cma = cma.DeepCopy()

	var state reconcileState
	var errs []error
	for _, reconciler := range c.reconcilers {
		cma, state, err = reconciler.reconcile(ctx, cma)
		if err != nil {
			errs = append(errs, err)
		}
		if state == reconcileStop {
			break
		}
	}

	return utilerrors.NewAggregate(errs)
}
