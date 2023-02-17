package addonmanagement

import (
	"context"
	"fmt"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"open-cluster-management.io/addon-framework/pkg/basecontroller/factory"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformerv1alpha1 "open-cluster-management.io/api/client/addon/informers/externalversions/addon/v1alpha1"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"
	clusterinformersv1beta1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1beta1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
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
) factory.Controller {
	err := clusterManagementAddonInformers.Informer().AddIndexers(
		cache.Indexers{
			clusterManagementAddonByPlacement: indexClusterManagementAddonByPlacement,
		})
	if err != nil {
		utilruntime.HandleError(err)
	}

	err = addonInformers.Informer().AddIndexers(
		cache.Indexers{
			managedClusterAddonByName: indexManagedClusterAddonByName,
		})
	if err != nil {
		utilruntime.HandleError(err)
	}

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
			},
		},
	}

	return factory.New().WithInformersQueueKeysFunc(
		func(obj runtime.Object) []string {
			key, _ := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
			return []string{key}
		},
		addonInformers.Informer(), clusterManagementAddonInformers.Informer()).
		WithInformersQueueKeysFunc(c.placementDecisionQueueKeysFunc, placementDecisionInformer.Informer()).
		WithInformersQueueKeysFunc(c.placementQueueKeysFunc, placementInformer.Informer()).
		WithSync(c.sync).ToController("addon-management-controller")
}

func (m *addonManagementController) placementQueueKeysFunc(obj runtime.Object) []string {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return []string{}
	}

	objs, err := m.clusterManagementAddonIndexer.ByIndex(clusterManagementAddonByPlacement, key)
	if err != nil {
		utilruntime.HandleError(err)
		return []string{}
	}

	var keys []string
	for _, o := range objs {
		cma := o.(*addonv1alpha1.ClusterManagementAddOn)
		klog.V(4).Infof("enqueue ClusterManagementAddon %s, because of placement %s", cma.Name, key)
		keys = append(keys, cma.Name)
	}

	return keys
}

func (m *addonManagementController) placementDecisionQueueKeysFunc(obj runtime.Object) []string {
	accessor, _ := meta.Accessor(obj)
	placementName, ok := accessor.GetLabels()[clusterv1beta1.PlacementLabel]
	if !ok {
		return []string{}
	}

	objs, err := m.clusterManagementAddonIndexer.ByIndex(clusterManagementAddonByPlacement, fmt.Sprintf("%s/%s", accessor.GetNamespace(), placementName))
	if err != nil {
		utilruntime.HandleError(err)
		return []string{}
	}

	var keys []string
	for _, o := range objs {
		cma := o.(*addonv1alpha1.ClusterManagementAddOn)
		klog.V(4).Infof("enqueue placeManifestWork %s, because of placementDecision %s/%s",
			cma.Name, accessor.GetNamespace(), accessor.GetName())
		keys = append(keys, cma.Name)
	}

	return keys
}

func (c *addonManagementController) sync(ctx context.Context, syncCtx factory.SyncContext, key string) error {
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
