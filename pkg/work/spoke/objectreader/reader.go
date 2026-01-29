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
	"k8s.io/klog/v2"

	workapiv1 "open-cluster-management.io/api/work/v1"
)

const byWorkIndex = "byWorkIndex"

type ObjectReader interface {
	// Get returns an object based on resourceMeta
	Get(ctx context.Context, resourceMeta workapiv1.ManifestResourceMeta) (*unstructured.Unstructured, metav1.Condition, error)

	// RegisterInformer registers an informer to the ObjectReader
	RegisterInformer(
		ctx context.Context, workName string,
		resourceMeta workapiv1.ManifestResourceMeta,
		queue workqueue.TypedRateLimitingInterface[string]) error

	// UnRegisterInformer unregister the informer from the ObjectReader
	UnRegisterInformer(workName string, resourceMeta workapiv1.ManifestResourceMeta) error
}

// ObjectReader reads spoke resources using informer-based caching or direct dynamic client calls.
type objectReader struct {
	sync.RWMutex

	dynamicClient dynamic.Interface

	informers map[informerKey]*informerWithCancel

	indexer cache.Indexer

	maxWatch int32
}

type informerWithCancel struct {
	informer cache.SharedIndexInformer
	lister   cache.GenericLister
	cancel   context.CancelFunc

	// registrations records all the event handler registrations, keyed by registrationKey
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

func (o *objectReader) Get(ctx context.Context, resourceMeta workapiv1.ManifestResourceMeta) (*unstructured.Unstructured, metav1.Condition, error) {
	if len(resourceMeta.Resource) == 0 || len(resourceMeta.Version) == 0 || len(resourceMeta.Name) == 0 {
		return nil, metav1.Condition{
			Type:    workapiv1.ManifestAvailable,
			Status:  metav1.ConditionUnknown,
			Reason:  "IncompleteResourceMeta",
			Message: "Resource meta is incomplete",
		}, fmt.Errorf("incomplete resource meta")
	}

	obj, err := o.getObject(ctx, resourceMeta)
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

func (o *objectReader) getObject(ctx context.Context, resourceMeta workapiv1.ManifestResourceMeta) (*unstructured.Unstructured, error) {
	gvr := schema.GroupVersionResource{
		Group:    resourceMeta.Group,
		Version:  resourceMeta.Version,
		Resource: resourceMeta.Resource,
	}
	key := informerKey{GroupVersionResource: gvr, namespace: resourceMeta.Namespace}

	o.RLock()
	i, found := o.informers[key]
	o.RUnlock()

	// Use informer cache only if it exists and has synced.
	// If informer is not synced (e.g., watch permission denied, initial sync in progress),
	// fallback to direct client.Get() which only requires GET permission.
	if found && i.informer.HasSynced() {
		var runObj runtime.Object
		var err error
		// For cluster-scoped resources (empty namespace), use Get() directly
		// ByNamespace("") doesn't work for cluster-scoped resources
		if resourceMeta.Namespace == "" {
			runObj, err = i.lister.Get(resourceMeta.Name)
		} else {
			runObj, err = i.lister.ByNamespace(resourceMeta.Namespace).Get(resourceMeta.Name)
		}
		if err != nil {
			return nil, err
		}
		obj, ok := runObj.(*unstructured.Unstructured)
		if !ok {
			return nil, fmt.Errorf("unexpected type from lister: %T", runObj)
		}
		return obj, nil
	}

	return o.dynamicClient.Resource(gvr).Namespace(resourceMeta.Namespace).Get(ctx, resourceMeta.Name, metav1.GetOptions{})
}

// RegisterInformer checks if there is an informer and if the event handler has been registered to the informer.
// this is called each time a resource needs to be watched. It is idempotent.
func (o *objectReader) RegisterInformer(
	ctx context.Context, workName string,
	resourceMeta workapiv1.ManifestResourceMeta,
	queue workqueue.TypedRateLimitingInterface[string]) error {
	logger := klog.FromContext(ctx)
	o.Lock()
	defer o.Unlock()

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
		workName:             workName,
	}
	informer, found := o.informers[key]
	if !found {
		if len(o.informers) >= int(o.maxWatch) {
			logger.Info("The number of registered informers has reached the maximum limit, fallback to feedback with poll")
			return nil
		}

		resourceInformer := dynamicinformer.NewFilteredDynamicInformer(
			o.dynamicClient, gvr, resourceMeta.Namespace, 24*time.Hour,
			cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}, nil)
		informerCtx, cancel := context.WithCancel(ctx)
		informer = &informerWithCancel{
			informer:      resourceInformer.Informer(),
			cancel:        cancel,
			lister:        resourceInformer.Lister(),
			registrations: map[registrationKey]cache.ResourceEventHandlerRegistration{},
		}
		o.informers[key] = informer
		logger.V(4).Info("Registered informer for objecr reader", "informerKey", key)
		go resourceInformer.Informer().Run(informerCtx.Done())
	}

	// check if the event handler has been registered.
	if _, registrationFound := informer.registrations[regKey]; registrationFound {
		return nil
	}

	logger.V(4).Info("Add event handler of informer for objecr reader", "informerKey", key, "resourceKey", regKey)
	// Add event handler into the informer so it can trigger work reconcile
	registration, err := informer.informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: o.queueWorkByResourceFunc(ctx, gvr, queue),
		UpdateFunc: func(old, new interface{}) {
			o.queueWorkByResourceFunc(ctx, gvr, queue)(new)
		},
	})
	if err != nil {
		return err
	}
	// record the event handler registration
	informer.registrations[regKey] = registration

	return nil
}

// UnRegisterInformer is called each time a resource is not watched.
func (o *objectReader) UnRegisterInformer(workName string, resourceMeta workapiv1.ManifestResourceMeta) error {
	o.Lock()
	defer o.Unlock()

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
		workName:             workName,
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

func (o *objectReader) queueWorkByResourceFunc(ctx context.Context, gvr schema.GroupVersionResource, queue workqueue.TypedRateLimitingInterface[string]) func(object interface{}) {
	return func(object interface{}) {
		logger := klog.FromContext(ctx)
		accessor, err := meta.Accessor(object)
		if err != nil {
			utilruntime.HandleErrorWithContext(ctx, err, "failed to access object")
			return
		}

		key := gvr.Group + "/" + gvr.Resource + "/" + gvr.Version + "/" + accessor.GetNamespace() + "/" + accessor.GetName()
		objects, err := o.indexer.ByIndex(byWorkIndex, key)
		if err != nil {
			utilruntime.HandleErrorWithContext(ctx, err, "failed to get object by index")
			return
		}

		for _, obj := range objects {
			work := obj.(*workapiv1.ManifestWork)
			logger.V(4).Info("enqueue work by resource", "resourceKey", key)
			queue.Add(work.Name)
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

func UnRegisterInformerFromAppliedManifestWork(ctx context.Context, o ObjectReader, workName string, appliedResources []workapiv1.AppliedManifestResourceMeta) {
	if o == nil {
		return
	}
	for _, r := range appliedResources {
		resourceMeta := workapiv1.ManifestResourceMeta{
			Group:     r.Group,
			Version:   r.Version,
			Resource:  r.Resource,
			Name:      r.Name,
			Namespace: r.Namespace,
		}
		if err := o.UnRegisterInformer(workName, resourceMeta); err != nil {
			utilruntime.HandleErrorWithContext(ctx, err, "failed to unregister informer")
		}
	}
}
