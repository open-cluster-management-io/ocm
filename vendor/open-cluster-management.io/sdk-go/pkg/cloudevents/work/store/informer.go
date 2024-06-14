package store

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	workv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
)

// SourceInformerWatcherStore extends the baseStore.
// It gets/lists the works from the given informer store and send
// the work add/update/delete event to the watch channel directly.
//
// It is used for building ManifestWork source client.
type SourceInformerWatcherStore struct {
	baseStore
}

var _ watch.Interface = &SourceInformerWatcherStore{}
var _ WorkClientWatcherStore = &SourceInformerWatcherStore{}

func NewSourceInformerWatcherStore(ctx context.Context) *SourceInformerWatcherStore {
	s := &SourceInformerWatcherStore{
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

func (s *SourceInformerWatcherStore) Add(work *workv1.ManifestWork) error {
	s.result <- watch.Event{Type: watch.Added, Object: work}
	return nil
}

func (s *SourceInformerWatcherStore) Update(work *workv1.ManifestWork) error {
	s.result <- watch.Event{Type: watch.Modified, Object: work}
	return nil
}

func (s *SourceInformerWatcherStore) Delete(work *workv1.ManifestWork) error {
	s.result <- watch.Event{Type: watch.Deleted, Object: work}
	return nil
}

func (s *SourceInformerWatcherStore) HasInitiated() bool {
	return s.initiated
}

func (s *SourceInformerWatcherStore) SetStore(store cache.Store) {
	s.store = store
	s.initiated = true
}

// AgentInformerWatcherStore extends the baseStore.
// It gets/lists the works from the given informer store and send
// the work add/update/delete event to the watch channel directly.
//
// It is used for building ManifestWork agent client.
type AgentInformerWatcherStore struct {
	baseStore
}

var _ watch.Interface = &AgentInformerWatcherStore{}
var _ WorkClientWatcherStore = &AgentInformerWatcherStore{}

func NewAgentInformerWatcherStore() *AgentInformerWatcherStore {
	return &AgentInformerWatcherStore{
		baseStore: baseStore{
			result: make(chan watch.Event),
			done:   make(chan struct{}),
		},
	}
}

func (s *AgentInformerWatcherStore) Add(work *workv1.ManifestWork) error {
	s.result <- watch.Event{Type: watch.Added, Object: work}
	return nil
}

func (s *AgentInformerWatcherStore) Update(work *workv1.ManifestWork) error {
	s.result <- watch.Event{Type: watch.Modified, Object: work}
	return nil
}

func (s *AgentInformerWatcherStore) Delete(work *workv1.ManifestWork) error {
	s.result <- watch.Event{Type: watch.Deleted, Object: work}
	return nil
}

func (s *AgentInformerWatcherStore) HandleReceivedWork(action types.ResourceAction, work *workv1.ManifestWork) error {
	switch action {
	case types.Added:
		return s.Add(work.DeepCopy())
	case types.Modified:
		lastWork, err := s.Get(work.Namespace, work.Name)
		if err != nil {
			return err
		}

		updatedWork := work.DeepCopy()

		// restore the fields that are maintained by local agent
		updatedWork.Labels = lastWork.Labels
		updatedWork.Annotations = lastWork.Annotations
		updatedWork.Finalizers = lastWork.Finalizers
		updatedWork.Status = lastWork.Status

		return s.Update(updatedWork)
	case types.Deleted:
		// the manifestwork is deleting on the source, we just update its deletion timestamp.
		lastWork, err := s.Get(work.Namespace, work.Name)
		if errors.IsNotFound(err) {
			return nil
		}

		if err != nil {
			return err
		}

		updatedWork := lastWork.DeepCopy()
		updatedWork.DeletionTimestamp = work.DeletionTimestamp
		return s.Update(updatedWork)
	default:
		return fmt.Errorf("unsupported resource action %s", action)
	}
}

func (s *AgentInformerWatcherStore) HasInitiated() bool {
	return s.initiated
}

func (s *AgentInformerWatcherStore) SetStore(store cache.Store) {
	s.store = store
	s.initiated = true
}
