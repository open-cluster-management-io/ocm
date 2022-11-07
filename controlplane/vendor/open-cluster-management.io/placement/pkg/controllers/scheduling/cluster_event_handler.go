package scheduling

import (
	"fmt"
	"reflect"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	cache "k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	clusterlisterv1beta1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1beta1"
	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	clusterapiv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
)

type clusterEventHandler struct {
	clusterSetLister        clusterlisterv1beta1.ManagedClusterSetLister
	clusterSetBindingLister clusterlisterv1beta1.ManagedClusterSetBindingLister
	placementLister         clusterlisterv1beta1.PlacementLister
	enqueuePlacementFunc    enqueuePlacementFunc
}

func (h *clusterEventHandler) OnAdd(obj interface{}) {
	h.onChange(obj)
}

func (h *clusterEventHandler) OnUpdate(oldObj, newObj interface{}) {
	newCluster, ok := newObj.(*clusterapiv1.ManagedCluster)
	if !ok {
		return
	}
	h.onChange(newObj)

	if oldObj == nil {
		return
	}
	oldCluster, ok := oldObj.(*clusterapiv1.ManagedCluster)
	if !ok {
		return
	}

	// if the cluster labels changes, process the original clusterset
	if !reflect.DeepEqual(newCluster.Labels, oldCluster.Labels) {
		h.onChange(oldCluster)
	}
}

func (h *clusterEventHandler) OnDelete(obj interface{}) {
	switch t := obj.(type) {
	case *clusterapiv1.ManagedCluster:
		h.onChange(obj)
	case cache.DeletedFinalStateUnknown:
		h.onChange(t.Obj)
	default:
		utilruntime.HandleError(fmt.Errorf("error decoding object, invalid type"))
	}
}

func (h *clusterEventHandler) onChange(obj interface{}) {
	cluster, ok := obj.(metav1.Object)
	if !ok {
		utilruntime.HandleError(fmt.Errorf("error decoding object, invalid type"))
		return
	}

	clusterSetNames, err := h.getClusterSetNames(cluster)
	if err != nil {
		klog.V(4).Infof("Unable to get clusterset of cluster %q: %v", cluster.GetName(), err)
		return
	}

	// skip cluster belongs to no clusterset
	if len(clusterSetNames) == 0 {
		return
	}

	for _, clusterSetName := range clusterSetNames {
		// enqueue placements which might be impacted
		err = enqueuePlacementsByClusterSet(
			clusterSetName,
			h.clusterSetBindingLister,
			h.placementLister,
			h.enqueuePlacementFunc,
		)
		if err != nil {
			klog.Errorf("Unable to enqueue placements with access to clusterset %q: %v", clusterSetName, err)
		}
	}
}

// getClusterSetName returns the name of the clusterset the cluster belongs to. It also checks the existence
// of the clusterset.
func (h *clusterEventHandler) getClusterSetNames(cluster metav1.Object) ([]string, error) {
	clusterSetNames := []string{}
	clusterSets, err := clusterapiv1beta1.GetClusterSetsOfCluster(cluster.(*clusterapiv1.ManagedCluster), h.clusterSetLister)
	if err != nil {
		return clusterSetNames, err
	}

	for _, cs := range clusterSets {
		clusterSetNames = append(clusterSetNames, cs.Name)
	}

	return clusterSetNames, nil
}
