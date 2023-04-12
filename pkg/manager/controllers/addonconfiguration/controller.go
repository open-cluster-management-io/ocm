package addonconfiguration

import (
	"context"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformerv1alpha1 "open-cluster-management.io/api/client/addon/informers/externalversions/addon/v1alpha1"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"
	clusterinformersv1beta1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1beta1"
	clusterlisterv1beta1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1beta1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"

	"open-cluster-management.io/addon-framework/pkg/basecontroller/factory"
	"open-cluster-management.io/addon-framework/pkg/index"
	"open-cluster-management.io/addon-framework/pkg/utils"
)

// addonConfigurationController is a controller to update configuration of mca with the following order
// 1. use configuration in mca spec if it is set
// 2. use configuration in install strategy
// 3. use configuration in the default configuration in cma
type addonConfigurationController struct {
	addonClient                   addonv1alpha1client.Interface
	clusterManagementAddonLister  addonlisterv1alpha1.ClusterManagementAddOnLister
	clusterManagementAddonIndexer cache.Indexer
	addonFilterFunc               utils.AddonManagementFilterFunc
	placementLister               clusterlisterv1beta1.PlacementLister
	placementDecisionLister       clusterlisterv1beta1.PlacementDecisionLister

	reconcilers []addonConfigurationReconcile
}

type addonConfigurationReconcile interface {
	reconcile(ctx context.Context, cma *addonv1alpha1.ClusterManagementAddOn) (*addonv1alpha1.ClusterManagementAddOn, reconcileState, error)
}

type reconcileState int64

const (
	reconcileStop reconcileState = iota
	reconcileContinue
)

func NewAddonConfigurationController(
	addonClient addonv1alpha1client.Interface,
	addonInformers addoninformerv1alpha1.ManagedClusterAddOnInformer,
	clusterManagementAddonInformers addoninformerv1alpha1.ClusterManagementAddOnInformer,
	placementInformer clusterinformersv1beta1.PlacementInformer,
	placementDecisionInformer clusterinformersv1beta1.PlacementDecisionInformer,
	addonFilterFunc utils.AddonManagementFilterFunc,
) factory.Controller {
	c := &addonConfigurationController{
		addonClient:                   addonClient,
		clusterManagementAddonLister:  clusterManagementAddonInformers.Lister(),
		clusterManagementAddonIndexer: clusterManagementAddonInformers.Informer().GetIndexer(),
		addonFilterFunc:               addonFilterFunc,
	}

	c.reconcilers = []addonConfigurationReconcile{
		&managedClusterAddonConfigurationReconciler{
			addonClient:                addonClient,
			managedClusterAddonIndexer: addonInformers.Informer().GetIndexer(),
			getClustersByPlacement:     c.getClustersByPlacement,
		},
	}

	controllerFactory := factory.New().WithInformersQueueKeysFunc(
		func(obj runtime.Object) []string {
			key, _ := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
			return []string{key}
		},
		addonInformers.Informer(), clusterManagementAddonInformers.Informer())

	// This is to handle the case the self managed addon-manager does not have placementInformer/placementDecisionInformer.
	// we will not consider installStratgy related placement for self managed addon-manager.
	if placementInformer != nil && placementDecisionInformer != nil {
		controllerFactory = controllerFactory.WithInformersQueueKeysFunc(
			index.ClusterManagementAddonByPlacementDecisionQueueKey(clusterManagementAddonInformers), placementDecisionInformer.Informer()).
			WithInformersQueueKeysFunc(index.ClusterManagementAddonByPlacementQueueKey(clusterManagementAddonInformers), placementInformer.Informer())
		c.placementLister = placementInformer.Lister()
		c.placementDecisionLister = placementDecisionInformer.Lister()
	}

	return controllerFactory.WithSync(c.sync).ToController("addon-configuration-controller")
}

func (c *addonConfigurationController) sync(ctx context.Context, syncCtx factory.SyncContext, key string) error {
	_, addonName, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		// ignore addon whose key is invalid
		return nil
	}

	klog.V(4).Infof("Reconciling addon %q", addonName)

	cma, err := c.clusterManagementAddonLister.Get(addonName)
	switch {
	case errors.IsNotFound(err):
		return nil
	case err != nil:
		return err
	}

	if !c.addonFilterFunc(cma) {
		return nil
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

func (c *addonConfigurationController) getClustersByPlacement(name, namespace string) ([]string, error) {
	var clusters []string
	if c.placementLister == nil || c.placementDecisionLister == nil {
		return clusters, nil
	}
	_, err := c.placementLister.Placements(namespace).Get(name)
	if err != nil {
		return clusters, err
	}

	decisionSelector := labels.SelectorFromSet(labels.Set{
		clusterv1beta1.PlacementLabel: name,
	})
	decisions, err := c.placementDecisionLister.PlacementDecisions(namespace).List(decisionSelector)
	if err != nil {
		return clusters, err
	}

	for _, d := range decisions {
		for _, sd := range d.Status.Decisions {
			clusters = append(clusters, sd.ClusterName)
		}
	}

	return clusters, nil
}
