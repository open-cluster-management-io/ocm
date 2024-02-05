package addonconfig

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/dynamic/dynamiclister"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"open-cluster-management.io/addon-framework/pkg/basecontroller/factory"
	"open-cluster-management.io/addon-framework/pkg/index"
	"open-cluster-management.io/addon-framework/pkg/utils"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformerv1alpha1 "open-cluster-management.io/api/client/addon/informers/externalversions/addon/v1alpha1"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"
	"open-cluster-management.io/sdk-go/pkg/patcher"
)

const (
	controllerName = "addon-config-controller"
)

type enqueueFunc func(obj interface{})

// addonConfigController reconciles all interested addon config types (GroupVersionResource) on the hub.
type addonConfigController struct {
	addonClient                  addonv1alpha1client.Interface
	addonLister                  addonlisterv1alpha1.ManagedClusterAddOnLister
	addonIndexer                 cache.Indexer
	configListers                map[schema.GroupResource]dynamiclister.Lister
	queue                        workqueue.RateLimitingInterface
	addonFilterFunc              factory.EventFilterFunc
	configGVRs                   map[schema.GroupVersionResource]bool
	clusterManagementAddonLister addonlisterv1alpha1.ClusterManagementAddOnLister
}

func NewAddonConfigController(
	addonClient addonv1alpha1client.Interface,
	addonInformers addoninformerv1alpha1.ManagedClusterAddOnInformer,
	clusterManagementAddonInformers addoninformerv1alpha1.ClusterManagementAddOnInformer,
	configInformerFactory dynamicinformer.DynamicSharedInformerFactory,
	configGVRs map[schema.GroupVersionResource]bool,
	addonFilterFunc factory.EventFilterFunc,
) factory.Controller {
	syncCtx := factory.NewSyncContext(controllerName)

	c := &addonConfigController{
		addonClient:                  addonClient,
		addonLister:                  addonInformers.Lister(),
		addonIndexer:                 addonInformers.Informer().GetIndexer(),
		configListers:                map[schema.GroupResource]dynamiclister.Lister{},
		queue:                        syncCtx.Queue(),
		addonFilterFunc:              addonFilterFunc,
		configGVRs:                   configGVRs,
		clusterManagementAddonLister: clusterManagementAddonInformers.Lister(),
	}

	configInformers := c.buildConfigInformers(configInformerFactory, configGVRs)

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
	for gvrRaw := range configGVRs {
		gvr := gvrRaw // copy the value since it will be used in the closure
		genericInformer := configInformerFactory.ForResource(gvr)
		indexInformer := genericInformer.Informer()
		_, err := indexInformer.AddEventHandler(
			cache.ResourceEventHandlerFuncs{
				AddFunc: c.enqueueAddOnsByConfig(gvr),
				UpdateFunc: func(oldObj, newObj interface{}) {
					c.enqueueAddOnsByConfig(gvr)(newObj)
				},
				DeleteFunc: c.enqueueAddOnsByConfig(gvr),
			},
		)
		if err != nil {
			utilruntime.HandleError(err)
		}
		configInformers = append(configInformers, indexInformer)
		c.configListers[schema.GroupResource{Group: gvr.Group, Resource: gvr.Resource}] = dynamiclister.New(indexInformer.GetIndexer(), gvr)
	}
	return configInformers
}

func (c *addonConfigController) enqueueAddOnsByConfig(gvr schema.GroupVersionResource) enqueueFunc {
	return func(obj interface{}) {
		namespaceName, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
		if err != nil {
			utilruntime.HandleError(fmt.Errorf("error to get accessor of object: %v", obj))
			return
		}

		objs, err := c.addonIndexer.ByIndex(index.AddonByConfig,
			fmt.Sprintf("%s/%s/%s", gvr.Group, gvr.Resource, namespaceName))
		if err != nil {
			utilruntime.HandleError(fmt.Errorf("error to get addons: %v", err))
			return
		}

		for _, obj := range objs {
			if obj == nil {
				continue
			}
			key, _ := cache.MetaNamespaceKeyFunc(obj)
			c.queue.Add(key)
		}
	}
}

func (c *addonConfigController) sync(ctx context.Context, syncCtx factory.SyncContext, key string) error {
	addonNamespace, addonName, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		// ignore addon whose key is invalid
		return nil
	}

	addon, err := c.addonLister.ManagedClusterAddOns(addonNamespace).Get(addonName)
	if errors.IsNotFound(err) {
		// addon could be deleted, ignore
		return nil
	}
	if err != nil {
		return err
	}

	cma, err := c.clusterManagementAddonLister.Get(addonName)
	if errors.IsNotFound(err) {
		// cluster management addon could be deleted, ignore
		return nil
	}
	if err != nil {
		return err
	}

	if !c.addonFilterFunc(cma) {
		return nil
	}

	addonCopy := addon.DeepCopy()
	if err := c.updateConfigSpecHashAndGenerations(addonCopy); err != nil {
		return err
	}

	addonPatcher := patcher.NewPatcher[
		*addonapiv1alpha1.ManagedClusterAddOn,
		addonapiv1alpha1.ManagedClusterAddOnSpec,
		addonapiv1alpha1.ManagedClusterAddOnStatus](c.addonClient.AddonV1alpha1().ManagedClusterAddOns(addonNamespace))

	_, err = addonPatcher.PatchStatus(ctx, addonCopy, addonCopy.Status, addon.Status)
	return err
}

func (c *addonConfigController) updateConfigSpecHashAndGenerations(addon *addonapiv1alpha1.ManagedClusterAddOn) error {
	supportedConfigSet := map[addonapiv1alpha1.ConfigGroupResource]bool{}
	for _, config := range addon.Status.SupportedConfigs {
		supportedConfigSet[config] = true
	}
	for index, configReference := range addon.Status.ConfigReferences {

		if !utils.ContainGR(
			c.configGVRs,
			configReference.ConfigGroupResource.Group,
			configReference.ConfigGroupResource.Resource) {
			continue
		}

		lister, ok := c.configListers[schema.GroupResource{Group: configReference.ConfigGroupResource.Group, Resource: configReference.ConfigGroupResource.Resource}]
		if !ok {
			continue
		}

		var config *unstructured.Unstructured
		var err error
		if configReference.Namespace == "" {
			config, err = lister.Get(configReference.Name)
		} else {
			config, err = lister.Namespace(configReference.Namespace).Get(configReference.Name)
		}

		if errors.IsNotFound(err) {
			continue
		}

		if err != nil {
			return err
		}

		// update LastObservedGeneration for all the configs in status
		addon.Status.ConfigReferences[index].LastObservedGeneration = config.GetGeneration()

		// update desired spec hash only for the configs in spec
		for _, addonconfig := range addon.Spec.Configs {
			// do not update spec hash for unsupported configs
			if _, ok := supportedConfigSet[addonconfig.ConfigGroupResource]; !ok {
				continue
			}
			if configReference.DesiredConfig == nil {
				continue
			}

			if configReference.ConfigGroupResource == addonconfig.ConfigGroupResource &&
				configReference.DesiredConfig.ConfigReferent == addonconfig.ConfigReferent {
				specHash, err := utils.GetSpecHash(config)
				if err != nil {
					return err
				}
				addon.Status.ConfigReferences[index].DesiredConfig.SpecHash = specHash
			}
		}
	}

	return nil
}
