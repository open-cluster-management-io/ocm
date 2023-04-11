package scheduling

import (
	"fmt"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	clusterinformerv1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterinformerv1beta1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1beta1"
	clusterinformerv1beta2 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1beta2"
	clusterlisterv1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterlisterv1beta2 "open-cluster-management.io/api/client/cluster/listers/cluster/v1beta2"
	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	clusterapiv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	clusterapiv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
)

const (
	// anyClusterSet is an index value for indexPlacementByClusterSet for placement
	// not setting any clusterset
	anyClusterSet                  = "*"
	placementsByClusterSetBinding  = "placementsByClusterSet"
	clustersetBindingsByClusterSet = "clustersetBindingsByClusterSet"
	placementsByScore              = "placementsByScore"
)

type enqueuer struct {
	queue                workqueue.RateLimitingInterface
	enqueuePlacementFunc func(obj interface{}, queue workqueue.RateLimitingInterface)

	clusterLister            clusterlisterv1.ManagedClusterLister
	clusterSetLister         clusterlisterv1beta2.ManagedClusterSetLister
	placementIndexer         cache.Indexer
	clusterSetBindingIndexer cache.Indexer
}

func newEnqueuer(
	queue workqueue.RateLimitingInterface,
	clusterInformer clusterinformerv1.ManagedClusterInformer,
	clusterSetInformer clusterinformerv1beta2.ManagedClusterSetInformer,
	placementInformer clusterinformerv1beta1.PlacementInformer,
	clusterSetBindingInformer clusterinformerv1beta2.ManagedClusterSetBindingInformer) *enqueuer {
	placementInformer.Informer().AddIndexers(cache.Indexers{
		placementsByScore:             indexPlacementsByScore,
		placementsByClusterSetBinding: indexPlacementByClusterSetBinding,
	})

	clusterSetBindingInformer.Informer().AddIndexers(cache.Indexers{
		clustersetBindingsByClusterSet: indexClusterSetBindingByClusterSet,
	})

	return &enqueuer{
		queue:                    queue,
		enqueuePlacementFunc:     enqueuePlacement,
		clusterLister:            clusterInformer.Lister(),
		clusterSetLister:         clusterSetInformer.Lister(),
		placementIndexer:         placementInformer.Informer().GetIndexer(),
		clusterSetBindingIndexer: clusterSetBindingInformer.Informer().GetIndexer(),
	}
}

func enqueuePlacement(obj interface{}, queue workqueue.RateLimitingInterface) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		runtime.HandleError(err)
		return
	}
	queue.Add(key)
}

func (e *enqueuer) enqueueClusterSetBinding(obj interface{}) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	namespace, _, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	// enqueue all placement that ref to the binding
	objs, err := e.placementIndexer.ByIndex(placementsByClusterSetBinding, key)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	anyObjs, err := e.placementIndexer.ByIndex(placementsByClusterSetBinding, fmt.Sprintf("%s/%s", namespace, anyClusterSet))
	if err != nil {
		runtime.HandleError(err)
		return
	}

	objs = append(objs, anyObjs...)

	for _, o := range objs {
		placement := o.(*clusterapiv1beta1.Placement)
		klog.V(4).Infof("enqueue placement %s/%s, because of binding %s", placement.Namespace, placement.Name, key)
		e.enqueuePlacementFunc(placement, e.queue)
	}
}

func (e *enqueuer) enqueueClusterSet(obj interface{}) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	objs, err := e.clusterSetBindingIndexer.ByIndex(clustersetBindingsByClusterSet, key)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	for _, o := range objs {
		clusterSetBinding := o.(*clusterapiv1beta2.ManagedClusterSetBinding)
		klog.V(4).Infof("enqueue clustersetbinding %s/%s, because of clusterset %s", clusterSetBinding.Namespace, clusterSetBinding.Name, key)
		e.enqueueClusterSetBinding(clusterSetBinding)
	}
}

