package clustermanagement

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/basecontroller/factory"
	"open-cluster-management.io/addon-framework/pkg/utils"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformerv1alpha1 "open-cluster-management.io/api/client/addon/informers/externalversions/addon/v1alpha1"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterlister "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
)

const UnsupportedConfigurationType = "UnsupportedConfiguration"

// clusterManagementController reconciles instances of managedclusteradd on the hub
// based on the clustermanagementaddon.
type clusterManagementController struct {
	addonClient                  addonv1alpha1client.Interface
	managedClusterLister         clusterlister.ManagedClusterLister
	managedClusterAddonLister    addonlisterv1alpha1.ManagedClusterAddOnLister
	clusterManagementAddonLister addonlisterv1alpha1.ClusterManagementAddOnLister
	agentAddons                  map[string]agent.AgentAddon
}

func NewClusterManagementController(
	addonClient addonv1alpha1client.Interface,
	clusterInformers clusterinformers.ManagedClusterInformer,
	addonInformers addoninformerv1alpha1.ManagedClusterAddOnInformer,
	clusterManagementAddonInformers addoninformerv1alpha1.ClusterManagementAddOnInformer,
	agentAddons map[string]agent.AgentAddon,
) factory.Controller {
	c := &clusterManagementController{
		addonClient:                  addonClient,
		managedClusterLister:         clusterInformers.Lister(),
		managedClusterAddonLister:    addonInformers.Lister(),
		clusterManagementAddonLister: clusterManagementAddonInformers.Lister(),
		agentAddons:                  agentAddons,
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
		addonInformers.Informer(), clusterManagementAddonInformers.Informer()).
		WithSync(c.sync).ToController("cluster-management-addon-controller")
}

func (c *clusterManagementController) sync(ctx context.Context, syncCtx factory.SyncContext, key string) error {
	klog.V(4).Infof("Reconciling addon %q", key)

	namespace, addonName, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		// ignore addon whose key is invalid
		return nil
	}

	clusterManagementAddon, err := c.clusterManagementAddonLister.Get(addonName)
	switch {
	case errors.IsNotFound(err):
		return nil
	case err != nil:
		return err
	}

	if len(namespace) == 0 {
		return c.syncAllAddon(syncCtx, addonName)
	}

	addon, err := c.managedClusterAddonLister.ManagedClusterAddOns(namespace).Get(addonName)
	switch {
	case errors.IsNotFound(err):
		return nil
	case err != nil:
		return err
	}

	addonCopy := addon.DeepCopy()

	// Add owner if it does not exist
	owner := metav1.NewControllerRef(clusterManagementAddon, addonapiv1alpha1.GroupVersion.WithKind("ClusterManagementAddOn"))
	modified := utils.MergeOwnerRefs(&addonCopy.OwnerReferences, *owner, false)
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

	// Add config references
	if err := mergeConfigReferences(&modified, c.agentAddons[addonName], clusterManagementAddon, addonCopy); err != nil {
		meta.SetStatusCondition(&addonCopy.Status.Conditions, metav1.Condition{
			Type:    UnsupportedConfigurationType,
			Status:  metav1.ConditionTrue,
			Reason:  "ConfigurationUnsupported",
			Message: err.Error(),
		})
		return utils.PatchAddonCondition(ctx, c.addonClient, addonCopy, addon)
	}

	// Update unsupported configuration condition if configuration becomes corrected
	unsupportedConfigCondition := meta.FindStatusCondition(addon.Status.Conditions, UnsupportedConfigurationType)
	if unsupportedConfigCondition != nil {
		meta.SetStatusCondition(&addonCopy.Status.Conditions, metav1.Condition{
			Type:    UnsupportedConfigurationType,
			Status:  metav1.ConditionFalse,
			Reason:  "ConfigurationSupported",
			Message: "the config resources are supported",
		})
		modified = true
	}

	if !modified {
		return nil
	}

	_, err = c.addonClient.AddonV1alpha1().ManagedClusterAddOns(namespace).UpdateStatus(ctx, addonCopy, metav1.UpdateOptions{})
	return err
}

