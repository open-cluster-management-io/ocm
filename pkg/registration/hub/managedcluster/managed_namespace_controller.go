package managedcluster

import (
	"context"
	"fmt"
	"reflect"
	"sort"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	clientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterinformerv1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterinformerv1beta2 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1beta2"
	clusterlisterv1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterlisterv1beta2 "open-cluster-management.io/api/client/cluster/listers/cluster/v1beta2"
	v1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
	clustersdkv1beta2 "open-cluster-management.io/sdk-go/pkg/apis/cluster/v1beta2"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"
	"open-cluster-management.io/sdk-go/pkg/patcher"
)

// managedNamespaceController reconciles managed namespaces for ManagedClusters
// by watching ManagedCluster changes and updating their managed namespace status based on
// all ManagedClusterSets they belong to
type managedNamespaceController struct {
	clusterPatcher   patcher.Patcher[*v1.ManagedCluster, v1.ManagedClusterSpec, v1.ManagedClusterStatus]
	clusterLister    clusterlisterv1.ManagedClusterLister
	clusterSetLister clusterlisterv1beta2.ManagedClusterSetLister
}

// NewManagedNamespaceController creates a new managed namespace controller
func NewManagedNamespaceController(
	clusterClient clientset.Interface,
	clusterInformer clusterinformerv1.ManagedClusterInformer,
	clusterSetInformer clusterinformerv1beta2.ManagedClusterSetInformer) factory.Controller {

	controllerName := "managed-namespace-controller"
	syncCtx := factory.NewSyncContext(controllerName)

	c := &managedNamespaceController{
		clusterPatcher: patcher.NewPatcher[
			*v1.ManagedCluster, v1.ManagedClusterSpec, v1.ManagedClusterStatus](
			clusterClient.ClusterV1().ManagedClusters()),
		clusterLister:    clusterInformer.Lister(),
		clusterSetLister: clusterSetInformer.Lister(),
	}

	// Add explicit event handlers for ManagedCluster
	_, err := clusterInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			cluster, ok := obj.(*v1.ManagedCluster)
			if !ok {
				utilruntime.HandleError(fmt.Errorf("error to get ManagedCluster object: %v", obj))
				return
			}
			syncCtx.Queue().Add(cluster.Name)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			oldCluster, ok := oldObj.(*v1.ManagedCluster)
			if !ok {
				utilruntime.HandleError(fmt.Errorf("error to get old ManagedCluster object: %v", oldObj))
				return
			}
			newCluster, ok := newObj.(*v1.ManagedCluster)
			if !ok {
				utilruntime.HandleError(fmt.Errorf("error to get new ManagedCluster object: %v", newObj))
				return
			}
			// Only care about label changes that affect cluster set membership
			if !reflect.DeepEqual(oldCluster.Labels, newCluster.Labels) {
				syncCtx.Queue().Add(newCluster.Name)
			}
		},
	})
	if err != nil {
		utilruntime.HandleError(err)
	}

	return factory.New().
		WithSyncContext(syncCtx).
		WithBareInformers(clusterInformer.Informer()).
		WithInformersQueueKeysFunc(c.clusterSetToClusterQueueKeysFunc, clusterSetInformer.Informer()).
		WithSync(c.sync).
		ToController("ManagedNamespaceController")
}

func (c *managedNamespaceController) sync(ctx context.Context, syncCtx factory.SyncContext, clusterName string) error {
	logger := klog.FromContext(ctx).WithValues("clusterName", clusterName)
	ctx = klog.NewContext(ctx, logger)
	if len(clusterName) == 0 {
		return nil
	}

	logger.V(4).Info("Reconciling managed namespaces for ManagedCluster")

	cluster, err := c.clusterLister.Get(clusterName)
	if errors.IsNotFound(err) {
		// Cluster deleted - nothing to do
		logger.V(4).Info("ManagedCluster not found, skipping", "clusterName", clusterName)
		return nil
	}
	if err != nil {
		return err
	}

	// If cluster is being deleted, skip processing
	if !cluster.DeletionTimestamp.IsZero() {
		logger.V(4).Info("ManagedCluster is being deleted, skipping", "clusterName", clusterName)
		return nil
	}

	if err := c.syncManagedNamespacesForCluster(ctx, syncCtx, cluster); err != nil {
		return fmt.Errorf("failed to sync managed namespaces for ManagedCluster %q: %w", cluster.Name, err)
	}

	return nil
}

