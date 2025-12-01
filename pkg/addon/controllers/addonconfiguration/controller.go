package addonconfiguration

import (
	"context"
	"errors"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformerv1alpha1 "open-cluster-management.io/api/client/addon/informers/externalversions/addon/v1alpha1"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"
	clusterinformersv1beta1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1beta1"
	clusterlisterv1beta1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1beta1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	addonindex "open-cluster-management.io/ocm/pkg/addon/index"
	"open-cluster-management.io/ocm/pkg/common/helpers"
	"open-cluster-management.io/ocm/pkg/common/queue"
)

const (
	// maxRequeueTime is the minimum informer resync period
	maxRequeueTime = 10 * time.Minute
)

// addonConfigurationController is a controller to update configuration of mca with the following order
// 1. use configuration in mca spec if it is set
// 2. use configuration in install strategy
// 3. use configuration in the default configuration in cma
type addonConfigurationController struct {
	addonClient                  addonv1alpha1client.Interface
	clusterManagementAddonLister addonlisterv1alpha1.ClusterManagementAddOnLister
	managedClusterAddonIndexer   cache.Indexer
	addonFilterFunc              factory.EventFilterFunc
	placementLister              clusterlisterv1beta1.PlacementLister
	placementDecisionGetter      helpers.PlacementDecisionGetter

	reconcilers []addonConfigurationReconcile
}

type addonConfigurationReconcile interface {
	reconcile(ctx context.Context, cma *addonv1alpha1.ClusterManagementAddOn,
		graph *configurationGraph) (*addonv1alpha1.ClusterManagementAddOn, reconcileState, error)
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
	addonFilterFunc factory.EventFilterFunc,
) factory.Controller {
	c := &addonConfigurationController{
		addonClient:                  addonClient,
		clusterManagementAddonLister: clusterManagementAddonInformers.Lister(),
		managedClusterAddonIndexer:   addonInformers.Informer().GetIndexer(),
		placementLister:              placementInformer.Lister(),
		placementDecisionGetter:      helpers.PlacementDecisionGetter{Client: placementDecisionInformer.Lister()},
		addonFilterFunc:              addonFilterFunc,
	}

	c.reconcilers = []addonConfigurationReconcile{
		&managedClusterAddonConfigurationReconciler{
			addonClient: addonClient,
		},
		&cmaProgressingReconciler{
			patcher: patcher.NewPatcher[
				*addonv1alpha1.ClusterManagementAddOn, addonv1alpha1.ClusterManagementAddOnSpec, addonv1alpha1.ClusterManagementAddOnStatus](
				addonClient.AddonV1alpha1().ClusterManagementAddOns()),
		},
	}

	controllerFactory := factory.New().WithFilteredEventsInformersQueueKeysFunc(
		queue.QueueKeyByMetaNamespaceName,
		c.addonFilterFunc,
		clusterManagementAddonInformers.Informer()).
		WithInformersQueueKeysFunc(queue.QueueKeyByMetaName, addonInformers.Informer()).
		WithInformersQueueKeysFunc(
			addonindex.ClusterManagementAddonByPlacementDecisionQueueKey(clusterManagementAddonInformers), placementDecisionInformer.Informer()).
		WithInformersQueueKeysFunc(
			addonindex.ClusterManagementAddonByPlacementQueueKey(clusterManagementAddonInformers), placementInformer.Informer())

	return controllerFactory.WithSync(c.sync).ToController("addon-configuration-controller")
}

func (c *addonConfigurationController) sync(ctx context.Context, syncCtx factory.SyncContext, addonName string) error {
	logger := klog.FromContext(ctx).WithValues("addonName", addonName)
	logger.V(4).Info("Reconciling addon")

	cma, err := c.clusterManagementAddonLister.Get(addonName)
	switch {
	case apierrors.IsNotFound(err):
		return nil
	case err != nil:
		return err
	}

	if !c.addonFilterFunc(cma) {
		return nil
	}

	cma = cma.DeepCopy()
	graph, err := c.buildConfigurationGraph(logger, cma)
	if err != nil {
		return err
	}

	// generate the rollout result before calling reconcile()
	// so that all the reconcilers are using the same rollout result
	err = graph.generateRolloutResult()
	if err != nil {
		return err
	}

	var state reconcileState
	var errs []error
	minRequeue := maxRequeueTime
	for _, reconciler := range c.reconcilers {
		cma, state, err = reconciler.reconcile(ctx, cma, graph)
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

	if len(errs) > 0 {
		return utilerrors.NewAggregate(errs)
	}

	if minRequeue < maxRequeueTime {
		syncCtx.Queue().AddAfter(addonName, minRequeue)
	}

	return nil
}

func (c *addonConfigurationController) buildConfigurationGraph(logger klog.Logger, cma *addonv1alpha1.ClusterManagementAddOn) (*configurationGraph, error) {
	graph := newGraph(cma.Spec.SupportedConfigs, cma.Status.DefaultConfigReferences)
	addons, err := c.managedClusterAddonIndexer.ByIndex(addonindex.ManagedClusterAddonByName, cma.Name)
	if err != nil {
		return graph, err
	}

	// add all existing addons to the default at first
	for _, addonObject := range addons {
		addon := addonObject.(*addonv1alpha1.ManagedClusterAddOn)
		graph.addAddonNode(addon)
	}

	if cma.Spec.InstallStrategy.Type == "" || cma.Spec.InstallStrategy.Type == addonv1alpha1.AddonInstallStrategyManual {
		return graph, nil
	}

	// check each install strategy in status
	var errs []error
	for _, installProgression := range cma.Status.InstallProgressions {
		for _, installStrategy := range cma.Spec.InstallStrategy.Placements {
			if installStrategy.PlacementRef != installProgression.PlacementRef {
				continue
			}

			// add placement node
			err = graph.addPlacementNode(installStrategy, installProgression, c.placementLister, c.placementDecisionGetter)
			if err != nil {
				errs = append(errs, err)
				continue
			}
		}
	}

	return graph, utilerrors.NewAggregate(errs)
}
