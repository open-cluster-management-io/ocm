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

// SimpleStore extends the BaseClientWatchStore.
// It handles the received resources in its local store.
//
// Note: this store does not provide a watcher
type SimpleStore[T generic.ResourceObject] struct {
	BaseClientWatchStore[T]
}

func NewSimpleStore[T generic.ResourceObject]() *SimpleStore[T] {
	return &SimpleStore[T]{
		BaseClientWatchStore[T]{
			Store: cache.NewStore(cache.DeletionHandlingMetaNamespaceKeyFunc)},
	}
}

func (s *SimpleStore[T]) GetWatcher(namespace string, opts metav1.ListOptions) (watch.Interface, error) {
	panic("watcher is unsupported")
}

func (s *SimpleStore[T]) Add(resource runtime.Object) error {
	return s.Store.Add(resource)
}

func (s *SimpleStore[T]) Update(resource runtime.Object) error {
	return s.Store.Update(resource)
}

func (s *SimpleStore[T]) Delete(resource runtime.Object) error {
	return s.Store.Delete(resource)
}

func (s *SimpleStore[T]) HasInitiated() bool {
	return true
}

func (s *SimpleStore[T]) HandleReceivedResource(action types.ResourceAction, resource T) error {
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
