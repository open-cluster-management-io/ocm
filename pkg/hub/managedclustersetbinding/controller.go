package managedclustersetbinding

import (
	"context"
	"encoding/json"
	"fmt"

	jsonpatch "github.com/evanphx/json-patch"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	clientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterinformerv1beta2 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1beta2"
	clusterlisterv1beta2 "open-cluster-management.io/api/client/cluster/listers/cluster/v1beta2"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
)

const (
	byClusterSet = "by-clusterset"
)

// managedClusterSetController reconciles instances of ManagedClusterSet on the hub.
type managedClusterSetBindingController struct {
	clusterClient             clientset.Interface
	clusterSetBindingLister   clusterlisterv1beta2.ManagedClusterSetBindingLister
	clusterSetLister          clusterlisterv1beta2.ManagedClusterSetLister
	clusterSetBindingIndexers cache.Indexer
	queue                     workqueue.RateLimitingInterface
	eventRecorder             events.Recorder
}

func NewManagedClusterSetBindingController(
	clusterClient clientset.Interface,
	clusterSetInformer clusterinformerv1beta2.ManagedClusterSetInformer,
	clusterSetBindingInformer clusterinformerv1beta2.ManagedClusterSetBindingInformer,
	recorder events.Recorder) factory.Controller {

	controllerName := "managed-clusterset-binding-controller"
	syncCtx := factory.NewSyncContext(controllerName, recorder)

	err := clusterSetBindingInformer.Informer().AddIndexers(cache.Indexers{
		byClusterSet: indexByClusterset,
	})

	if err != nil {
		utilruntime.HandleError(err)
	}

	c := &managedClusterSetBindingController{
		clusterClient:             clusterClient,
		clusterSetLister:          clusterSetInformer.Lister(),
		clusterSetBindingLister:   clusterSetBindingInformer.Lister(),
		eventRecorder:             recorder.WithComponentSuffix(controllerName),
		clusterSetBindingIndexers: clusterSetBindingInformer.Informer().GetIndexer(),
		queue:                     syncCtx.Queue(),
	}

	clusterSetInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: c.enqueueBindingsByClusterSet,
			UpdateFunc: func(oldObj, newObj interface{}) {
				c.enqueueBindingsByClusterSet(newObj)
			},
			DeleteFunc: c.enqueueBindingsByClusterSet,
		},
	)

	return factory.New().
		WithSyncContext(syncCtx).
		WithInformersQueueKeyFunc(func(obj runtime.Object) string {
			key, _ := cache.MetaNamespaceKeyFunc(obj)
			return key
		}, clusterSetBindingInformer.Informer()).
		WithBareInformers(clusterSetInformer.Informer()).
		WithSync(c.sync).
		ToController("ManagedClusterSetController", recorder)
}

func indexByClusterset(obj interface{}) ([]string, error) {
	binding, ok := obj.(*clusterv1beta2.ManagedClusterSetBinding)
	if !ok {
		return []string{}, fmt.Errorf("obj is supposed to be a ManagedClusterSetBinding, but is %T", obj)
	}

	return []string{binding.Spec.ClusterSet}, nil
}

func (c *managedClusterSetBindingController) getClusterBindingsByClusterSet(name string) ([]*clusterv1beta2.ManagedClusterSetBinding, error) {
	objs, err := c.clusterSetBindingIndexers.ByIndex(byClusterSet, name)
	if err != nil {
		return nil, err
	}

	bindings := make([]*clusterv1beta2.ManagedClusterSetBinding, len(objs))
	for _, obj := range objs {
		binding := obj.(*clusterv1beta2.ManagedClusterSetBinding)
		bindings = append(bindings, binding)
	}

	return bindings, nil
}

func (c *managedClusterSetBindingController) enqueueBindingsByClusterSet(obj interface{}) {
	name, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("error to get accessor of object: %v", obj))
		return
	}

	bindings, err := c.getClusterBindingsByClusterSet(name)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("error to get bindings of object: %v", obj))
		return
	}

	for _, binding := range bindings {
		// TODO(qiujian16) it is weird that index can return nil. Needs more investigation.
		if binding == nil {
			continue
		}
		key, _ := cache.MetaNamespaceKeyFunc(binding)
		c.queue.Add(key)
	}
}

func (c *managedClusterSetBindingController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	key := syncCtx.QueueKey()
	if len(key) == 0 {
		return nil
	}

	klog.V(4).Infof("Reconciling ManagedClusterSetBinding %s", key)

	bindingNamespace, bindingName, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}

	if len(bindingNamespace) == 0 {
		return nil
	}

	binding, err := c.clusterSetBindingLister.ManagedClusterSetBindings(bindingNamespace).Get(bindingName)
	switch {
	case errors.IsNotFound(err):
		return nil
	case err != nil:
		return err
	}

	_, err = c.clusterSetLister.Get(binding.Spec.ClusterSet)

	bindingCopy := binding.DeepCopy()
	switch {
	case errors.IsNotFound(err):
		meta.SetStatusCondition(&bindingCopy.Status.Conditions, metav1.Condition{
			Type:   clusterv1beta2.ClusterSetBindingBoundType,
			Status: metav1.ConditionFalse,
			Reason: "ClusterSetNotFound",
		})
		return c.patchCondition(ctx, binding, bindingCopy)
	case err != nil:
		return err
	}

	meta.SetStatusCondition(&bindingCopy.Status.Conditions, metav1.Condition{
		Type:   clusterv1beta2.ClusterSetBindingBoundType,
		Status: metav1.ConditionTrue,
		Reason: "ClusterSetBound",
	})

	return c.patchCondition(ctx, binding, bindingCopy)
}

func (c *managedClusterSetBindingController) patchCondition(ctx context.Context, old, new *clusterv1beta2.ManagedClusterSetBinding) error {
	if equality.Semantic.DeepEqual(old.Status.Conditions, new.Status.Conditions) {
		return nil
	}

	oldData, err := json.Marshal(clusterv1beta2.ManagedClusterSetBinding{
		Status: clusterv1beta2.ManagedClusterSetBindingStatus{
			Conditions: old.Status.Conditions,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to Marshal old data for workspace %s: %w", old.Name, err)
	}

	newData, err := json.Marshal(clusterv1beta2.ManagedClusterSetBinding{
		ObjectMeta: metav1.ObjectMeta{
			UID:             old.UID,
			ResourceVersion: old.ResourceVersion,
		}, // to ensure they appear in the patch as preconditions
		Status: clusterv1beta2.ManagedClusterSetBindingStatus{
			Conditions: new.Status.Conditions,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to Marshal new data for workspace %s: %w", new.Name, err)
	}

	patchBytes, err := jsonpatch.CreateMergePatch(oldData, newData)
	if err != nil {
		return fmt.Errorf("failed to create patch for workspace %s: %w", new.Name, err)
	}

	c.eventRecorder.Eventf("PatchClusterSetBindingCondition", "patch clustersetbinding %s/%s condition", new.Namespace, new.Name)

	_, err = c.clusterClient.ClusterV1beta2().ManagedClusterSetBindings(new.Namespace).Patch(ctx, new.Name, types.MergePatchType, patchBytes, metav1.PatchOptions{}, "status")
	return err
}
