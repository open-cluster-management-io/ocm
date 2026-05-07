package addonowner

import (
	"context"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"open-cluster-management.io/addon-framework/pkg/utils"
	addonv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	addonclient "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformerv1beta1 "open-cluster-management.io/api/client/addon/informers/externalversions/addon/v1beta1"
	addonlisterv1beta1 "open-cluster-management.io/api/client/addon/listers/addon/v1beta1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"

	addonindex "open-cluster-management.io/ocm/pkg/addon/index"
	"open-cluster-management.io/ocm/pkg/common/queue"
)

const UnsupportedConfigurationType = "UnsupportedConfiguration"

// addonOwnerController reconciles instances of managedclusteradd on the hub
// to add related ClusterManagementAddon as the owner.
type addonOwnerController struct {
	addonClient                  addonclient.Interface
	managedClusterAddonLister    addonlisterv1beta1.ManagedClusterAddOnLister
	managedClusterAddonIndexer   cache.Indexer
	clusterManagementAddonLister addonlisterv1beta1.ClusterManagementAddOnLister
	addonFilterFunc              factory.EventFilterFunc
}

func NewAddonOwnerController(
	addonClient addonclient.Interface,
	addonInformers addoninformerv1beta1.ManagedClusterAddOnInformer,
	clusterManagementAddonInformers addoninformerv1beta1.ClusterManagementAddOnInformer,
	addonFilterFunc factory.EventFilterFunc,
) factory.Controller {
	c := &addonOwnerController{
		addonClient:                  addonClient,
		managedClusterAddonLister:    addonInformers.Lister(),
		managedClusterAddonIndexer:   addonInformers.Informer().GetIndexer(),
		clusterManagementAddonLister: clusterManagementAddonInformers.Lister(),
		addonFilterFunc:              addonFilterFunc,
	}

	return factory.New().
		WithFilteredEventsInformersQueueKeysFunc(
			queue.QueueKeyByMetaNamespaceName,
			c.addonFilterFunc).
		WithInformersQueueKeysFunc(
			addonindex.ManagedClusterAddonByNameQueueKey(addonInformers),
			clusterManagementAddonInformers.Informer(),
		).
		WithInformersQueueKeysFunc(
			queue.QueueKeyByMetaNamespaceName,
			addonInformers.Informer()).
		WithSync(c.sync).
		ToController("addon-owner-controller")
}

func (c *addonOwnerController) sync(ctx context.Context, syncCtx factory.SyncContext, key string) error {
	logger := klog.FromContext(ctx).WithValues("addon", key)
	logger.V(4).Info("Reconciling addon")

	namespace, addonName, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		// ignore addon whose key is invalid
		return nil
	}

	addon, err := c.managedClusterAddonLister.ManagedClusterAddOns(namespace).Get(addonName)
	switch {
	case errors.IsNotFound(err):
		return nil
	case err != nil:
		return err
	}

	addonCopy := addon.DeepCopy()
	modified := false

	clusterManagementAddon, err := c.clusterManagementAddonLister.Get(addonName)
	if errors.IsNotFound(err) {
		return nil
	}

	if err != nil {
		return err
	}

	if !c.addonFilterFunc(clusterManagementAddon) {
		return nil
	}

	owner := metav1.NewControllerRef(clusterManagementAddon, schema.GroupVersionKind{
		Group:   addonv1beta1.GroupName,
		Version: addonv1beta1.GroupVersion.Version,
		Kind:    "ClusterManagementAddOn",
	})
	modified = utils.MergeOwnerRefs(&addonCopy.OwnerReferences, *owner, false)
	if modified {
		_, err = c.addonClient.AddonV1beta1().ManagedClusterAddOns(namespace).Update(ctx, addonCopy, metav1.UpdateOptions{})
		return err
	}

	return nil
}
