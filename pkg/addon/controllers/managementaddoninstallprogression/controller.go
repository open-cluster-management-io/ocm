package managementaddoninstallprogression

import (
	"context"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformerv1alpha1 "open-cluster-management.io/api/client/addon/informers/externalversions/addon/v1alpha1"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	"open-cluster-management.io/ocm/pkg/common/queue"
)

// managementAddonInstallProgressionController reconciles instances of clustermanagementaddon the hub
// based to update related object and status condition.
type managementAddonInstallProgressionController struct {
	patcher patcher.Patcher[
		*addonv1alpha1.ClusterManagementAddOn, addonv1alpha1.ClusterManagementAddOnSpec, addonv1alpha1.ClusterManagementAddOnStatus]
	managedClusterAddonLister    addonlisterv1alpha1.ManagedClusterAddOnLister
	clusterManagementAddonLister addonlisterv1alpha1.ClusterManagementAddOnLister
	addonFilterFunc              factory.EventFilterFunc
}

func NewManagementAddonInstallProgressionController(
	addonClient addonv1alpha1client.Interface,
	addonInformers addoninformerv1alpha1.ManagedClusterAddOnInformer,
	clusterManagementAddonInformers addoninformerv1alpha1.ClusterManagementAddOnInformer,
	addonFilterFunc factory.EventFilterFunc,
	recorder events.Recorder,
) factory.Controller {
	c := &managementAddonInstallProgressionController{
		patcher: patcher.NewPatcher[
			*addonv1alpha1.ClusterManagementAddOn, addonv1alpha1.ClusterManagementAddOnSpec, addonv1alpha1.ClusterManagementAddOnStatus](
			addonClient.AddonV1alpha1().ClusterManagementAddOns()),
		managedClusterAddonLister:    addonInformers.Lister(),
		clusterManagementAddonLister: clusterManagementAddonInformers.Lister(),
		addonFilterFunc:              addonFilterFunc,
	}

	return factory.New().WithInformersQueueKeysFunc(
		queue.QueueKeyByMetaName,
		addonInformers.Informer(), clusterManagementAddonInformers.Informer()).
		WithSync(c.sync).ToController("management-addon-status-controller", recorder)

}

func (c *managementAddonInstallProgressionController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	addonName := syncCtx.QueueKey()
	logger := klog.FromContext(ctx)
	logger.V(4).Info("Reconciling addon", "addonName", addonName)
	mgmtAddon, err := c.clusterManagementAddonLister.Get(addonName)
	switch {
	case errors.IsNotFound(err):
		return nil
	case err != nil:
		return err
	}

	mgmtAddonCopy := mgmtAddon.DeepCopy()

	clusterManagementAddon, err := c.clusterManagementAddonLister.Get(addonName)
	if errors.IsNotFound(err) {
		return nil
	}

	if err != nil {
		return err
	}

	// set default config reference
	mgmtAddonCopy.Status.DefaultConfigReferences = setDefaultConfigReference(mgmtAddonCopy.Spec.SupportedConfigs, mgmtAddonCopy.Status.DefaultConfigReferences)

	// update default config reference when type is manual
	if mgmtAddonCopy.Spec.InstallStrategy.Type == "" || mgmtAddonCopy.Spec.InstallStrategy.Type == addonv1alpha1.AddonInstallStrategyManual {
		mgmtAddonCopy.Status.InstallProgressions = []addonv1alpha1.InstallProgression{}
		_, err = c.patcher.PatchStatus(ctx, mgmtAddonCopy, mgmtAddonCopy.Status, mgmtAddon.Status)
		return err
	}

	// only update default config references and skip updating install progression for self-managed addon
	if !c.addonFilterFunc(clusterManagementAddon) {
		_, err = c.patcher.PatchStatus(ctx, mgmtAddonCopy, mgmtAddonCopy.Status, mgmtAddon.Status)
		return err
	}

	// set install progression
	mgmtAddonCopy.Status.InstallProgressions = setInstallProgression(mgmtAddonCopy.Spec.SupportedConfigs,
		mgmtAddonCopy.Spec.InstallStrategy.Placements, mgmtAddonCopy.Status.InstallProgressions)

	// update cma status
	_, err = c.patcher.PatchStatus(ctx, mgmtAddonCopy, mgmtAddonCopy.Status, mgmtAddon.Status)
	return err
}

