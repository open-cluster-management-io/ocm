package factory

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

// syncContext implements SyncContext and provide user access to queue and object that caused
// the sync to be triggered.
type syncContext struct {
	queue workqueue.RateLimitingInterface
}

var _ SyncContext = syncContext{}

// NewSyncContext gives new sync context.
func NewSyncContext(name string) SyncContext {
	return syncContext{
		queue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), name),
	}
}

func (c syncContext) Queue() workqueue.RateLimitingInterface {
	return c.queue
}

// eventHandler provides default event handler that is added to an informers passed to controller factory.
func (c syncContext) eventHandler(queueKeysFunc ObjectQueueKeysFunc, filter EventFilterFunc) cache.ResourceEventHandler {
	resourceEventHandler := cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			runtimeObj, ok := obj.(runtime.Object)
			if !ok {
				utilruntime.HandleError(fmt.Errorf("added object %+v is not runtime Object", obj))
				return
			}
			c.enqueueKeys(queueKeysFunc(runtimeObj)...)
		},
		UpdateFunc: func(old, new interface{}) {
			runtimeObj, ok := new.(runtime.Object)
			if !ok {
				utilruntime.HandleError(fmt.Errorf("updated object %+v is not runtime Object", runtimeObj))
				return
			}
			c.enqueueKeys(queueKeysFunc(runtimeObj)...)
		},
		DeleteFunc: func(obj interface{}) {
			runtimeObj, ok := obj.(runtime.Object)
			if !ok {
				if tombstone, ok := obj.(cache.DeletedFinalStateUnknown); ok {
					c.enqueueKeys(queueKeysFunc(tombstone.Obj.(runtime.Object))...)

					return
				}
				utilruntime.HandleError(fmt.Errorf("updated object %+v is not runtime Object", runtimeObj))
				return
			}
			c.enqueueKeys(queueKeysFunc(runtimeObj)...)
		},
	}
	if filter == nil {
		return resourceEventHandler
	}
	return cache.FilteringResourceEventHandler{
		FilterFunc: filter,
		Handler:    resourceEventHandler,
	}
}

func (c syncContext) enqueueKeys(keys ...string) {
	for _, qKey := range keys {
		c.queue.Add(qKey)
	}
}
