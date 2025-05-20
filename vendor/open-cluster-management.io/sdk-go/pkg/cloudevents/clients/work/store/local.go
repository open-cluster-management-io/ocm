package store

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	workv1 "open-cluster-management.io/api/work/v1"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/store"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/utils"
)

// ListLocalWorksFunc loads the works from the local environment.
type ListLocalWorksFunc func(ctx context.Context) ([]*workv1.ManifestWork, error)

type watchEvent struct {
	Key  string
	Type watch.EventType
}

var _ store.ClientWatcherStore[*workv1.ManifestWork] = &SourceLocalWatcherStore{}

// SourceLocalWatcherStore caches the works in this local store and provide the watch ability by watch event channel.
//
// It is used for building ManifestWork source client.
type SourceLocalWatcherStore struct {
	baseSourceStore
	watcher    *store.Watcher
	eventQueue cache.Queue
}

// NewSourceLocalWatcherStore returns a LocalWatcherStore with works that list by ListLocalWorksFunc
func NewSourceLocalWatcherStore(ctx context.Context, listFunc ListLocalWorksFunc) (*SourceLocalWatcherStore, error) {
	works, err := listFunc(ctx)
	if err != nil {
		return nil, err
	}

	// A local localStore to cache the works
	localStore := cache.NewStore(cache.MetaNamespaceKeyFunc)
	for _, work := range works {
		if errs := utils.ValidateWork(work); len(errs) != 0 {
			return nil, fmt.Errorf("%s", errs.ToAggregate().Error())
		}

		if err := localStore.Add(work.DeepCopy()); err != nil {
			return nil, err
		}
	}

	s := &SourceLocalWatcherStore{
		baseSourceStore: baseSourceStore{
			BaseClientWatchStore: store.BaseClientWatchStore[*workv1.ManifestWork]{
				Store:     localStore,
				Initiated: true,
			},

			// A queue to save the received work events, it helps us retry events
			// where errors occurred while processing
			receivedWorks: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "local-watcher-store"), // nolint:staticcheck // SA1019
		},

		watcher: store.NewWatcher(),

		// A queue to save the work client send events, if run a client without a watcher,
		// it will block the client, this queue helps to resolve this blocking.
		// Only save the latest event for a work.
		eventQueue: cache.NewFIFO(func(obj interface{}) (string, error) {
			evt, ok := obj.(*watchEvent)
			if !ok {
				return "", fmt.Errorf("unknown object type %T", obj)
			}

			return evt.Key, nil
		}),
	}

	// start a goroutine to process the received work events from the work queue with current store.
	go newWorkProcessor(s.receivedWorks, s).run(ctx.Done())

	// start a goroutine to handle the events that are produced by work client
	go wait.Until(s.processLoop, time.Second, ctx.Done())

	return s, nil
}

// Add a work to the cache and send an event to the event queue
func (s *SourceLocalWatcherStore) Add(work runtime.Object) error {
	s.Lock()
	defer s.Unlock()

	if err := s.Store.Add(work); err != nil {
		return err
	}

	key, err := key(work)
	if err != nil {
		return err
	}

	return s.eventQueue.Add(&watchEvent{Key: key, Type: watch.Added})
}

// Update a work in the cache and send an event to the event queue
func (s *SourceLocalWatcherStore) Update(work runtime.Object) error {
	s.Lock()
	defer s.Unlock()

	if err := s.Store.Update(work); err != nil {
		return err
	}

	key, err := key(work)
	if err != nil {
		return err
	}

	return s.eventQueue.Update(&watchEvent{Key: key, Type: watch.Modified})
}

// Delete a work from the cache and send an event to the event queue
func (s *SourceLocalWatcherStore) Delete(work runtime.Object) error {
	s.Lock()
	defer s.Unlock()

	if err := s.Store.Delete(work); err != nil {
		return err
	}

	key, err := key(work)
	if err != nil {
		return err
	}

	return s.eventQueue.Update(&watchEvent{Key: key, Type: watch.Deleted})
}

func (s *SourceLocalWatcherStore) HasInitiated() bool {
	return s.Initiated
}

func (s *SourceLocalWatcherStore) GetWatcher(namespace string, opts metav1.ListOptions) (watch.Interface, error) {
	// TODO may consider to support watch with namespace
	if namespace != metav1.NamespaceAll {
		return nil, fmt.Errorf("unsupported to watch from the namespace %s", namespace)
	}

	return s.watcher, nil
}

// processLoop drains the work event queue and send the event to the watch channel.
func (s *SourceLocalWatcherStore) processLoop() {
	for {
		// this will be blocked until the event queue has events
		obj, err := s.eventQueue.Pop(func(interface{}, bool) error {
			// do nothing
			return nil
		})
		if err != nil {
			if err == cache.ErrFIFOClosed {
				return
			}

			klog.Warningf("failed to pop the %v requeue it, %v", obj, err)
			// this is the safe way to re-enqueue.
			if err := s.eventQueue.AddIfNotPresent(obj); err != nil {
				klog.Errorf("failed to requeue the obj %v, %v", obj, err)
				return
			}
		}

		evt, ok := obj.(*watchEvent)
		if !ok {
			klog.Errorf("unknown the object type %T from the event queue", obj)
			return
		}

		obj, exists, err := s.Store.GetByKey(evt.Key)
		if err != nil {
			klog.Errorf("failed to get the work %s, %v", evt.Key, err)
			return
		}

		if !exists {
			if evt.Type == watch.Deleted {
				namespace, name, err := cache.SplitMetaNamespaceKey(evt.Key)
				if err != nil {
					klog.Errorf("unexpected event key %s, %v", evt.Key, err)
					return
				}

				// the work has been deleted, return a work only with its namespace and name
				// this will be blocked until this event is consumed
				s.watcher.Receive(watch.Event{
					Type: watch.Deleted,
					Object: &workv1.ManifestWork{
						ObjectMeta: metav1.ObjectMeta{
							Name:      name,
							Namespace: namespace,
						},
					},
				})
				return
			}

			klog.Errorf("the work %s does not exist in the cache", evt.Key)
			return
		}

		work, ok := obj.(*workv1.ManifestWork)
		if !ok {
			klog.Errorf("unknown the object type %T from the cache", obj)
			return
		}

		// this will be blocked until this event is consumed
		s.watcher.Receive(watch.Event{Type: evt.Type, Object: work})
	}
}

func key(obj runtime.Object) (string, error) {
	work, ok := obj.(*workv1.ManifestWork)
	if !ok {
		return "", fmt.Errorf("obj %T is not a work", obj)
	}
	return work.Namespace + "/" + work.Name, nil
}