func (c *clusterManagementController) syncAllAddon(syncCtx factory.SyncContext, addonName string) error {
	clusters, err := c.managedClusterLister.List(labels.Everything())
	if err != nil {
		return err
	}

	for _, cluster := range clusters {
		addon, err := c.managedClusterAddonLister.ManagedClusterAddOns(cluster.Name).Get(addonName)
		switch {
		case errors.IsNotFound(err):
			continue
		case err != nil:
			return err
		}

		key, _ := cache.MetaNamespaceKeyFunc(addon)
		syncCtx.Queue().Add(key)
	}

	return nil
}

func mergeConfigReferences(
	modified *bool,
	agent agent.AgentAddon,
	clusterManagementAddon *addonapiv1alpha1.ClusterManagementAddOn,
	addon *addonapiv1alpha1.ManagedClusterAddOn,
) error {
	// make sure the supported configs in ClusterManagementAddon are registered and no duplicated
	cmaConfigSet, err := validateCMAConfigs(clusterManagementAddon.Spec.SupportedConfigs, agent.GetAgentAddonOptions().SupportedConfigGVRs)
	if err != nil {
		return err
	}

	if len(cmaConfigSet) == 0 {
		if len(addon.Spec.Configs) != 0 {
			return fmt.Errorf("the supported config resources are required in ClusterManagementAddon")
		}

		// the supported configs are not specified and no config refers in the managed cluster addon
		// for compatibility, try to merge old addon configuration
		// TODO  this will be removed after next few releases
		mergeAddOnConfiguration(modified, clusterManagementAddon, addon)
		return nil
	}

	// merge the ClusterManagementAddOn default configs and ManagedClusterAddOn configs
	// TODO After merged there may be multiple configs with the same group and resource in the config reference list,
	// currently, we save all of them, in the future, we may consider a way to define which config should be used
	expectedConfigReferences, err := mergeConfigs(cmaConfigSet, addon.Spec.Configs)
	if err != nil {
		return err
	}

	if len(expectedConfigReferences) == 0 {
		// the config references are not defined, ignore
		return nil
	}

	// we should ignore the last observed generation when we compare two config references
	actualConfigReferences := []addonapiv1alpha1.ConfigReference{}
	for _, config := range addon.Status.ConfigReferences {
		actualConfigReferences = append(actualConfigReferences, addonapiv1alpha1.ConfigReference{
			ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
				Group:    config.Group,
				Resource: config.Resource,
			},
			ConfigReferent: addonapiv1alpha1.ConfigReferent{
				Name:      config.Name,
				Namespace: config.Namespace,
			},
		})
	}

	if !equality.Semantic.DeepEqual(actualConfigReferences, expectedConfigReferences) {
		addon.Status.ConfigReferences = expectedConfigReferences
		*modified = true
	}

	return nil
}

// for compatibility, ignore the deprecation warnings
func mergeAddOnConfiguration(
	modified *bool,
	clusterManagementAddon *addonapiv1alpha1.ClusterManagementAddOn,
	addon *addonapiv1alpha1.ManagedClusterAddOn,
) {
	expectedCoordinate := addonapiv1alpha1.ConfigCoordinates{
		//nolint:staticcheck
		//lint:ignore SA1019 Ignore the deprecation warnings
		CRDName: clusterManagementAddon.Spec.AddOnConfiguration.CRDName,
		//nolint:staticcheck
		//lint:ignore SA1019 Ignore the deprecation warnings
		CRName: clusterManagementAddon.Spec.AddOnConfiguration.CRName,
	}
	actualCoordinate := addonapiv1alpha1.ConfigCoordinates{
		//nolint:staticcheck
		//lint:ignore SA1019 Ignore the deprecation warnings
		CRDName: addon.Status.AddOnConfiguration.CRDName,
		//nolint:staticcheck
		//lint:ignore SA1019 Ignore the deprecation warnings
		CRName: addon.Status.AddOnConfiguration.CRName,
	}

	if !equality.Semantic.DeepEqual(expectedCoordinate, actualCoordinate) {
		//nolint:staticcheck
		//lint:ignore SA1019 Ignore the deprecation warnings
		addon.Status.AddOnConfiguration.CRDName = expectedCoordinate.CRDName
		//nolint:staticcheck
		//lint:ignore SA1019 Ignore the deprecation warnings
		addon.Status.AddOnConfiguration.CRName = expectedCoordinate.CRName
		*modified = true
	}
}