func (e *enqueuer) enqueueCluster(obj interface{}) {
	cluster, ok := obj.(*clusterapiv1.ManagedCluster)
	if !ok {
		runtime.HandleError(fmt.Errorf("obj %T is not a ManagedCluster", obj))
		return
	}

	clusterSets, err := clusterapiv1beta2.GetClusterSetsOfCluster(cluster, e.clusterSetLister)
	if err != nil {
		klog.V(4).Infof("Unable to get clusterSets of cluster %q: %w", cluster.GetName(), err)
		return
	}

	for _, clusterSet := range clusterSets {
		klog.V(4).Infof("enqueue clusterSet %s, because of cluster %s", clusterSet.Name, cluster.Name)
		e.enqueueClusterSet(clusterSet)
	}
}

func (e *enqueuer) enqueuePlacementScore(obj interface{}) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	objs, err := e.placementIndexer.ByIndex(placementsByScore, name)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	// filter the namespace of placement based on cluster. Find all related clustersetbinding
	// to the cluster at first. Only enqueue placement when its namespace is in the valid namespaces
	// of clustersetbindings.
	filteredBindingNamespaces := sets.NewString()
	cluster, err := e.clusterLister.Get(namespace)
	if err != nil {
		klog.V(4).Infof("Unable to get cluster %s: %w", namespace, err)
	}

	clusterSets, err := clusterapiv1beta2.GetClusterSetsOfCluster(cluster, e.clusterSetLister)
	if err != nil {
		klog.V(4).Infof("Unable to get clusterSets of cluster %q: %w", cluster.GetName(), err)
		return
	}

	for _, clusterset := range clusterSets {
		bindingObjs, err := e.clusterSetBindingIndexer.ByIndex(clustersetBindingsByClusterSet, clusterset.Name)
		if err != nil {
			klog.V(4).Infof("Unable to get clusterSetBindings of clusterset %q: %w", clusterset.Name, err)
			continue
		}

		for _, bindingObj := range bindingObjs {
			binding := bindingObj.(*clusterapiv1beta2.ManagedClusterSetBinding)
			filteredBindingNamespaces.Insert(binding.Namespace)
		}
	}

	for _, o := range objs {
		placement := o.(*clusterapiv1beta1.Placement)
		if filteredBindingNamespaces.Has(placement.Namespace) {
			klog.V(4).Infof("enqueue placement %s/%s, because of score %s", placement.Namespace, placement.Name, key)
			e.enqueuePlacementFunc(placement, e.queue)
		}
	}
}

func indexPlacementByClusterSetBinding(obj interface{}) ([]string, error) {
	placement, ok := obj.(*clusterapiv1beta1.Placement)
	if !ok {
		return []string{}, fmt.Errorf("obj %T is not a Placement", obj)
	}

	clustersets := placement.Spec.ClusterSets
	if len(clustersets) == 0 {
		return []string{fmt.Sprintf("%s/%s", placement.Namespace, anyClusterSet)}, nil
	}

	var bindings []string
	for _, clusterset := range clustersets {
		bindings = append(bindings, fmt.Sprintf("%s/%s", placement.Namespace, clusterset))
	}

	return bindings, nil
}

func indexPlacementsByScore(obj interface{}) ([]string, error) {
	placement, ok := obj.(*clusterapiv1beta1.Placement)
	if !ok {
		return []string{}, fmt.Errorf("obj %T is not a Placement", obj)
	}

	var keys []string
	for _, config := range placement.Spec.PrioritizerPolicy.Configurations {
		if config.ScoreCoordinate == nil {
			continue
		}
		if config.ScoreCoordinate.Type != clusterapiv1beta1.ScoreCoordinateTypeAddOn {
			continue
		}
		if config.ScoreCoordinate.AddOn == nil {
			continue
		}
		keys = append(keys, config.ScoreCoordinate.AddOn.ResourceName)
	}

	return keys, nil
}

func indexClusterSetBindingByClusterSet(obj interface{}) ([]string, error) {
	binding, ok := obj.(*clusterapiv1beta2.ManagedClusterSetBinding)
	if !ok {
		return []string{}, fmt.Errorf("obj %T is not a ManagedClusterSetBinding", obj)
	}

	return []string{binding.Spec.ClusterSet}, nil
}
