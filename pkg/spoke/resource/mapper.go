package resource

import (
	"fmt"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/restmapper"
)

// Mapper is a struct to define resource mapping
type Mapper struct {
	Mapper   meta.RESTMapper
	syncLock sync.RWMutex
}

// NewMapper is to create the mapper struct
func NewMapper(discoveryclient discovery.CachedDiscoveryInterface) *Mapper {
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(discoveryclient)

	return &Mapper{
		Mapper: mapper,
	}
}

// Run start the refresh goroutine
func (p *Mapper) Run(stopCh <-chan struct{}) {
	go wait.Until(func() {
		p.syncLock.Lock()
		defer p.syncLock.Unlock()
		deferredMappd := p.Mapper.(*restmapper.DeferredDiscoveryRESTMapper)
		deferredMappd.Reset()
	}, 30*time.Second, stopCh)
}

// MappingForGVK returns the RESTMapping for a gvk
func (p *Mapper) MappingForGVK(gvk schema.GroupVersionKind) (*meta.RESTMapping, error) {
	p.syncLock.RLock()
	defer p.syncLock.RUnlock()
	mapping, err := p.Mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return nil, fmt.Errorf("the server doesn't have a resource type %q", gvk.Kind)
	}

	return mapping, nil
}
