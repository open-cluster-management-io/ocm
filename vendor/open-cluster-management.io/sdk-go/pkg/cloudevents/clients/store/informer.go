package store

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/utils"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
)

// AgentInformerWatcherStore extends the BaseClientWatchStore.

// It gets/lists the resources from the given local store and send
// the resource add/update/delete event to the watch channel directly.
//
// It is used for building resource agent client.
type AgentInformerWatcherStore[T generic.ResourceObject] struct {
	BaseClientWatchStore[T]
	Watcher *Watcher
}

func NewAgentInformerWatcherStore[T generic.ResourceObject]() *AgentInformerWatcherStore[T] {
	return &AgentInformerWatcherStore[T]{
		BaseClientWatchStore: BaseClientWatchStore[T]{
			Store: cache.NewStore(cache.MetaNamespaceKeyFunc),
		},
		Watcher: NewWatcher(),
	}
}

func (s *AgentInformerWatcherStore[T]) Add(resource runtime.Object) error {
	s.Watcher.Receive(watch.Event{Type: watch.Added, Object: resource})
	return s.Store.Add(resource)
}

func (s *AgentInformerWatcherStore[T]) Update(resource runtime.Object) error {
	s.Watcher.Receive(watch.Event{Type: watch.Modified, Object: resource})
	return s.Store.Update(resource)
}

func (s *AgentInformerWatcherStore[T]) Delete(resource runtime.Object) error {
	s.Watcher.Receive(watch.Event{Type: watch.Deleted, Object: resource})
	return s.Store.Delete(resource)
}

func (s *AgentInformerWatcherStore[T]) HandleReceivedResource(ctx context.Context, action types.ResourceAction, resource T) error {
	switch action {
	case types.Added:
		newObj, err := utils.ToRuntimeObject(resource)
		if err != nil {
			return err
		}

		return s.Add(newObj)
	case types.Modified:
		accessor, err := meta.Accessor(resource)
		if err != nil {
			return err
		}

		lastObj, exists, err := s.Get(accessor.GetNamespace(), accessor.GetName())
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("the resource %s/%s does not exist", accessor.GetNamespace(), accessor.GetName())
		}

		// if resource is deleting, keep the deletion timestamp
		if !lastObj.GetDeletionTimestamp().IsZero() {
			accessor.SetDeletionTimestamp(lastObj.GetDeletionTimestamp())
		}

		updated, err := utils.ToRuntimeObject(resource)
		if err != nil {
			return err
		}

		return s.Update(updated)
	case types.Deleted:
		newObj, err := meta.Accessor(resource)
		if err != nil {
			return err
		}

		if newObj.GetDeletionTimestamp().IsZero() {
			return nil
		}

		last, exists, err := s.Get(newObj.GetNamespace(), newObj.GetName())
		if err != nil {
			return err
		}
		if !exists {
			return nil
		}

		deletingObj, err := utils.ToRuntimeObject(last)
		if err != nil {
			return err
		}

		// trigger an update event if the object is deleting.
		// Only need to update generation/finalizer/deletionTimeStamp of the object.
		if len(newObj.GetFinalizers()) != 0 {
			accessor, err := meta.Accessor(deletingObj)
			if err != nil {
				return err
			}
			accessor.SetDeletionTimestamp(newObj.GetDeletionTimestamp())
			accessor.SetFinalizers(newObj.GetFinalizers())
			accessor.SetGeneration(newObj.GetGeneration())
			return s.Update(deletingObj)
		}

		return s.Delete(deletingObj)
	default:
		return fmt.Errorf("unsupported resource action %s", action)
	}
}

func (s *AgentInformerWatcherStore[T]) GetWatcher(namespace string, opts metav1.ListOptions) (watch.Interface, error) {
	return s.Watcher, nil
}

func (s *AgentInformerWatcherStore[T]) HasInitiated() bool {
	return true
}
