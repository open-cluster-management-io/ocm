package scheduling

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	cache "k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	clusterlisterv1beta1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1beta1"
	clusterapiv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
)

type clusterSetBindingEventHandler struct {
	clusterSetLister     clusterlisterv1beta1.ManagedClusterSetLister
	placementLister      clusterlisterv1beta1.PlacementLister
	enqueuePlacementFunc enqueuePlacementFunc
}

func (h *clusterSetBindingEventHandler) OnAdd(obj interface{}) {
	h.onChange(obj)
}

func (h *clusterSetBindingEventHandler) OnUpdate(oldObj, newObj interface{}) {
	h.onChange(newObj)
}

func (h *clusterSetBindingEventHandler) OnDelete(obj interface{}) {
	switch t := obj.(type) {
	case *clusterapiv1beta1.ManagedClusterSetBinding:
		h.onChange(obj)
	case cache.DeletedFinalStateUnknown:
		h.onChange(t.Obj)
	default:
		utilruntime.HandleError(fmt.Errorf("error decoding object, invalid type"))
	}
}

func (h *clusterSetBindingEventHandler) onChange(obj interface{}) {
	clusterSetBinding, ok := obj.(metav1.Object)
	if !ok {
		utilruntime.HandleError(fmt.Errorf("error decoding object, invalid type"))
		return
	}

	namespace := clusterSetBinding.GetNamespace()
	clusterSetBindingName := clusterSetBinding.GetName()

	_, err := h.clusterSetLister.Get(clusterSetBindingName)
	// skip if the clusterset does not exist
	if errors.IsNotFound(err) {
		return
	}
	if err != nil {
		klog.Errorf("Unable to get clusterset %q: %v", clusterSetBindingName, err)
		return
	}

	err = enqueuePlacementsByClusterSetBinding(namespace, clusterSetBindingName, h.placementLister, h.enqueuePlacementFunc)
	if err != nil {
		klog.Errorf("Unable to enqueue placements by clustersetbinding %s/%s: %v", namespace, clusterSetBindingName, err)
	}
}

// enqueuePlacementsByClusterSetBinding enqueues placements that might be impacted by a particular clustersetbinding
// into controller queue for further reconciliation
func enqueuePlacementsByClusterSetBinding(
	namespace, clusterSetBindingName string,
	placementLister clusterlisterv1beta1.PlacementLister,
	enqueuePlacementFunc enqueuePlacementFunc,
) error {
	placements, err := placementLister.Placements(namespace).List(labels.Everything())
	if err != nil {
		return err
	}
	for _, placement := range placements {
		// ignore placement whose .spec.clusterSets is specified but does no include this
		// particular clusterset.
		clusterSets := sets.NewString(placement.Spec.ClusterSets...)
		if clusterSets.Len() != 0 && !clusterSets.Has(clusterSetBindingName) {
			continue
		}
		enqueuePlacementFunc(placement.Namespace, placement.Name)
	}
	return nil
}
