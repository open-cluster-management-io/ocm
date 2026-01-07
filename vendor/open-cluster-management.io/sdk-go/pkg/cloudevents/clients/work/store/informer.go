package store

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	"k8s.io/apimachinery/pkg/api/meta"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubetypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	workv1 "open-cluster-management.io/api/work/v1"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/store"
)

// SourceInformerWatcherStore extends the baseStore.
// It gets/lists the works from the given informer store and send
// the work add/update/delete event to the watch channel directly.
//
// It is used for building ManifestWork source client.
type SourceInformerWatcherStore struct {
	baseSourceStore
	watcher  *store.Watcher
	informer cache.SharedIndexInformer
}

var _ store.ClientWatcherStore[*workv1.ManifestWork] = &SourceInformerWatcherStore{}

func NewSourceInformerWatcherStore(ctx context.Context) *SourceInformerWatcherStore {
	s := &SourceInformerWatcherStore{
		baseSourceStore: baseSourceStore{
			BaseClientWatchStore: store.BaseClientWatchStore[*workv1.ManifestWork]{},
			receivedWorks: workqueue.NewTypedRateLimitingQueueWithConfig(
				workqueue.DefaultTypedControllerRateLimiter[*workv1.ManifestWork](),
				workqueue.TypedRateLimitingQueueConfig[*workv1.ManifestWork]{Name: "informer-watcher-store"},
			),
		},
		watcher: store.NewWatcher(),
	}

	// start a goroutine to process the received work events from the work queue with current store.
	go newWorkProcessor(s.receivedWorks, s).run(ctx)

	return s
}

func (s *SourceInformerWatcherStore) Add(work runtime.Object) error {
	s.watcher.Receive(watch.Event{Type: watch.Added, Object: work})
	return nil
}

func (s *SourceInformerWatcherStore) Update(work runtime.Object) error {
	s.watcher.Receive(watch.Event{Type: watch.Modified, Object: work})
	return nil
}

func (s *SourceInformerWatcherStore) Delete(work runtime.Object) error {
	s.watcher.Receive(watch.Event{Type: watch.Deleted, Object: work})
	return nil
}

func (s *SourceInformerWatcherStore) HasInitiated() bool {
	return s.Initiated && s.informer.HasSynced()
}

func (s *SourceInformerWatcherStore) GetWatcher(ctx context.Context, namespace string, opts metav1.ListOptions) (watch.Interface, error) {
	if namespace != metav1.NamespaceAll {
		return nil, fmt.Errorf("unsupported to watch from the namespace %s", namespace)
	}

	return s.watcher, nil
}

func (s *SourceInformerWatcherStore) SetInformer(informer cache.SharedIndexInformer) {
	s.informer = informer
	s.Store = informer.GetStore()
	s.Initiated = true
}

// AgentInformerWatcherStore extends the baseStore.
// It gets/lists the works from the given informer store and send
// the work add/update/delete event to the watch channel directly.
//
// It is used for building ManifestWork agent client.
type AgentInformerWatcherStore struct {
	store.AgentInformerWatcherStore[*workv1.ManifestWork]

	versions *versioner
}

type versioner struct {
	versions map[string]int64
	lock     sync.RWMutex
}

func newVersioner() *versioner {
	return &versioner{
		versions: make(map[string]int64),
	}
}

func (v *versioner) increment(name string) int64 {
	v.lock.Lock()
	defer v.lock.Unlock()

	if _, ok := v.versions[name]; !ok {
		v.versions[name] = 1
	} else {
		v.versions[name] = v.versions[name] + 1
	}

	return v.versions[name]
}

func (v *versioner) delete(name string) {
	v.lock.Lock()
	defer v.lock.Unlock()
	delete(v.versions, name)
}

var _ store.ClientWatcherStore[*workv1.ManifestWork] = &AgentInformerWatcherStore{}

