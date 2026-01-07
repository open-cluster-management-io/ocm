package store

import (
	"context"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/utils"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic"
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

func (s *SimpleStore[T]) GetWatcher(ctx context.Context, namespace string, opts metav1.ListOptions) (watch.Interface, error) {
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

func (s *SimpleStore[T]) HandleReceivedResource(ctx context.Context, resource T) error {
	runtimeObj, err := utils.ToRuntimeObject(resource)
	if err != nil {
		return err
	}

	metaObj, err := meta.Accessor(runtimeObj)
	if err != nil {
		return err
	}

	_, exists, err := s.Get(ctx, metaObj.GetNamespace(), metaObj.GetName())
	if err != nil {
		return err
	}
	if !exists {
		return s.Add(runtimeObj)
	}

	if !metaObj.GetDeletionTimestamp().IsZero() {
		if len(metaObj.GetFinalizers()) != 0 {
			return nil
		}
		return s.Delete(runtimeObj)
	}

	return s.Update(runtimeObj)
}
