package objectreader

import (
	"context"
	"fmt"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	workinformers "open-cluster-management.io/api/client/work/informers/externalversions/work/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"
)

const byWorkIndex = "byWorkIndex"

// ObjectReader is an interface that reads spoke resource
type ObjectReader struct {
	dynamicClient dynamic.Interface

	informers map[informerKey]*informerWithCancel

	lock sync.RWMutex

	indexer cache.Indexer

	manifestWorkQueue workqueue.TypedRateLimitingInterface[string]
}

type informerWithCancel struct {
	informer cache.SharedIndexInformer
	lister   cache.GenericLister
	cancel   context.CancelFunc

	// This record all the evenhandler registrations, the key is
	registrations map[registrationKey]cache.ResourceEventHandlerRegistration
}

// informerKey is the key to register an informer
type informerKey struct {
	schema.GroupVersionResource
	namespace string
}

type registrationKey struct {
	schema.GroupVersionResource
	namespace string
	name      string
	workName  string
}

func NewObjectReader(dynamicClient dynamic.Interface, workInformer workinformers.ManifestWorkInformer, queue workqueue.TypedRateLimitingInterface[string]) (*ObjectReader, error) {
	err := workInformer.Informer().AddIndexers(map[string]cache.IndexFunc{
		byWorkIndex: indexWorkByResource,
	})
	if err != nil {
		return nil, err
	}

	return &ObjectReader{
		dynamicClient:     dynamicClient,
		manifestWorkQueue: queue,
		informers:         map[informerKey]*informerWithCancel{},
		indexer:           workInformer.Informer().GetIndexer(),
	}, nil
}

func (o *ObjectReader) Get(ctx context.Context, resourceMeta workapiv1.ManifestResourceMeta) (*unstructured.Unstructured, metav1.Condition, error) {
	o.lock.RLock()
	defer o.lock.RUnlock()
	if len(resourceMeta.Resource) == 0 || len(resourceMeta.Version) == 0 || len(resourceMeta.Name) == 0 {
		return nil, metav1.Condition{
			Type:    workapiv1.ManifestAvailable,
			Status:  metav1.ConditionUnknown,
			Reason:  "IncompletedResourceMeta",
			Message: "Resource meta is incompleted",
		}, fmt.Errorf("incomplete resource meta")
	}

	gvr := schema.GroupVersionResource{
		Group:    resourceMeta.Group,
		Version:  resourceMeta.Version,
		Resource: resourceMeta.Resource,
	}

	key := informerKey{GroupVersionResource: gvr, namespace: resourceMeta.Namespace}

	var err error
	var obj *unstructured.Unstructured
	if i, found := o.informers[key]; found {
		var runObj runtime.Object
		runObj, err = i.lister.ByNamespace(resourceMeta.Namespace).Get(resourceMeta.Name)
		if err == nil {
			obj = runObj.(*unstructured.Unstructured)
		}
	} else {
		obj, err = o.dynamicClient.Resource(gvr).Namespace(resourceMeta.Namespace).Get(ctx, resourceMeta.Name, metav1.GetOptions{})
	}

	switch {
	case errors.IsNotFound(err):
		return nil, metav1.Condition{
			Type:    workapiv1.ManifestAvailable,
			Status:  metav1.ConditionFalse,
			Reason:  "ResourceNotAvailable",
			Message: "Resource is not available",
		}, err
	case err != nil:
		return nil, metav1.Condition{
			Type:    workapiv1.ManifestAvailable,
			Status:  metav1.ConditionUnknown,
			Reason:  "FetchingResourceFailed",
			Message: fmt.Sprintf("Failed to fetch resource: %v", err),
		}, err
	}

	return obj, metav1.Condition{
		Type:    workapiv1.ManifestAvailable,
		Status:  metav1.ConditionTrue,
		Reason:  "ResourceAvailable",
		Message: "Resource is available",
	}, nil
}

