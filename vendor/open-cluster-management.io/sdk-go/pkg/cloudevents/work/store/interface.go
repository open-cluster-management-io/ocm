package store

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/klog/v2"

	workv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
)

const syncedPollPeriod = 100 * time.Millisecond

// StoreInitiated is a function that can be used to determine if a store has initiated.
type StoreInitiated func() bool

// WorkClientWatcherStore provides a watcher with a work store.
type WorkClientWatcherStore interface {
	// GetWatcher returns a watcher to receive work changes.
	GetWatcher(namespace string, opts metav1.ListOptions) (watch.Interface, error)

	// HandleReceivedWork handles the client received work events.
	HandleReceivedWork(action types.ResourceAction, work *workv1.ManifestWork) error

	// Add will be called by work client when adding works. The implementation is based on the specific
	// watcher store, in some case, it does not need to update a store, but just send a watch event.
	Add(work *workv1.ManifestWork) error

	// Update will be called by work client when updating works. The implementation is based on the specific
	// watcher store, in some case, it does not need to update a store, but just send a watch event.
	Update(work *workv1.ManifestWork) error

	// Delete will be called by work client when deleting works. The implementation is based on the specific
	// watcher store, in some case, it does not need to update a store, but just send a watch event.
	Delete(work *workv1.ManifestWork) error

	// List returns the works from store for a given namespace with list options
	List(namespace string, opts metav1.ListOptions) (*workv1.ManifestWorkList, error)

	// ListAll list all of the works from store
	ListAll() ([]*workv1.ManifestWork, error)

	// Get returns a work from store with work namespace and name
	Get(namespace, name string) (*workv1.ManifestWork, bool, error)

	// HasInitiated marks the store has been initiated, A resync may be required after the store is initiated
	// when building a work client.
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
