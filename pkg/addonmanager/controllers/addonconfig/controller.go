package addonconfig

import (
	"context"
	"encoding/json"
	"fmt"

	jsonpatch "github.com/evanphx/json-patch"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/dynamic/dynamiclister"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"open-cluster-management.io/addon-framework/pkg/basecontroller/factory"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformerv1alpha1 "open-cluster-management.io/api/client/addon/informers/externalversions/addon/v1alpha1"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"
)

const (
	controllerName = "addon-config-controller"
	byAddOnConfig  = "by-addon-config"
)

type enqueueFunc func(obj interface{})

// addonConfigController reconciles all interested addon config types (GroupVersionResource) on the hub.
type addonConfigController struct {
	addonClient   addonv1alpha1client.Interface
	addonLister   addonlisterv1alpha1.ManagedClusterAddOnLister
	addonIndexer  cache.Indexer
	configListers map[string]dynamiclister.Lister
	queue         workqueue.RateLimitingInterface
}

func NewAddonConfigController(
	addonClient addonv1alpha1client.Interface,
	addonInformers addoninformerv1alpha1.ManagedClusterAddOnInformer,
	configInformerFactory dynamicinformer.DynamicSharedInformerFactory,
	configGVRs map[schema.GroupVersionResource]bool,
) factory.Controller {
	syncCtx := factory.NewSyncContext(controllerName)

	c := &addonConfigController{
		addonClient:   addonClient,
		addonLister:   addonInformers.Lister(),
		addonIndexer:  addonInformers.Informer().GetIndexer(),
		configListers: map[string]dynamiclister.Lister{},
		queue:         syncCtx.Queue(),
	}

	configInformers := c.buildConfigInformers(configInformerFactory, configGVRs)

	if err := addonInformers.Informer().AddIndexers(cache.Indexers{byAddOnConfig: c.indexByConfig}); err != nil {
		utilruntime.HandleError(err)
	}

	return factory.New().
		WithSyncContext(syncCtx).
		WithInformersQueueKeysFunc(func(obj runtime.Object) []string {
			key, _ := cache.MetaNamespaceKeyFunc(obj)
			return []string{key}
		}, addonInformers.Informer()).
		WithBareInformers(configInformers...).
		WithSync(c.sync).ToController(controllerName)
}

func (c *addonConfigController) buildConfigInformers(
	configInformerFactory dynamicinformer.DynamicSharedInformerFactory,
	configGVRs map[schema.GroupVersionResource]bool,
) []factory.Informer {
	configInformers := []factory.Informer{}
	for gvr := range configGVRs {
		genericInformer := configInformerFactory.ForResource(gvr)
		indexInformer := genericInformer.Informer()
		indexInformer.AddEventHandler(
			cache.ResourceEventHandlerFuncs{
				AddFunc: c.enqueueAddOnsByConfig(gvr),
				UpdateFunc: func(oldObj, newObj interface{}) {
					c.enqueueAddOnsByConfig(gvr)(newObj)
				},
				DeleteFunc: c.enqueueAddOnsByConfig(gvr),
			},
		)
		configInformers = append(configInformers, indexInformer)
		c.configListers[toListerKey(gvr.Group, gvr.Resource)] = dynamiclister.New(indexInformer.GetIndexer(), gvr)
	}
	return configInformers
}

func (c *addonConfigController) enqueueAddOnsByConfig(gvr schema.GroupVersionResource) enqueueFunc {
	return func(obj interface{}) {
		name, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
		if err != nil {
			utilruntime.HandleError(fmt.Errorf("error to get accessor of object: %v", obj))
			return
		}

		objs, err := c.addonIndexer.ByIndex(byAddOnConfig, fmt.Sprintf("%s/%s/%s", gvr.Group, gvr.Resource, name))
		if err != nil {
			utilruntime.HandleError(fmt.Errorf("error to get addons: %v", err))
			return
		}

		for _, obj := range objs {
			if obj == nil {
				continue
			}

			addon := obj.(*addonapiv1alpha1.ManagedClusterAddOn)
			key, _ := cache.MetaNamespaceKeyFunc(addon)
			c.queue.Add(key)
		}
	}
}