// RegisterInformer checks if there is an informer and if the event handler has been registered to the informer.
// this is called each time a resource needs to be watched. It is idempotent.
func (o *ObjectReader) RegisterInformer(ctx context.Context, work *workapiv1.ManifestWork, resourceMeta workapiv1.ManifestResourceMeta) error {
	o.lock.Lock()
	defer o.lock.Unlock()

	gvr := schema.GroupVersionResource{
		Group:    resourceMeta.Group,
		Version:  resourceMeta.Version,
		Resource: resourceMeta.Resource,
	}

	key := informerKey{GroupVersionResource: gvr, namespace: resourceMeta.Namespace}
	regKey := registrationKey{
		GroupVersionResource: gvr,
		namespace:            resourceMeta.Namespace,
		name:                 resourceMeta.Name,
		workName:             work.Name,
	}
	informer, found := o.informers[key]
	if found {
		// check if the event handler has been registered.
		if _, registrationFound := informer.registrations[regKey]; registrationFound {
			return nil
		}
		// Add event handler into the informer so it can trigger work reconcile
		registration, err := informer.informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc: o.queueWorkByResourceFunc(ctx, gvr),
			UpdateFunc: func(old, new interface{}) {
				o.queueWorkByResourceFunc(ctx, gvr)(new)
			},
		})
		if err != nil {
			return err
		}
		// record the event handler registration
		informer.registrations[regKey] = registration
		return nil
	}

	resourceInformer := dynamicinformer.NewFilteredDynamicInformer(
		o.dynamicClient, gvr, resourceMeta.Namespace, 24*time.Hour,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}, nil)
	registration, err := resourceInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: o.queueWorkByResourceFunc(ctx, gvr),
		UpdateFunc: func(old, new interface{}) {
			o.queueWorkByResourceFunc(ctx, gvr)(new)
		},
	})
	if err != nil {
		return err
	}
	informerCtx, cancel := context.WithCancel(ctx)
	o.informers[key] = &informerWithCancel{
		informer: resourceInformer.Informer(),
		cancel:   cancel,
		lister:   resourceInformer.Lister(),
		registrations: map[registrationKey]cache.ResourceEventHandlerRegistration{
			regKey: registration,
		},
	}
	go resourceInformer.Informer().Run(informerCtx.Done())
	return nil
}

// UnRegisterInformer is called each time a resource is not watched.
func (o *ObjectReader) UnRegisterInformer(work *workapiv1.ManifestWork, resourceMeta workapiv1.ManifestResourceMeta) error {
	o.lock.Lock()
	defer o.lock.Unlock()

	gvr := schema.GroupVersionResource{
		Group:    resourceMeta.Group,
		Version:  resourceMeta.Version,
		Resource: resourceMeta.Resource,
	}

	key := informerKey{GroupVersionResource: gvr, namespace: resourceMeta.Namespace}
	regKey := registrationKey{
		GroupVersionResource: gvr,
		namespace:            resourceMeta.Namespace,
		name:                 resourceMeta.Name,
		workName:             work.Name,
	}
	informer, found := o.informers[key]
	if !found {
		return nil
	}

	registration, found := informer.registrations[regKey]
	if !found {
		return nil
	}

	if err := informer.informer.RemoveEventHandler(registration); err != nil {
		return err
	}
	delete(informer.registrations, regKey)

	// stop the informer if no one use it.
	if len(informer.registrations) == 0 {
		informer.cancel()
		delete(o.informers, key)
	}

	return nil
}

func (o *ObjectReader) queueWorkByResourceFunc(ctx context.Context, gvr schema.GroupVersionResource) func(object interface{}) {
	return func(object interface{}) {
		accessor, err := meta.Accessor(object)
		if err != nil {
			utilruntime.HandleErrorWithContext(ctx, err, "failed to access object")
		}

		key := gvr.Group + "/" + gvr.Resource + "/" + gvr.Version + "/" + accessor.GetNamespace() + "/" + accessor.GetName()
		objects, err := o.indexer.ByIndex(byWorkIndex, key)
		if err != nil {
			utilruntime.HandleErrorWithContext(ctx, err, "failed to get object by index")
		}

		for _, obj := range objects {
			work := obj.(*workapiv1.ManifestWork)
			o.manifestWorkQueue.Add(work.Name)
		}
	}
}

func indexWorkByResource(obj interface{}) ([]string, error) {
	work, ok := obj.(*workapiv1.ManifestWork)
	if !ok {
		return []string{}, nil
	}

	var keys []string
	for _, m := range work.Status.ResourceStatus.Manifests {
		key := m.ResourceMeta.Group + "/" + m.ResourceMeta.Resource + "/" + m.ResourceMeta.Version + "/" + m.ResourceMeta.Namespace + "/" + m.ResourceMeta.Name
		keys = append(keys, key)
	}
	return keys, nil
}
