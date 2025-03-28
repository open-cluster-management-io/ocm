package store

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/utils"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
)

// AgentInformerWatcherStore extends the BaseClientWatchStore.

// It gets/lists the resources from the given informer store and send
// the resource add/update/delete event to the watch channel directly.
//
// It is used for building resource agent client.
type AgentInformerWatcherStore[T generic.ResourceObject] struct {
	BaseClientWatchStore[T]
	Watcher *Watcher

	informer cache.SharedIndexInformer
}

func NewAgentInformerWatcherStore[T generic.ResourceObject]() *AgentInformerWatcherStore[T] {
	return &AgentInformerWatcherStore[T]{
		BaseClientWatchStore: BaseClientWatchStore[T]{},
		Watcher:              NewWatcher(),
	}
}

func (s *AgentInformerWatcherStore[T]) Add(resource runtime.Object) error {
	s.Watcher.Receive(watch.Event{Type: watch.Added, Object: resource})
	return nil
}

func (s *AgentInformerWatcherStore[T]) Update(resource runtime.Object) error {
	s.Watcher.Receive(watch.Event{Type: watch.Modified, Object: resource})
	return nil
}

func (s *AgentInformerWatcherStore[T]) Delete(resource runtime.Object) error {
	s.Watcher.Receive(watch.Event{Type: watch.Deleted, Object: resource})
	return nil
}

func (s *AgentInformerWatcherStore[T]) HandleReceivedResource(action types.ResourceAction, resource T) error {
	switch action {
	case types.Added:
		newObj, err := utils.ToRuntimeObject(resource)
		if err != nil {
			return err
		}

		return s.Add(newObj)
	case types.Modified:
		newObj, err := meta.Accessor(resource)
		if err != nil {
			return err
		}

		lastObj, exists, err := s.Get(newObj.GetNamespace(), newObj.GetName())
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("the resource %s/%s does not exist", newObj.GetNamespace(), newObj.GetName())
		}

		// prevent the resource from being updated if it is deleting
		if !lastObj.GetDeletionTimestamp().IsZero() {
			klog.Warningf("the resource %s/%s is deleting, ignore the update", newObj.GetNamespace(), newObj.GetName())
			return nil
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

		if len(newObj.GetFinalizers()) != 0 {
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

		return s.Delete(deletingObj)
	default:
		return fmt.Errorf("unsupported resource action %s", action)
	}
}

func (s *AgentInformerWatcherStore[T]) GetWatcher(namespace string, opts metav1.ListOptions) (watch.Interface, error) {
	return s.Watcher, nil
}

func (s *AgentInformerWatcherStore[T]) HasInitiated() bool {
	return s.Initiated && s.informer.HasSynced()
}

func (s *AgentInformerWatcherStore[T]) SetInformer(informer cache.SharedIndexInformer) {
	s.informer = informer
	s.Store = informer.GetStore()
	s.Initiated = true
}
