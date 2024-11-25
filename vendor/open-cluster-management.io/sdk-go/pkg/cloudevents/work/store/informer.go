package store

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	workv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
)

// SourceInformerWatcherStore extends the baseStore.
// It gets/lists the works from the given informer store and send
// the work add/update/delete event to the watch channel directly.
//
// It is used for building ManifestWork source client.
type SourceInformerWatcherStore struct {
	baseSourceStore
	watcher  *workWatcher
	informer cache.SharedIndexInformer
}

var _ WorkClientWatcherStore = &SourceInformerWatcherStore{}

func NewSourceInformerWatcherStore(ctx context.Context) *SourceInformerWatcherStore {
	s := &SourceInformerWatcherStore{
		baseSourceStore: baseSourceStore{
			baseStore:     baseStore{},
			receivedWorks: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "informer-watcher-store"),
		},
		watcher: newWorkWatcher(),
	}

	// start a goroutine to process the received work events from the work queue with current store.
	go newWorkProcessor(s.receivedWorks, s).run(ctx.Done())

	return s
}

func (s *SourceInformerWatcherStore) Add(work *workv1.ManifestWork) error {
	s.watcher.Receive(watch.Event{Type: watch.Added, Object: work})
	return nil
}

func (s *SourceInformerWatcherStore) Update(work *workv1.ManifestWork) error {
	s.watcher.Receive(watch.Event{Type: watch.Modified, Object: work})
	return nil
}

func (s *SourceInformerWatcherStore) Delete(work *workv1.ManifestWork) error {
	s.watcher.Receive(watch.Event{Type: watch.Deleted, Object: work})
	return nil
}

func (s *SourceInformerWatcherStore) HasInitiated() bool {
	return s.initiated && s.informer.HasSynced()
}

func (s *SourceInformerWatcherStore) GetWatcher(namespace string, opts metav1.ListOptions) (watch.Interface, error) {
	if namespace != metav1.NamespaceAll {
		return nil, fmt.Errorf("unsupported to watch from the namespace %s", namespace)
	}

	return s.watcher, nil
}

func (s *SourceInformerWatcherStore) SetInformer(informer cache.SharedIndexInformer) {
	s.informer = informer
	s.store = informer.GetStore()
	s.initiated = true
}

// AgentInformerWatcherStore extends the baseStore.
// It gets/lists the works from the given informer store and send
// the work add/update/delete event to the watch channel directly.
//
// It is used for building ManifestWork agent client.
type AgentInformerWatcherStore struct {
	baseStore
	informer cache.SharedIndexInformer
	watcher  *workWatcher
}

var _ WorkClientWatcherStore = &AgentInformerWatcherStore{}

func NewAgentInformerWatcherStore() *AgentInformerWatcherStore {
	return &AgentInformerWatcherStore{
		baseStore: baseStore{},
		watcher:   newWorkWatcher(),
	}
}

func (s *AgentInformerWatcherStore) Add(work *workv1.ManifestWork) error {
	s.watcher.Receive(watch.Event{Type: watch.Added, Object: work})
	return nil
}

func (s *AgentInformerWatcherStore) Update(work *workv1.ManifestWork) error {
	s.watcher.Receive(watch.Event{Type: watch.Modified, Object: work})
	return nil
}

func (s *AgentInformerWatcherStore) Delete(work *workv1.ManifestWork) error {
	s.watcher.Receive(watch.Event{Type: watch.Deleted, Object: work})
	return nil
}

func (s *AgentInformerWatcherStore) HandleReceivedWork(action types.ResourceAction, work *workv1.ManifestWork) error {
	switch action {
	case types.Added:
		return s.Add(work.DeepCopy())
	case types.Modified:
		lastWork, exists, err := s.Get(work.Namespace, work.Name)
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("the work %s/%s does not exist", work.Namespace, work.Name)
		}
		// prevent the work from being updated if it is deleting
		if !lastWork.GetDeletionTimestamp().IsZero() {
			klog.Warningf("the work %s/%s is deleting, ignore the update", work.Namespace, work.Name)
			return nil
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
		lastWork, exists, err := s.Get(work.Namespace, work.Name)
		if err != nil {
			return err
		}
		if !exists {
			return nil
		}

		updatedWork := lastWork.DeepCopy()
		updatedWork.DeletionTimestamp = work.DeletionTimestamp
		return s.Update(updatedWork)
	default:
		return fmt.Errorf("unsupported resource action %s", action)
	}
}

func (s *AgentInformerWatcherStore) GetWatcher(namespace string, opts metav1.ListOptions) (watch.Interface, error) {
	return s.watcher, nil
}

func (s *AgentInformerWatcherStore) HasInitiated() bool {
	return s.initiated && s.informer.HasSynced()
}

func (s *AgentInformerWatcherStore) SetInformer(informer cache.SharedIndexInformer) {
	s.informer = informer
	s.store = informer.GetStore()
	s.initiated = true
}
