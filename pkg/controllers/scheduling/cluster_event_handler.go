package scheduling

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	cache "k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	clusterlisterv1beta1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1beta1"
	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
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

	// if the clusterset of the cluster changes, process the original clusterset
	if newCluster.Labels[clusterSetLabel] != oldCluster.Labels[clusterSetLabel] {
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

	clusterSetName, err := h.getClusterSetName(cluster)
	if err != nil {
		klog.V(4).Infof("Unable to get clusterset of cluster %q: %v", cluster.GetName(), err)
		return
	}

	// skip cluster belongs to no clusterset
	if len(clusterSetName) == 0 {
		return
	}

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

// getClusterSetName returns the name of the clusterset the cluster belongs to. It also checks the existence
// of the clusterset.
func (h *clusterEventHandler) getClusterSetName(cluster metav1.Object) (string, error) {
	// skip cluster belongs to no clusterset
	labels := cluster.GetLabels()
	clusterSetName, ok := labels[clusterSetLabel]
	if !ok {
		return "", nil
	}
	_, err := h.clusterSetLister.Get(clusterSetName)
	// skip if the clusterset does not exist
	if errors.IsNotFound(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}

	return clusterSetName, nil
}
