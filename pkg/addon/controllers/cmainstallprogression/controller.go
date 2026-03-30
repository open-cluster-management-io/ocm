package cmainstallprogression

import (
	"context"
	"sort"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"

	addonv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	addonclient "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformerv1beta1 "open-cluster-management.io/api/client/addon/informers/externalversions/addon/v1beta1"
	addonlisterv1beta1 "open-cluster-management.io/api/client/addon/listers/addon/v1beta1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	"open-cluster-management.io/ocm/pkg/common/queue"
)

// cmaInstallProgressionController reconciles instances of clustermanagementaddon the hub
// based to update related object and status condition.
type cmaInstallProgressionController struct {
	patcher patcher.Patcher[
		*addonv1beta1.ClusterManagementAddOn, addonv1beta1.ClusterManagementAddOnSpec, addonv1beta1.ClusterManagementAddOnStatus]
	clusterManagementAddonLister addonlisterv1beta1.ClusterManagementAddOnLister
	addonFilterFunc              factory.EventFilterFunc
}

func NewCMAInstallProgressionController(
	addonClient addonclient.Interface,
	addonInformers addoninformerv1beta1.ManagedClusterAddOnInformer,
	clusterManagementAddonInformers addoninformerv1beta1.ClusterManagementAddOnInformer,
	addonFilterFunc factory.EventFilterFunc,
) factory.Controller {
	c := &cmaInstallProgressionController{
		patcher: patcher.NewPatcher[
			*addonv1beta1.ClusterManagementAddOn, addonv1beta1.ClusterManagementAddOnSpec, addonv1beta1.ClusterManagementAddOnStatus](
			addonClient.AddonV1beta1().ClusterManagementAddOns()),
		clusterManagementAddonLister: clusterManagementAddonInformers.Lister(),
		addonFilterFunc:              addonFilterFunc,
	}

	return factory.New().WithInformersQueueKeysFunc(
		queue.QueueKeyByMetaName,
		addonInformers.Informer(), clusterManagementAddonInformers.Informer()).
		WithSync(c.sync).ToController("cma-install-progression-controller")

}

func (c *cmaInstallProgressionController) sync(ctx context.Context, syncCtx factory.SyncContext, addonName string) error {
	logger := klog.FromContext(ctx).WithValues("addonName", addonName)
	logger.V(4).Info("Reconciling addon")
	mgmtAddon, err := c.clusterManagementAddonLister.Get(addonName)
	switch {
	case errors.IsNotFound(err):
		return nil
	case err != nil:
		return err
	}

	mgmtAddonCopy := mgmtAddon.DeepCopy()

	// set default config reference
	mgmtAddonCopy.Status.DefaultConfigReferences = setDefaultConfigReference(mgmtAddonCopy.Spec.DefaultConfigs, mgmtAddonCopy.Status.DefaultConfigReferences)

	// update default config reference when type is manual
	if mgmtAddonCopy.Spec.InstallStrategy.Type == "" || mgmtAddonCopy.Spec.InstallStrategy.Type == addonv1beta1.AddonInstallStrategyManual {
		mgmtAddonCopy.Status.InstallProgressions = []addonv1beta1.InstallProgression{}
		_, err = c.patcher.PatchStatus(ctx, mgmtAddonCopy, mgmtAddonCopy.Status, mgmtAddon.Status)
		return err
	}

	// only update default config references and skip updating install progression for self-managed addon
	if !c.addonFilterFunc(mgmtAddon) {
		_, err = c.patcher.PatchStatus(ctx, mgmtAddonCopy, mgmtAddonCopy.Status, mgmtAddon.Status)
		return err
	}

	// set install progression
	mgmtAddonCopy.Status.InstallProgressions = setInstallProgression(mgmtAddonCopy.Spec.DefaultConfigs,
		mgmtAddonCopy.Spec.InstallStrategy.Placements, mgmtAddonCopy.Status.InstallProgressions)

	// update cma status
	_, err = c.patcher.PatchStatus(ctx, mgmtAddonCopy, mgmtAddonCopy.Status, mgmtAddon.Status)
	return err
}

func setDefaultConfigReference(supportedConfigs []addonv1beta1.AddOnConfig,
	existDefaultConfigReferences []addonv1beta1.DefaultConfigReference) []addonv1beta1.DefaultConfigReference {
	newDefaultConfigReferences := []addonv1beta1.DefaultConfigReference{}
	for _, config := range supportedConfigs {
		if config.ConfigReferent.Name == "" {
			continue
		}
		configRef := addonv1beta1.DefaultConfigReference{
			ConfigGroupResource: config.ConfigGroupResource,
			DesiredConfig: &addonv1beta1.ConfigSpecHash{
				ConfigReferent: config.ConfigReferent,
			},
		}
		// if the config already exists in status, keep the existing spec hash
		if existConfigRef, exist := findDefaultConfigReference(&configRef, existDefaultConfigReferences); exist {
			configRef.DesiredConfig.SpecHash = existConfigRef.DesiredConfig.SpecHash
		}
		newDefaultConfigReferences = append(newDefaultConfigReferences, configRef)
	}
	return newDefaultConfigReferences
}

func findDefaultConfigReference(
	newobj *addonv1beta1.DefaultConfigReference,
	oldobjs []addonv1beta1.DefaultConfigReference,
) (*addonv1beta1.DefaultConfigReference, bool) {
	for _, oldconfig := range oldobjs {
		if oldconfig.ConfigGroupResource == newobj.ConfigGroupResource && oldconfig.DesiredConfig.ConfigReferent == newobj.DesiredConfig.ConfigReferent {
			return &oldconfig, true
		}
	}
	return nil, false
}

