package store

import (
	"context"

	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	workv1 "open-cluster-management.io/api/work/v1"
)

// InformerWatcherStore extends the baseStore.
// It gets/lists the works from the given informer store and send
// the work add/update/delete event to the watch channel directly.
type InformerWatcherStore struct {
	baseStore
}

var _ watch.Interface = &LocalWatcherStore{}
var _ WorkClientWatcherStore = &InformerWatcherStore{}

func NewInformerWatcherStore(ctx context.Context) *InformerWatcherStore {
	s := &InformerWatcherStore{
		baseStore: baseStore{
			result:        make(chan watch.Event),
			done:          make(chan struct{}),
			receivedWorks: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "informer-watcher-store"),
		},
	}

	// start a goroutine to process the received work events from the work queue with current store.
	go newWorkProcessor(s.baseStore.receivedWorks, s).run(ctx.Done())

	return s
}

func (s *InformerWatcherStore) Add(work *workv1.ManifestWork) error {
	s.result <- watch.Event{Type: watch.Added, Object: work}
	return nil
}

func (s *InformerWatcherStore) Update(work *workv1.ManifestWork) error {
	s.result <- watch.Event{Type: watch.Modified, Object: work}
	return nil
}

func (s *InformerWatcherStore) Delete(work *workv1.ManifestWork) error {
	s.result <- watch.Event{Type: watch.Deleted, Object: work}
	return nil
}

func (s *InformerWatcherStore) HasInitiated() bool {
	return s.initiated
}

func (s *InformerWatcherStore) SetStore(store cache.Store) {
	s.store = store
	s.initiated = true
}
