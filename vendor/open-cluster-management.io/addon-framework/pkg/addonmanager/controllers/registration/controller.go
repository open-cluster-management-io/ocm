package registration

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
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterlister "open-cluster-management.io/api/client/cluster/listers/cluster/v1"

	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/basecontroller/factory"
	"open-cluster-management.io/addon-framework/pkg/utils"
)

// addonRegistrationController reconciles instances of ManagedClusterAddon on the hub.
type addonRegistrationController struct {
	addonClient               addonv1alpha1client.Interface
	managedClusterLister      clusterlister.ManagedClusterLister
	managedClusterAddonLister addonlisterv1alpha1.ManagedClusterAddOnLister
	agentAddons               map[string]agent.AgentAddon
}

func NewAddonRegistrationController(
	addonClient addonv1alpha1client.Interface,
	clusterInformers clusterinformers.ManagedClusterInformer,
	addonInformers addoninformerv1alpha1.ManagedClusterAddOnInformer,
	agentAddons map[string]agent.AgentAddon,
) factory.Controller {
	c := &addonRegistrationController{
		addonClient:               addonClient,
		managedClusterLister:      clusterInformers.Lister(),
		managedClusterAddonLister: addonInformers.Lister(),
		agentAddons:               agentAddons,
	}

	return factory.New().WithFilteredEventsInformersQueueKeysFunc(
		func(obj runtime.Object) []string {
			key, _ := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
			return []string{key}
		},
		func(obj interface{}) bool {
			accessor, _ := meta.Accessor(obj)
			if _, ok := c.agentAddons[accessor.GetName()]; !ok {
				return false
			}

			return true
		},
		addonInformers.Informer()).
		WithSync(c.sync).ToController("addon-registration-controller")
}