func setInstallProgression(supportedConfigs []addonv1beta1.AddOnConfig, placementStrategies []addonv1beta1.PlacementStrategy,
	existInstallProgressions []addonv1beta1.InstallProgression) []addonv1beta1.InstallProgression {
	newInstallProgressions := []addonv1beta1.InstallProgression{}
	for _, placementStrategy := range placementStrategies {
		// set placement ref
		installProgression := addonv1beta1.InstallProgression{
			PlacementRef: placementStrategy.PlacementRef,
		}

		// set config references as default configuration
		installConfigReferencesMap := map[addonv1beta1.ConfigGroupResource]sets.Set[addonv1beta1.ConfigReferent]{}
		for _, config := range supportedConfigs {
			if config.ConfigReferent.Name == "" || config.ConfigReferent.Namespace == addonv1beta1.ReservedNoDefaultConfigName {
				continue
			}
			refs, ok := installConfigReferencesMap[config.ConfigGroupResource]
			if !ok {
				refs = sets.New[addonv1beta1.ConfigReferent]()
				installConfigReferencesMap[config.ConfigGroupResource] = refs
			}
			refs.Insert(config.ConfigReferent)
		}

		// override the default configuration for each placement
		overrideConfigMapByAddOnConfigs(installConfigReferencesMap, placementStrategy.Configs)

		// sort gvk
		gvks := []addonv1beta1.ConfigGroupResource{}
		for gvk := range installConfigReferencesMap {
			gvks = append(gvks, gvk)
		}
		sort.Slice(gvks, func(i, j int) bool {
			if gvks[i].Group == gvks[j].Group {
				return gvks[i].Resource < gvks[j].Resource
			}
			return gvks[i].Group < gvks[j].Group
		})

		// set the config references for each install progression
		installConfigReferences := []addonv1beta1.InstallConfigReference{}
		for _, gvk := range gvks {
			if configRefs, ok := installConfigReferencesMap[gvk]; ok {
				for configRef := range configRefs {
					installConfigReferences = append(installConfigReferences,
						addonv1beta1.InstallConfigReference{
							ConfigGroupResource: gvk,
							DesiredConfig: &addonv1beta1.ConfigSpecHash{
								ConfigReferent: configRef,
							},
						},
					)
				}
			}
		}

		installProgression.ConfigReferences = installConfigReferences

		// if the config group resource already exists in status, merge the install progression
		if existInstallProgression, exist := findInstallProgression(&installProgression, existInstallProgressions); exist {
			mergeInstallProgression(&installProgression, existInstallProgression)
		}
		newInstallProgressions = append(newInstallProgressions, installProgression)
	}
	return newInstallProgressions
}

func findInstallProgression(newobj *addonv1beta1.InstallProgression, oldobjs []addonv1beta1.InstallProgression) (*addonv1beta1.InstallProgression, bool) {
	for _, oldobj := range oldobjs {
		if oldobj.PlacementRef == newobj.PlacementRef {
			count := 0
			for _, oldconfig := range oldobj.ConfigReferences {
				for _, newconfig := range newobj.ConfigReferences {
					if oldconfig.ConfigGroupResource == newconfig.ConfigGroupResource && oldconfig.DesiredConfig.ConfigReferent == newconfig.DesiredConfig.ConfigReferent {
						count += 1
					}
				}
			}
			if count == len(newobj.ConfigReferences) {
				return &oldobj, true
			}
		}
	}
	return nil, false
}

func mergeInstallProgression(newobj, oldobj *addonv1beta1.InstallProgression) {
	// merge config reference
	for i := range newobj.ConfigReferences {
		for _, oldconfig := range oldobj.ConfigReferences {
			if newobj.ConfigReferences[i].ConfigGroupResource == oldconfig.ConfigGroupResource &&
				newobj.ConfigReferences[i].DesiredConfig.ConfigReferent == oldconfig.DesiredConfig.ConfigReferent {
				if oldconfig.DesiredConfig.SpecHash != "" {
					newobj.ConfigReferences[i].DesiredConfig.SpecHash = oldconfig.DesiredConfig.SpecHash
				}
				newobj.ConfigReferences[i].LastAppliedConfig = oldconfig.LastAppliedConfig.DeepCopy()
				newobj.ConfigReferences[i].LastKnownGoodConfig = oldconfig.LastKnownGoodConfig.DeepCopy()
			}
		}
	}
	newobj.Conditions = oldobj.Conditions
}

// Override the desired configs by a slice of AddOnConfig (from cma install strategy),
func overrideConfigMapByAddOnConfigs(
	desiredConfigs map[addonv1beta1.ConfigGroupResource]sets.Set[addonv1beta1.ConfigReferent],
	addOnConfigs []addonv1beta1.AddOnConfig,
) {
	gvkOverwritten := sets.New[addonv1beta1.ConfigGroupResource]()
	// Go through the cma install strategy configs,
	// for a group of configs with same gvk, install strategy configs override the desiredConfigs.
	for _, config := range addOnConfigs {
		gr := config.ConfigGroupResource
		if !gvkOverwritten.Has(gr) {
			desiredConfigs[gr] = sets.New[addonv1beta1.ConfigReferent]()
			gvkOverwritten.Insert(gr)
		}
		// If a config not exist in the desiredConfigs, append it.
		// This is to avoid adding duplicate configs (name + namespace).
		if !desiredConfigs[gr].Has(config.ConfigReferent) {
			desiredConfigs[gr].Insert(config.ConfigReferent)
		}
	}
}
