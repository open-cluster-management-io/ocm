package addonstatus

import (
	"context"
	"encoding/json"
	"fmt"

	jsonpatch "github.com/evanphx/json-patch"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
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

// addonStatusController reconciles instances of managedclusteradd on the hub
// based to update related object and status condition.
type addonStatusController struct {
	addonClient                  addonv1alpha1client.Interface
	managedClusterAddonLister    addonlisterv1alpha1.ManagedClusterAddOnLister
	clusterManagementAddonLister addonlisterv1alpha1.ClusterManagementAddOnLister
}

func NewAddonStatusController(
	addonClient addonv1alpha1client.Interface,
	addonInformers addoninformerv1alpha1.ManagedClusterAddOnInformer,
	clusterManagementAddonInformers addoninformerv1alpha1.ClusterManagementAddOnInformer,
) factory.Controller {
	c := &addonStatusController{
		addonClient:                  addonClient,
		managedClusterAddonLister:    addonInformers.Lister(),
		clusterManagementAddonLister: clusterManagementAddonInformers.Lister(),
	}

	return factory.New().WithInformersQueueKeysFunc(
		func(obj runtime.Object) []string {
			key, _ := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
			return []string{key}
		},
		addonInformers.Informer()).
		WithSync(c.sync).ToController("addon-status-controller")
}

func (c *addonStatusController) sync(ctx context.Context, syncCtx factory.SyncContext, key string) error {
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
	if err == nil {
		owner := metav1.NewControllerRef(clusterManagementAddon, addonapiv1alpha1.GroupVersion.WithKind("ClusterManagementAddOn"))
		modified = utils.MergeOwnerRefs(&addonCopy.OwnerReferences, *owner, false)
		if modified {
			_, err = c.addonClient.AddonV1alpha1().ManagedClusterAddOns(namespace).Update(ctx, addonCopy, metav1.UpdateOptions{})
			return err
		}

		// Add related ClusterManagementAddon
		utils.MergeRelatedObjects(&modified, &addonCopy.Status.RelatedObjects, addonapiv1alpha1.ObjectReference{
			Name:     clusterManagementAddon.Name,
			Resource: "clustermanagementaddons",
			Group:    addonapiv1alpha1.GroupVersion.Group,
		})
	} else if !errors.IsNotFound(err) {
		return err
	}

	// update namespace here also
	if len(addonCopy.Spec.InstallNamespace) != 0 && addonCopy.Status.Namespace != addonCopy.Spec.InstallNamespace {
		addonCopy.Status.Namespace = addonCopy.Spec.InstallNamespace
	}

	// Add config references
	if supported, config := isConfigurationSupported(addonCopy); !supported {
		meta.SetStatusCondition(&addonCopy.Status.Conditions, metav1.Condition{
			Type:    UnsupportedConfigurationType,
			Status:  metav1.ConditionTrue,
			Reason:  "ConfigurationUnsupported",
			Message: fmt.Sprintf("Configuration with gvr %s/%s is not supported for this addon", config.Group, config.Resource),
		})
		return c.patchAddonStatus(ctx, addonCopy, addon)
	}

	// Update unsupported configuration condition if configuration becomes corrected
	meta.SetStatusCondition(&addonCopy.Status.Conditions, metav1.Condition{
		Type:    UnsupportedConfigurationType,
		Status:  metav1.ConditionFalse,
		Reason:  "ConfigurationSupported",
		Message: "the config resources are supported",
	})

	return c.patchAddonStatus(ctx, addonCopy, addon)
}

func (c *addonStatusController) patchAddonStatus(ctx context.Context, new, old *addonapiv1alpha1.ManagedClusterAddOn) error {
	if equality.Semantic.DeepEqual(new.Status, old.Status) {
		return nil
	}

	oldData, err := json.Marshal(&addonapiv1alpha1.ManagedClusterAddOn{
		Status: addonapiv1alpha1.ManagedClusterAddOnStatus{
			RelatedObjects: old.Status.RelatedObjects,
			Namespace:      old.Status.Namespace,
			Conditions:     old.Status.Conditions,
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
			RelatedObjects: new.Status.RelatedObjects,
			Namespace:      new.Status.Namespace,
			Conditions:     new.Status.Conditions,
		},
	})
	if err != nil {
		return err
	}

	patchBytes, err := jsonpatch.CreateMergePatch(oldData, newData)
	if err != nil {
		return fmt.Errorf("failed to create patch for addon %s: %w", new.Name, err)
	}

	klog.V(2).Infof("Patching addon %s/%s condition with %s", new.Namespace, new.Name, string(patchBytes))
	_, err = c.addonClient.AddonV1alpha1().ManagedClusterAddOns(new.Namespace).Patch(
		ctx, new.Name, types.MergePatchType, patchBytes, metav1.PatchOptions{}, "status")
	return err
}

func isConfigurationSupported(addon *addonapiv1alpha1.ManagedClusterAddOn) (bool, addonapiv1alpha1.ConfigGroupResource) {
	supportedConfigSet := map[addonapiv1alpha1.ConfigGroupResource]bool{}
	for _, config := range addon.Status.SupportedConfigs {
		supportedConfigSet[config] = true
	}

	for _, config := range addon.Spec.Configs {
		if _, ok := supportedConfigSet[config.ConfigGroupResource]; !ok {
			return false, config.ConfigGroupResource
		}
	}

	return true, addonapiv1alpha1.ConfigGroupResource{}
}
