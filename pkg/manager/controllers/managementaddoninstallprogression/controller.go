package managementaddoninstallprogression

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
	"k8s.io/klog/v2"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformerv1alpha1 "open-cluster-management.io/api/client/addon/informers/externalversions/addon/v1alpha1"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"

	"open-cluster-management.io/addon-framework/pkg/basecontroller/factory"
)

// managementAddonInstallProgressionController reconciles instances of clustermanagementaddon the hub
// based to update related object and status condition.
type managementAddonInstallProgressionController struct {
	addonClient                  addonv1alpha1client.Interface
	managedClusterAddonLister    addonlisterv1alpha1.ManagedClusterAddOnLister
	clusterManagementAddonLister addonlisterv1alpha1.ClusterManagementAddOnLister
}

func NewManagementAddonInstallProgressionController(
	addonClient addonv1alpha1client.Interface,
	addonInformers addoninformerv1alpha1.ManagedClusterAddOnInformer,
	clusterManagementAddonInformers addoninformerv1alpha1.ClusterManagementAddOnInformer,
) factory.Controller {
	c := &managementAddonInstallProgressionController{
		addonClient:                  addonClient,
		managedClusterAddonLister:    addonInformers.Lister(),
		clusterManagementAddonLister: clusterManagementAddonInformers.Lister(),
	}

	return factory.New().WithInformersQueueKeysFunc(
		func(obj runtime.Object) []string {
			accessor, _ := meta.Accessor(obj)
			return []string{accessor.GetName()}
		},
		addonInformers.Informer(), clusterManagementAddonInformers.Informer()).
		WithSync(c.sync).ToController("management-addon-status-controller")

}

func (c *managementAddonInstallProgressionController) sync(ctx context.Context, syncCtx factory.SyncContext, addonName string) error {
	klog.V(4).Infof("Reconciling addon %q", addonName)

	mgmtAddon, err := c.clusterManagementAddonLister.Get(addonName)
	switch {
	case errors.IsNotFound(err):
		return nil
	case err != nil:
		return err
	}

	mgmtAddonCopy := mgmtAddon.DeepCopy()

	// set default config reference
	mgmtAddonCopy.Status.DefaultConfigReferences = setDefaultConfigReference(mgmtAddonCopy.Spec.SupportedConfigs, mgmtAddonCopy.Status.DefaultConfigReferences)

	// update default config reference when type is manual
	if mgmtAddonCopy.Spec.InstallStrategy.Type == "" || mgmtAddonCopy.Spec.InstallStrategy.Type == addonv1alpha1.AddonInstallStrategyManual {
		mgmtAddonCopy.Status.InstallProgressions = []addonv1alpha1.InstallProgression{}
		return c.patchMgmtAddonStatus(ctx, mgmtAddonCopy, mgmtAddon)
	}

	// set install progression
	mgmtAddonCopy.Status.InstallProgressions = setInstallProgression(mgmtAddonCopy.Spec.SupportedConfigs,
		mgmtAddonCopy.Spec.InstallStrategy.Placements, mgmtAddonCopy.Status.InstallProgressions)

	// update cma status
	return c.patchMgmtAddonStatus(ctx, mgmtAddonCopy, mgmtAddon)
}

func (c *managementAddonInstallProgressionController) patchMgmtAddonStatus(ctx context.Context, new, old *addonv1alpha1.ClusterManagementAddOn) error {
	if equality.Semantic.DeepEqual(new.Status, old.Status) {
		return nil
	}

	oldData, err := json.Marshal(&addonv1alpha1.ClusterManagementAddOn{
		Status: addonv1alpha1.ClusterManagementAddOnStatus{
			DefaultConfigReferences: old.Status.DefaultConfigReferences,
			InstallProgressions:     old.Status.InstallProgressions,
		},
	})
	if err != nil {
		return err
	}

	newData, err := json.Marshal(&addonv1alpha1.ClusterManagementAddOn{
		ObjectMeta: metav1.ObjectMeta{
			UID:             new.UID,
			ResourceVersion: new.ResourceVersion,
		},
		Status: addonv1alpha1.ClusterManagementAddOnStatus{
			DefaultConfigReferences: new.Status.DefaultConfigReferences,
			InstallProgressions:     new.Status.InstallProgressions,
		},
	})
	if err != nil {
		return err
	}

	patchBytes, err := jsonpatch.CreateMergePatch(oldData, newData)
	if err != nil {
		return fmt.Errorf("failed to create patch for addon %s: %w", new.Name, err)
	}

	klog.V(2).Infof("Patching clustermanagementaddon %s status with %s", new.Name, string(patchBytes))
	_, err = c.addonClient.AddonV1alpha1().ClusterManagementAddOns().Patch(
		ctx, new.Name, types.MergePatchType, patchBytes, metav1.PatchOptions{}, "status")
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

		// if the config already exists in status, keep the existing spec hash
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

func mergeInstallProgression(newobj, oldobj *addonv1alpha1.InstallProgression) {
	// merge config reference
	for i := range newobj.ConfigReferences {
		for _, oldconfig := range oldobj.ConfigReferences {
			if newobj.ConfigReferences[i].ConfigGroupResource == oldconfig.ConfigGroupResource &&
				newobj.ConfigReferences[i].DesiredConfig.ConfigReferent == oldconfig.DesiredConfig.ConfigReferent {
				newobj.ConfigReferences[i].DesiredConfig.SpecHash = oldconfig.DesiredConfig.SpecHash
				newobj.ConfigReferences[i].LastAppliedConfig = oldconfig.LastAppliedConfig
				newobj.ConfigReferences[i].LastKnownGoodConfig = oldconfig.LastKnownGoodConfig
			}
		}
	}
	newobj.Conditions = oldobj.Conditions
}