// syncManagedNamespacesForCluster updates the managed namespace configuration for a specific cluster
// based on all cluster sets it belongs to
func (c *managedNamespaceController) syncManagedNamespacesForCluster(ctx context.Context, syncCtx factory.SyncContext, cluster *v1.ManagedCluster) error {
	logger := klog.FromContext(ctx)

	// Get all cluster sets this cluster belongs to
	clusterSets, err := clustersdkv1beta2.GetClusterSetsOfCluster(cluster, c.clusterSetLister)
	if err != nil {
		return fmt.Errorf("failed to get cluster sets for cluster %q: %w", cluster.Name, err)
	}

	// Build the complete list of managed namespaces from all cluster sets
	var allManagedNamespaces []v1.ClusterSetManagedNamespaceConfig
	for _, clusterSet := range clusterSets {
		// Skip cluster sets that are being deleted
		if !clusterSet.DeletionTimestamp.IsZero() {
			logger.V(4).Info("Skipping cluster set being deleted", "clusterSetName", clusterSet.Name, "clusterName", cluster.Name)
			continue
		}

		for _, nsConfig := range clusterSet.Spec.ManagedNamespaces {
			managedNS := v1.ClusterSetManagedNamespaceConfig{
				ManagedNamespaceConfig: nsConfig,
				ClusterSet:             clusterSet.Name,
			}
			allManagedNamespaces = append(allManagedNamespaces, managedNS)
		}
	}

	// Sort by cluster set name first, then by namespace name for consistent ordering
	sort.Slice(allManagedNamespaces, func(i, j int) bool {
		if allManagedNamespaces[i].ClusterSet == allManagedNamespaces[j].ClusterSet {
			// Same cluster set, sort by namespace name
			return allManagedNamespaces[i].Name < allManagedNamespaces[j].Name
		}
		// Different cluster sets, sort by cluster set name
		return allManagedNamespaces[i].ClusterSet < allManagedNamespaces[j].ClusterSet
	})

	// Update cluster status
	updatedCluster := cluster.DeepCopy()
	updatedCluster.Status.ManagedNamespaces = allManagedNamespaces

	updated, err := c.clusterPatcher.PatchStatus(ctx, updatedCluster, updatedCluster.Status, cluster.Status)
	if err != nil {
		return fmt.Errorf("failed to update ManagedCluster status for cluster %q: %w", cluster.Name, err)
	}

	// Only record event if there was an actual update
	if updated {
		logger.V(4).Info("Updated managed namespaces for cluster", "clusterName", cluster.Name, "namespacesCount", len(allManagedNamespaces))
		syncCtx.Recorder().Eventf(ctx, "ManagedNamespacesUpdated", "Updated managed namespaces for cluster %q (total: %d)", cluster.Name, len(allManagedNamespaces))
	}

	return nil
}

// clusterSetToClusterQueueKeysFunc maps ManagedClusterSet changes to cluster names that should be reconciled
func (c *managedNamespaceController) clusterSetToClusterQueueKeysFunc(obj runtime.Object) []string {
	clusterSet, ok := obj.(*clusterv1beta2.ManagedClusterSet)
	if !ok {
		utilruntime.HandleError(fmt.Errorf("expected ManagedClusterSet, got %T", obj))
		return nil
	}

	if clusterSet == nil {
		return nil
	}

	clusterNames := sets.Set[string]{}

	// Get all clusters that currently belong to this cluster set
	currentClusters, err := clustersdkv1beta2.GetClustersFromClusterSet(clusterSet, c.clusterLister)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("error getting clusters from cluster set %q: %v", clusterSet.Name, err))
	} else {
		for _, cluster := range currentClusters {
			clusterNames.Insert(cluster.Name)
		}
	}

	// Get all clusters that previously had managed namespaces from this cluster set
	previousClusters, err := c.getClustersPreviouslyInSet(clusterSet.Name)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("error getting clusters previously in cluster set %q: %v", clusterSet.Name, err))
	} else {
		for _, cluster := range previousClusters {
			clusterNames.Insert(cluster.Name)
		}
	}

	// Convert set to slice
	return clusterNames.UnsortedList()
}

// getClustersPreviouslyInSet returns all clusters that have managed namespaces from the specified cluster set
func (c *managedNamespaceController) getClustersPreviouslyInSet(clusterSetName string) ([]*v1.ManagedCluster, error) {
	allClusters, err := c.clusterLister.List(labels.Everything())
	if err != nil {
		return nil, err
	}

	var clustersWithNamespaces []*v1.ManagedCluster
	for _, cluster := range allClusters {
		for _, managedNS := range cluster.Status.ManagedNamespaces {
			if managedNS.ClusterSet == clusterSetName {
				clustersWithNamespaces = append(clustersWithNamespaces, cluster)
				break
			}
		}
	}

	return clustersWithNamespaces, nil
}
