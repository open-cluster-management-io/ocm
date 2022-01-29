package scheduling

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	cache "k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	errorhelpers "github.com/openshift/library-go/pkg/operator/v1helpers"
	clusterlisterv1beta1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1beta1"
	clusterapiv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
)

type clusterSetEventHandler struct {
	clusterSetBindingLister clusterlisterv1beta1.ManagedClusterSetBindingLister
	placementLister         clusterlisterv1beta1.PlacementLister
	enqueuePlacementFunc    enqueuePlacementFunc
}

func (h *clusterSetEventHandler) OnAdd(obj interface{}) {
	h.onChange(obj)
}

func (h *clusterSetEventHandler) OnUpdate(oldObj, newObj interface{}) {
	// ignore Update event
}

func (h *clusterSetEventHandler) OnDelete(obj interface{}) {
	var clusterSetName string
	switch t := obj.(type) {
	case *clusterapiv1beta1.ManagedClusterSet:
		clusterSetName = t.Name
	case cache.DeletedFinalStateUnknown:
		clusterSet, ok := t.Obj.(metav1.Object)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("error decoding object tombstone, invalid type"))
			return
		}
		clusterSetName = clusterSet.GetName()
	default:
		utilruntime.HandleError(fmt.Errorf("error decoding object, invalid type"))
		return
	}

	err := enqueuePlacementsByClusterSet(clusterSetName, h.clusterSetBindingLister,
		h.placementLister, h.enqueuePlacementFunc)
	if err != nil {
		klog.Errorf("Unable to enqueue placements by clusterset %q: %v", clusterSetName, err)
	}
}

func (h *clusterSetEventHandler) onChange(obj interface{}) {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("error accessing metadata: %w", err))
		return
	}

	clusterSetName := accessor.GetName()
	err = enqueuePlacementsByClusterSet(clusterSetName, h.clusterSetBindingLister,
		h.placementLister, h.enqueuePlacementFunc)
	if err != nil {
		klog.Errorf("Unable to enqueue placements by clusterset %q: %v", clusterSetName, err)
	}
}

// enqueuePlacementsByClusterSet enqueues placements that might be impacted by the given clusterset into
// controller queue for further reconciliation
func enqueuePlacementsByClusterSet(
	clusterSetName string,
	clusterSetBindingLister clusterlisterv1beta1.ManagedClusterSetBindingLister,
	placementLister clusterlisterv1beta1.PlacementLister,
	enqueuePlacementFunc enqueuePlacementFunc,
) error {
	bindings, err := clusterSetBindingLister.List(labels.Everything())
	if err != nil {
		return err
	}

	errs := []error{}
	for _, binding := range bindings {
		if binding.Name != clusterSetName {
			continue
		}

		if err := enqueuePlacementsByClusterSetBinding(binding.Namespace, binding.Name, placementLister, enqueuePlacementFunc); err != nil {
			errs = append(errs, err)
		}
	}
	return errorhelpers.NewMultiLineAggregate(errs)
}
