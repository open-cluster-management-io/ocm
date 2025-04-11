package store

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/klog/v2"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
)

const syncedPollPeriod = 100 * time.Millisecond

// ResourceList is a collection of resources.
type ResourceList[T generic.ResourceObject] struct {
	// ListMeta describes list metadata
	metav1.ListMeta

	// Items is a list of resources.
	Items []T
}

// StoreInitiated is a function that can be used to determine if a store has initiated.
type StoreInitiated func() bool

// ClientWatcherStore provides a watcher with a resource store.
type ClientWatcherStore[T generic.ResourceObject] interface {
	// GetWatcher returns a watcher to receive resource changes.
	GetWatcher(namespace string, opts metav1.ListOptions) (watch.Interface, error)

	// HandleReceivedResource handles the client received resource events.
	HandleReceivedResource(action types.ResourceAction, resource T) error

	// Add will be called by resource client when adding resources. The implementation is based on the specific
	// watcher store, in some case, it does not need to update a store, but just send a watch event.
	Add(resource runtime.Object) error

	// Update will be called by resource client when updating works. The implementation is based on the specific
	// watcher store, in some case, it does not need to update a store, but just send a watch event.
	Update(resource runtime.Object) error

	// Delete will be called by resource client when deleting works. The implementation is based on the specific
	// watcher store, in some case, it does not need to update a store, but just send a watch event.
	Delete(resource runtime.Object) error

	// List returns the resources from store for a given namespace with list options
	List(namespace string, opts metav1.ListOptions) (*ResourceList[T], error)

	// ListAll list all of the resources from store
	ListAll() ([]T, error)

	// Get returns a resource from store with resource namespace and name
	Get(namespace, name string) (T, bool, error)

	// HasInitiated marks the store has been initiated, A resync may be required after the store is initiated
	// when building a resource client.
	HasInitiated() bool
}

func WaitForStoreInit(ctx context.Context, cacheSyncs ...StoreInitiated) bool {
	err := wait.PollUntilContextCancel(
		ctx,
		syncedPollPeriod,
		true,
		func(ctx context.Context) (bool, error) {
			for _, syncFunc := range cacheSyncs {
				if !syncFunc() {
					return false, nil
				}
			}
			return true, nil
		},
	)
	if err != nil {
		klog.Errorf("stop WaitForStoreInit, %v", err)
		return false
	}

	return true
}
