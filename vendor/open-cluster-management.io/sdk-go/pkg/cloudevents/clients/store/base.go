package store

import (
	"fmt"
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/utils"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic"
)

// BaseClientWatchStore implements the ClientWatcherStore ListAll/List/Get methods
type BaseClientWatchStore[T generic.ResourceObject] struct {
	sync.RWMutex

	Store     cache.Store
	Initiated bool
}

// List the resources from the store with the list options
func (s *BaseClientWatchStore[T]) List(namespace string, opts metav1.ListOptions) (*ResourceList[T], error) {
	s.RLock()
	defer s.RUnlock()

	resources, err := utils.ListResourcesWithOptions[T](s.Store, namespace, opts)
	if err != nil {
		return nil, err
	}

	return &ResourceList[T]{Items: resources}, nil
}

// Get a resource from the store
func (s *BaseClientWatchStore[T]) Get(namespace, name string) (resource T, exists bool, err error) {
	s.RLock()
	defer s.RUnlock()

	key := name
	if len(namespace) != 0 {
		key = fmt.Sprintf("%s/%s", namespace, name)
	}

	obj, exists, err := s.Store.GetByKey(key)
	if err != nil {
		return resource, false, err
	}

	if !exists {
		return resource, false, nil
	}

	res, ok := obj.(T)
	if !ok {
		return resource, false, fmt.Errorf("unknown type %T", obj)
	}

	return res, true, nil
}

// List all of resources from the store
func (s *BaseClientWatchStore[T]) ListAll() ([]T, error) {
	s.RLock()
	defer s.RUnlock()

	resources := []T{}
	for _, obj := range s.Store.List() {
		if res, ok := obj.(T); ok {
			resources = append(resources, res)
		}
	}

	return resources, nil
}
