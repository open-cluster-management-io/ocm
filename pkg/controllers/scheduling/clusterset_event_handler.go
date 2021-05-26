package scheduling

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	cache "k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	clusterlisterv1alpha1 "github.com/open-cluster-management/api/client/cluster/listers/cluster/v1alpha1"
	clusterapiv1alpha1 "github.com/open-cluster-management/api/cluster/v1alpha1"
	errorhelpers "github.com/openshift/library-go/pkg/operator/v1helpers"
)

type clusterSetEventHandler struct {
	clusterSetBindingLister clusterlisterv1alpha1.ManagedClusterSetBindingLister
	placementLister         clusterlisterv1alpha1.PlacementLister
	enqueuePlacementFunc    enqueuePlacementFunc
}

func (h *clusterSetEventHandler) OnAdd(obj interface{}) {
	// ignore Add event
}

func (h *clusterSetEventHandler) OnUpdate(oldObj, newObj interface{}) {
	// ignore Update event
}

func (h *clusterSetEventHandler) OnDelete(obj interface{}) {
	var clusterSetName string
	switch t := obj.(type) {
	case *clusterapiv1alpha1.ManagedClusterSet:
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

// enqueuePlacementsByClusterSet enqueues placements that might be impacted by the given clusterset into
// controller queue for further reconciliation
func enqueuePlacementsByClusterSet(
	clusterSetName string,
	clusterSetBindingLister clusterlisterv1alpha1.ManagedClusterSetBindingLister,
	placementLister clusterlisterv1alpha1.PlacementLister,
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