func (c *addonConfigController) indexByConfig(obj interface{}) ([]string, error) {
	addon, ok := obj.(*addonapiv1alpha1.ManagedClusterAddOn)
	if !ok {
		return nil, fmt.Errorf("obj is supposed to be a ManagedClusterAddOn, but is %T", obj)
	}

	if len(addon.Status.ConfigReferences) == 0 {
		// no config references, ignore
		return nil, nil
	}

	configNames := []string{}
	for _, configReference := range addon.Status.ConfigReferences {
		if configReference.Name == "" {
			// bad config reference, ignore
			continue
		}

		configNames = append(configNames, getIndex(configReference))
	}

	return configNames, nil
}

func (c *addonConfigController) sync(ctx context.Context, syncCtx factory.SyncContext, key string) error {
	addonNamespace, addonName, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		// ignore addon whose key is invalid
		return nil
	}

	addon, err := c.addonLister.ManagedClusterAddOns(addonNamespace).Get(addonName)
	if errors.IsNotFound(err) {
		// addon cloud be deleted, ignore
		return nil
	}
	if err != nil {
		return err
	}

	addonCopy := addon.DeepCopy()

	if err := c.updateConfigGenerations(addonCopy); err != nil {
		return err
	}

	return c.patchConfigReferences(ctx, addon, addonCopy)
}

func (c *addonConfigController) updateConfigGenerations(addon *addonapiv1alpha1.ManagedClusterAddOn) error {
	if len(addon.Status.ConfigReferences) == 0 {
		// no config references, ignore
		return nil
	}

	for index, configReference := range addon.Status.ConfigReferences {
		lister, ok := c.configListers[toListerKey(configReference.Group, configReference.Resource)]
		if !ok {
			continue
		}

		namespace := configReference.ConfigReferent.Namespace
		name := configReference.ConfigReferent.Name

		var config *unstructured.Unstructured
		var err error
		if namespace == "" {
			config, err = lister.Get(name)
		} else {
			config, err = lister.Namespace(namespace).Get(name)
		}

		if errors.IsNotFound(err) {
			continue
		}

		if err != nil {
			return err
		}

		// TODO if config is configmap or secret, the generation will not be increased automatically,
		// we may need to consider how to handle this in the future
		addon.Status.ConfigReferences[index].LastObservedGeneration = config.GetGeneration()
	}

	return nil
}

func (c *addonConfigController) patchConfigReferences(ctx context.Context, old, new *addonapiv1alpha1.ManagedClusterAddOn) error {
	if equality.Semantic.DeepEqual(new.Status.ConfigReferences, old.Status.ConfigReferences) {
		return nil
	}

	oldData, err := json.Marshal(&addonapiv1alpha1.ManagedClusterAddOn{
		Status: addonapiv1alpha1.ManagedClusterAddOnStatus{
			ConfigReferences: old.Status.ConfigReferences,
		},
	})
	if err != nil {
		return err
	}

	newData, err := json.Marshal(&addonapiv1alpha1.ManagedClusterAddOn{
		ObjectMeta: metav1.ObjectMeta{
			UID:             new.UID,
			ResourceVersion: new.ResourceVersion,
		},
		Status: addonapiv1alpha1.ManagedClusterAddOnStatus{
			ConfigReferences: new.Status.ConfigReferences,
		},
	})
	if err != nil {
		return err
	}

	patchBytes, err := jsonpatch.CreateMergePatch(oldData, newData)
	if err != nil {
		return fmt.Errorf("failed to create patch for addon %s: %w", new.Name, err)
	}

	klog.V(4).Infof("Patching addon %s/%s config reference with %s", new.Namespace, new.Name, string(patchBytes))
	_, err = c.addonClient.AddonV1alpha1().ManagedClusterAddOns(new.Namespace).Patch(
		ctx,
		new.Name,
		types.MergePatchType,
		patchBytes,
		metav1.PatchOptions{},
		"status",
	)
	return err
}

func getIndex(config addonapiv1alpha1.ConfigReference) string {
	if config.Namespace != "" {
		return fmt.Sprintf("%s/%s/%s/%s", config.Group, config.Resource, config.Namespace, config.Name)
	}

	return fmt.Sprintf("%s/%s/%s", config.Group, config.Resource, config.Name)
}

func toListerKey(group, resource string) string {
	return fmt.Sprintf("%s/%s", group, resource)
}