func isRegistedConfig(gvrs []schema.GroupVersionResource, config addonapiv1alpha1.ConfigMeta) bool {
	for _, gvr := range gvrs {
		if gvr.Group == config.Group && gvr.Resource == config.Resource {
			return true
		}
	}
	return false
}

func listRegistedConfigs(gvrs []schema.GroupVersionResource) string {
	keys := make([]string, 0, len(gvrs))
	for _, gvr := range gvrs {
		keys = append(keys, gvr.String())
	}
	return strings.Join(keys, ";")
}

func validateCMAConfigs(cmaSupportedConfigs []addonapiv1alpha1.ConfigMeta,
	registedConfigs []schema.GroupVersionResource) (map[schema.GroupResource]*addonapiv1alpha1.ConfigReferent, error) {
	supportedConfigSet := map[schema.GroupResource]*addonapiv1alpha1.ConfigReferent{}
	for _, cmaConfig := range cmaSupportedConfigs {
		configGR := schema.GroupResource{
			Group:    cmaConfig.Group,
			Resource: cmaConfig.Resource,
		}

		_, existed := supportedConfigSet[configGR]
		if existed {
			return nil, fmt.Errorf("the config resource %q is duplicated", configGR.String())
		}

		// the supported config in ClusterManagementAddon should be registed in add-on framework
		if !isRegistedConfig(registedConfigs, cmaConfig) {
			return nil, fmt.Errorf("the config resource %q in ClusterManagementAddon is unregistered, registered configs: %q",
				configGR.String(), listRegistedConfigs(registedConfigs))
		}

		supportedConfigSet[configGR] = cmaConfig.DefaultConfig
	}

	return supportedConfigSet, nil
}

func mergeConfigs(
	cmaConfigSet map[schema.GroupResource]*addonapiv1alpha1.ConfigReferent,
	mcaConfigs []addonapiv1alpha1.AddOnConfig) ([]addonapiv1alpha1.ConfigReference, error) {
	configReferences := []addonapiv1alpha1.ConfigReference{}
	mcaConfigGRs := []schema.GroupResource{}

	// using ManagedClusterAddOn configs override the ClusterManagementAddOn default configs
	for _, mcaConfig := range mcaConfigs {
		configGR := schema.GroupResource{
			Group:    mcaConfig.Group,
			Resource: mcaConfig.Resource,
		}

		_, supported := cmaConfigSet[configGR]
		if !supported {
			return nil, fmt.Errorf("the config resource %q is unsupported", configGR.String())
		}

		if mcaConfig.Name == "" {
			return nil, fmt.Errorf("the config name is required in %q", configGR.String())
		}

		configReferences = append(configReferences, addonapiv1alpha1.ConfigReference{
			ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
				Group:    mcaConfig.Group,
				Resource: mcaConfig.Resource,
			},
			ConfigReferent: addonapiv1alpha1.ConfigReferent{
				Name:      mcaConfig.Name,
				Namespace: mcaConfig.Namespace,
			},
		})

		mcaConfigGRs = append(mcaConfigGRs, configGR)
	}

	// remove the ClusterManagementAddOn default configs from ManagedClusterAddOn configs
	for _, configGR := range mcaConfigGRs {
		delete(cmaConfigSet, configGR)
	}

	// add the default configs from ClusterManagementAddOn
	for groupResource, defautlConifg := range cmaConfigSet {
		if defautlConifg == nil {
			continue
		}

		configReferences = append(configReferences, addonapiv1alpha1.ConfigReference{
			ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
				Group:    groupResource.Group,
				Resource: groupResource.Resource,
			},
			ConfigReferent: addonapiv1alpha1.ConfigReferent{
				Name:      defautlConifg.Name,
				Namespace: defautlConifg.Namespace,
			},
		})
	}

	return configReferences, nil
}
