package addonowner

import (
	"context"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformerv1alpha1 "open-cluster-management.io/api/client/addon/informers/externalversions/addon/v1alpha1"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"

	"open-cluster-management.io/addon-framework/pkg/basecontroller/factory"
	"open-cluster-management.io/addon-framework/pkg/utils"
)

const UnsupportedConfigurationType = "UnsupportedConfiguration"

// addonOwnerController reconciles instances of managedclusteradd on the hub
// to add related ClusterManagementAddon as the owner.
type addonOwnerController struct {
	addonClient                  addonv1alpha1client.Interface
	managedClusterAddonLister    addonlisterv1alpha1.ManagedClusterAddOnLister
	clusterManagementAddonLister addonlisterv1alpha1.ClusterManagementAddOnLister
	addonFilterFunc              factory.EventFilterFunc
}

func NewAddonOwnerController(
	addonClient addonv1alpha1client.Interface,
	addonInformers addoninformerv1alpha1.ManagedClusterAddOnInformer,
	clusterManagementAddonInformers addoninformerv1alpha1.ClusterManagementAddOnInformer,
	addonFilterFunc factory.EventFilterFunc,
) factory.Controller {
	c := &addonOwnerController{
		addonClient:                  addonClient,
		managedClusterAddonLister:    addonInformers.Lister(),
		clusterManagementAddonLister: clusterManagementAddonInformers.Lister(),
		addonFilterFunc:              addonFilterFunc,
	}

	return factory.New().WithFilteredEventsInformersQueueKeysFunc(
		func(obj runtime.Object) []string {
			key, _ := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
			return []string{key}
		},
		c.addonFilterFunc, clusterManagementAddonInformers.Informer()).
		WithInformersQueueKeysFunc(
			func(obj runtime.Object) []string {
				key, _ := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
				return []string{key}
			},
			addonInformers.Informer()).WithSync(c.sync).ToController("addon-owner-controller")
}

func (c *addonOwnerController) sync(ctx context.Context, syncCtx factory.SyncContext, key string) error {
	klog.V(4).Infof("Reconciling addon %q", key)

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

	owner := metav1.NewControllerRef(clusterManagementAddon, addonapiv1alpha1.GroupVersion.WithKind("ClusterManagementAddOn"))
	modified = utils.MergeOwnerRefs(&addonCopy.OwnerReferences, *owner, false)
	if modified {
		_, err = c.addonClient.AddonV1alpha1().ManagedClusterAddOns(namespace).Update(ctx, addonCopy, metav1.UpdateOptions{})
		return err
	}

	return nil
}