func (c *addonRegistrationController) sync(ctx context.Context, syncCtx factory.SyncContext, key string) error {
	klog.V(4).Infof("Reconciling addon registration %q", key)

	clusterName, addonName, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		// ignore addon whose key is not in format: namespace/name
		return nil
	}

	agentAddon, ok := c.agentAddons[addonName]
	if !ok {
		return nil
	}

	// Get ManagedCluster
	managedCluster, err := c.managedClusterLister.Get(clusterName)
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	managedClusterAddon, err := c.managedClusterAddonLister.ManagedClusterAddOns(clusterName).Get(addonName)
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	managedClusterAddonCopy := managedClusterAddon.DeepCopy()

	// wait until the mca's ownerref is set.
	if !utils.IsOwnedByCMA(managedClusterAddonCopy) {
		return nil
	}

	var supportedConfigs []addonapiv1alpha1.ConfigGroupResource
	for _, config := range agentAddon.GetAgentAddonOptions().SupportedConfigGVRs {
		supportedConfigs = append(supportedConfigs, addonapiv1alpha1.ConfigGroupResource{
			Group:    config.Group,
			Resource: config.Resource,
		})
	}
	managedClusterAddonCopy.Status.SupportedConfigs = supportedConfigs

	registrationOption := agentAddon.GetAgentAddonOptions().Registration
	if registrationOption == nil {
		meta.SetStatusCondition(&managedClusterAddonCopy.Status.Conditions, metav1.Condition{
			Type:    addonapiv1alpha1.ManagedClusterAddOnRegistrationApplied,
			Status:  metav1.ConditionTrue,
			Reason:  addonapiv1alpha1.RegistrationAppliedNilRegistration,
			Message: "Registration of the addon agent is configured",
		})
		return c.patchAddonStatus(ctx, managedClusterAddonCopy, managedClusterAddon)
	}

	if registrationOption.PermissionConfig != nil {
		err = registrationOption.PermissionConfig(managedCluster, managedClusterAddonCopy)
		if err != nil {
			meta.SetStatusCondition(&managedClusterAddonCopy.Status.Conditions, metav1.Condition{
				Type:    addonapiv1alpha1.ManagedClusterAddOnRegistrationApplied,
				Status:  metav1.ConditionFalse,
				Reason:  addonapiv1alpha1.RegistrationAppliedSetPermissionFailed,
				Message: fmt.Sprintf("Failed to set permission for hub agent: %v", err),
			})
			if patchErr := c.patchAddonStatus(ctx, managedClusterAddonCopy, managedClusterAddon); patchErr != nil {
				return patchErr
			}
			return err
		}
	}

	if registrationOption.CSRConfigurations == nil {
		meta.SetStatusCondition(&managedClusterAddonCopy.Status.Conditions, metav1.Condition{
			Type:    addonapiv1alpha1.ManagedClusterAddOnRegistrationApplied,
			Status:  metav1.ConditionTrue,
			Reason:  addonapiv1alpha1.RegistrationAppliedNilRegistration,
			Message: "Registration of the addon agent is configured",
		})
		return c.patchAddonStatus(ctx, managedClusterAddonCopy, managedClusterAddon)
	}
	configs := registrationOption.CSRConfigurations(managedCluster)

	managedClusterAddonCopy.Status.Registrations = configs

	managedClusterAddonCopy.Status.Namespace = registrationOption.Namespace
	if len(managedClusterAddonCopy.Spec.InstallNamespace) > 0 {
		managedClusterAddonCopy.Status.Namespace = managedClusterAddonCopy.Spec.InstallNamespace
	}

	if registrationOption.AgentInstallNamespace != nil {
		ns := registrationOption.AgentInstallNamespace(managedClusterAddonCopy)
		if len(ns) > 0 {
			managedClusterAddonCopy.Status.Namespace = ns
		} else {
			klog.Infof("Namespace for addon %s/%s returned by agent install namespace func is empty",
				managedClusterAddonCopy.Namespace, managedClusterAddonCopy.Name)
		}
	}

	meta.SetStatusCondition(&managedClusterAddonCopy.Status.Conditions, metav1.Condition{
		Type:    addonapiv1alpha1.ManagedClusterAddOnRegistrationApplied,
		Status:  metav1.ConditionTrue,
		Reason:  addonapiv1alpha1.RegistrationAppliedSetPermissionApplied,
		Message: "Registration of the addon agent is configured",
	})

	return c.patchAddonStatus(ctx, managedClusterAddonCopy, managedClusterAddon)
}

func (c *addonRegistrationController) patchAddonStatus(ctx context.Context, new, old *addonapiv1alpha1.ManagedClusterAddOn) error {
	if equality.Semantic.DeepEqual(new.Status.Registrations, old.Status.Registrations) &&
		equality.Semantic.DeepEqual(new.Status.Conditions, old.Status.Conditions) &&
		equality.Semantic.DeepEqual(new.Status.SupportedConfigs, old.Status.SupportedConfigs) &&
		new.Status.Namespace == old.Status.Namespace {
		return nil
	}

	oldData, err := json.Marshal(&addonapiv1alpha1.ManagedClusterAddOn{
		Status: addonapiv1alpha1.ManagedClusterAddOnStatus{
			Registrations:    old.Status.Registrations,
			Namespace:        old.Status.Namespace,
			SupportedConfigs: old.Status.SupportedConfigs,
			Conditions:       old.Status.Conditions,
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
			Registrations:    new.Status.Registrations,
			Namespace:        new.Status.Namespace,
			SupportedConfigs: new.Status.SupportedConfigs,
			Conditions:       new.Status.Conditions,
		},
	})
	if err != nil {
		return err
	}

	patchBytes, err := jsonpatch.CreateMergePatch(oldData, newData)
	if err != nil {
		return fmt.Errorf("failed to create patch for addon %s: %w", new.Name, err)
	}

	klog.V(2).Infof("Patching addon %s/%s status with %s", new.Namespace, new.Name, string(patchBytes))
	_, err = c.addonClient.AddonV1alpha1().ManagedClusterAddOns(new.Namespace).Patch(
		ctx, new.Name, types.MergePatchType, patchBytes, metav1.PatchOptions{}, "status")
	return err
}