func setDefaultConfigReference(supportedConfigs []addonv1alpha1.ConfigMeta,
	existDefaultConfigReferences []addonv1alpha1.DefaultConfigReference) []addonv1alpha1.DefaultConfigReference {
	newDefaultConfigReferences := []addonv1alpha1.DefaultConfigReference{}
	for _, config := range supportedConfigs {
		if config.DefaultConfig == nil {
			continue
		}
		configRef := addonv1alpha1.DefaultConfigReference{
			ConfigGroupResource: config.ConfigGroupResource,
			DesiredConfig: &addonv1alpha1.ConfigSpecHash{
				ConfigReferent: *config.DefaultConfig,
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
	newobj *addonv1alpha1.DefaultConfigReference,
	oldobjs []addonv1alpha1.DefaultConfigReference,
) (*addonv1alpha1.DefaultConfigReference, bool) {
	for _, oldconfig := range oldobjs {
		if oldconfig.ConfigGroupResource == newobj.ConfigGroupResource && oldconfig.DesiredConfig.ConfigReferent == newobj.DesiredConfig.ConfigReferent {
			return &oldconfig, true
		}
	}
	return nil, false
}

func setInstallProgression(supportedConfigs []addonv1alpha1.ConfigMeta, placementStrategies []addonv1alpha1.PlacementStrategy,
	existInstallProgressions []addonv1alpha1.InstallProgression) []addonv1alpha1.InstallProgression {
	newInstallProgressions := []addonv1alpha1.InstallProgression{}
	for _, placementStrategy := range placementStrategies {
		// set placement ref
		installProgression := addonv1alpha1.InstallProgression{
			PlacementRef: placementStrategy.PlacementRef,
		}

		// set config references as default configuration
		installConfigReferences := []addonv1alpha1.InstallConfigReference{}
		installConfigReferencesMap := map[addonv1alpha1.ConfigGroupResource]addonv1alpha1.ConfigReferent{}
		for _, config := range supportedConfigs {
			if config.DefaultConfig != nil {
				installConfigReferencesMap[config.ConfigGroupResource] = *config.DefaultConfig
			}
		}

		// override the default configuration for each placement
		for _, config := range placementStrategy.Configs {
			installConfigReferencesMap[config.ConfigGroupResource] = config.ConfigReferent
		}

		// set the config references for each install progression
		for k, v := range installConfigReferencesMap {
			installConfigReferences = append(installConfigReferences,
				addonv1alpha1.InstallConfigReference{
					ConfigGroupResource: k,
					DesiredConfig: &addonv1alpha1.ConfigSpecHash{
						ConfigReferent: v,
					},
				},
			)
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

func findInstallProgression(newobj *addonv1alpha1.InstallProgression, oldobjs []addonv1alpha1.InstallProgression) (*addonv1alpha1.InstallProgression, bool) {
	for _, oldobj := range oldobjs {
		if oldobj.PlacementRef == newobj.PlacementRef {
			count := 0
			for _, oldconfig := range oldobj.ConfigReferences {
				for _, newconfig := range newobj.ConfigReferences {
					if oldconfig.ConfigGroupResource == newconfig.ConfigGroupResource {
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

func mergeInstallProgression(newobj, oldobj *addonv1alpha1.InstallProgression) {
	// merge config reference
	for i := range newobj.ConfigReferences {
		for _, oldconfig := range oldobj.ConfigReferences {
			if newobj.ConfigReferences[i].ConfigGroupResource == oldconfig.ConfigGroupResource {
				if newobj.ConfigReferences[i].DesiredConfig.ConfigReferent == oldconfig.DesiredConfig.ConfigReferent {
					newobj.ConfigReferences[i].DesiredConfig.SpecHash = oldconfig.DesiredConfig.SpecHash
				}
				newobj.ConfigReferences[i].LastAppliedConfig = oldconfig.LastAppliedConfig.DeepCopy()
				newobj.ConfigReferences[i].LastKnownGoodConfig = oldconfig.LastKnownGoodConfig.DeepCopy()
			}
		}
	}
	newobj.Conditions = oldobj.Conditions
}
