package scheduling

import (
	"fmt"
	"reflect"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	cache "k8s.io/client-go/tools/cache"
	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
)

type clusterEventHandler struct {
	enqueuer *enqueuer
}

func (h *clusterEventHandler) OnAdd(obj interface{}) {
	h.enqueuer.enqueueCluster(obj)
}

func (h *clusterEventHandler) OnUpdate(oldObj, newObj interface{}) {
	newCluster, ok := newObj.(*clusterapiv1.ManagedCluster)
	if !ok {
		return
	}
	h.enqueuer.enqueueCluster(newObj)

	if oldObj == nil {
		return
	}
	oldCluster, ok := oldObj.(*clusterapiv1.ManagedCluster)
	if !ok {
		return
	}

	// if the cluster labels changes, process the original clusterset
	if !reflect.DeepEqual(newCluster.Labels, oldCluster.Labels) {
		h.enqueuer.enqueueCluster(oldCluster)
	}
}

func (h *clusterEventHandler) OnDelete(obj interface{}) {
	switch t := obj.(type) {
	case *clusterapiv1.ManagedCluster:
		h.enqueuer.enqueueCluster(obj)
	case cache.DeletedFinalStateUnknown:
		h.enqueuer.enqueueCluster(t.Obj)
	default:
		utilruntime.HandleError(fmt.Errorf("error decoding object, invalid type"))
	}
}