func NewAgentInformerWatcherStore() *AgentInformerWatcherStore {
	return &AgentInformerWatcherStore{
		AgentInformerWatcherStore: store.AgentInformerWatcherStore[*workv1.ManifestWork]{
			BaseClientWatchStore: store.BaseClientWatchStore[*workv1.ManifestWork]{
				Store: cache.NewStore(cache.MetaNamespaceKeyFunc),
			},
			Watcher: store.NewWatcher(),
		},
		versions: newVersioner(),
	}
}

func (s *AgentInformerWatcherStore) Add(resource runtime.Object) error {
	accessor, err := meta.Accessor(resource)
	if err != nil {
		return err
	}
	accessor.SetResourceVersion(strconv.FormatInt(s.versions.increment(accessor.GetName()), 10))
	return s.AgentInformerWatcherStore.Add(resource)
}

func (s *AgentInformerWatcherStore) Update(resource runtime.Object) error {
	accessor, err := meta.Accessor(resource)
	if err != nil {
		return err
	}
	accessor.SetResourceVersion(strconv.FormatInt(s.versions.increment(accessor.GetName()), 10))
	return s.AgentInformerWatcherStore.Update(resource)
}

func (s *AgentInformerWatcherStore) Delete(resource runtime.Object) error {
	accessor, err := meta.Accessor(resource)
	if err != nil {
		return err
	}
	s.versions.delete(accessor.GetName())
	return s.AgentInformerWatcherStore.Delete(resource)
}

func (s *AgentInformerWatcherStore) HandleReceivedResource(ctx context.Context, work *workv1.ManifestWork) error {
	// for compatibility, we get the work by its UID
	// TODO get the work by its namespace/name
	existingWorks, err := s.findWorksByUID(ctx, work.UID)
	if err != nil {
		return err
	}

	if len(existingWorks) == 0 {
		return s.Add(work.DeepCopy())
	}

	lastWork := s.getWork(existingWorks, work)
	if lastWork == nil {
		// For compatibility, if a work is found by UID but not by namespace/name,
		// it means the work's name has changed — replace the existing work with the new one.
		if err := s.Add(work.DeepCopy()); err != nil {
			return err
		}

		for _, obsoleted := range existingWorks {
			if err := s.Store.Delete(obsoleted); err != nil {
				return err
			}
		}

		return nil
	}

	if !work.DeletionTimestamp.IsZero() {
		deletingWork := lastWork.DeepCopy()
		// update the deletionTimeStamp and generation of last work.
		// generation needs to be updated because it is possible that generation still change after
		// the object is in deleting state.
		deletingWork.DeletionTimestamp = work.DeletionTimestamp
		deletingWork.Generation = work.Generation
		return s.Update(deletingWork)
	}

	// Skip processing if the incoming work has a valid but stale generation.
	// We only consider generation for freshness when it is non-zero;
	// if work generation is 0, it is treated as "unversioned" and not compared.
	// This avoids incorrectly rejecting unversioned work when lastWork.generation is non-zero,
	// and also handles the case where both are 0 (no meaningful order).
	if work.Generation != 0 && work.Generation < lastWork.Generation {
		return nil
	}

	updatedWork := work.DeepCopy()
	// restore the fields that are maintained by local agent.
	updatedWork.Finalizers = lastWork.Finalizers
	updatedWork.Status = lastWork.Status
	return s.Update(updatedWork)
}

func (s *AgentInformerWatcherStore) findWorksByUID(ctx context.Context, uid kubetypes.UID) ([]*workv1.ManifestWork, error) {
	existingWorks := []*workv1.ManifestWork{}
	works, err := s.ListAll(ctx)
	if err != nil {
		return existingWorks, err
	}
	for _, work := range works {
		if work.GetUID() == uid {
			existingWorks = append(existingWorks, work.DeepCopy())
		}
	}

	return existingWorks, nil
}

func (s *AgentInformerWatcherStore) getWork(existingWorks []*workv1.ManifestWork, work *workv1.ManifestWork) *workv1.ManifestWork {
	for _, existing := range existingWorks {
		if existing.Namespace == work.Namespace &&
			existing.Name == work.Name {
			return existing
		}
	}

	return nil
}
