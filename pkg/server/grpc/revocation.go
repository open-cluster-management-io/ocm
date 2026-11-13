package grpc

import (
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	addonv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

// connectionCloser is what revocation needs from the SPIFFE connection registry.
type connectionCloser interface {
	CloseCluster(cluster string) int
	CloseAddOn(cluster, addon string) int
}

// clusterRevocationHandler closes a cluster's SPIFFE connections, Klusterlet and add-on agents
// alike, when the hub withdraws access: the cluster goes from accepted to denied, starts
// deleting, or is deleted. A cluster that was never accepted keeps its connections, as with the
// grpc driver: its agent may create the cluster and update its status before acceptance.
// Closing is the fast path; new requests are refused by the cluster gate and RBAC.
func clusterRevocationHandler(reg connectionCloser) cache.ResourceEventHandler {
	closeCluster := func(name, why string) {
		if n := reg.CloseCluster(name); n > 0 {
			klog.Infof("closed %d connection(s) of cluster %s: %s", n, name, why)
		}
	}
	return cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(oldObj, newObj any) {
			older, ok1 := oldObj.(*clusterv1.ManagedCluster)
			newer, ok2 := newObj.(*clusterv1.ManagedCluster)
			if !ok1 || !ok2 {
				return
			}
			switch {
			case newer.DeletionTimestamp != nil && older.DeletionTimestamp == nil:
				closeCluster(newer.Name, "cluster is being deleted")
			case older.Spec.HubAcceptsClient && !newer.Spec.HubAcceptsClient:
				closeCluster(newer.Name, "hubAcceptsClient is false")
			}
		},
		DeleteFunc: func(obj any) {
			if c, ok := tombstone[*clusterv1.ManagedCluster](obj); ok {
				closeCluster(c.Name, "cluster deleted")
			}
		},
	}
}

// addOnRevocationHandler closes one add-on's SPIFFE connections on its cluster when the add-on
// is disabled (its ManagedClusterAddOn starts deleting or is deleted).
func addOnRevocationHandler(reg connectionCloser) cache.ResourceEventHandler {
	closeAddOn := func(a *addonv1beta1.ManagedClusterAddOn, why string) {
		if n := reg.CloseAddOn(a.Namespace, a.Name); n > 0 {
			klog.Infof("closed %d connection(s) of add-on %s on cluster %s: %s", n, a.Name, a.Namespace, why)
		}
	}
	return cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(oldObj, newObj any) {
			older, ok1 := oldObj.(*addonv1beta1.ManagedClusterAddOn)
			newer, ok2 := newObj.(*addonv1beta1.ManagedClusterAddOn)
			if ok1 && ok2 && newer.DeletionTimestamp != nil && older.DeletionTimestamp == nil {
				closeAddOn(newer, "add-on is being deleted")
			}
		},
		DeleteFunc: func(obj any) {
			if a, ok := tombstone[*addonv1beta1.ManagedClusterAddOn](obj); ok {
				closeAddOn(a, "add-on deleted")
			}
		},
	}
}

// tombstone returns the object of a delete event, unwrapping DeletedFinalStateUnknown.
func tombstone[T any](obj any) (T, bool) {
	if d, ok := obj.(cache.DeletedFinalStateUnknown); ok {
		obj = d.Obj
	}
	t, ok := obj.(T)
	return t, ok
}
