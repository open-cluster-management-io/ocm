package addonconfiguration

import (
	"context"

	utilerrors "k8s.io/apimachinery/pkg/util/errors"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	"open-cluster-management.io/ocm/pkg/common/helpers"
)

type managedClusterAddonConfigurationReconciler struct {
	addonClient addonv1alpha1client.Interface
}

func (d *managedClusterAddonConfigurationReconciler) reconcile(
	ctx context.Context, cma *addonv1alpha1.ClusterManagementAddOn, graph *configurationGraph) (*addonv1alpha1.ClusterManagementAddOn, reconcileState, error) {
	var errs []error

	for _, addon := range graph.getAddonsToUpdate() {
		mca := d.mergeAddonConfig(addon.mca, addon.desiredConfigs)
		patcher := patcher.NewPatcher[
			*addonv1alpha1.ManagedClusterAddOn, addonv1alpha1.ManagedClusterAddOnSpec, addonv1alpha1.ManagedClusterAddOnStatus](
			d.addonClient.AddonV1alpha1().ManagedClusterAddOns(mca.Namespace))
		_, err := patcher.PatchStatus(ctx, mca, mca.Status, addon.mca.Status)
		if err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return cma, reconcileContinue, utilerrors.NewAggregate(errs)
	}

	if graph.getRequeueTime() < maxRequeueTime {
		return cma, reconcileContinue, helpers.NewRequeueError("Rollout requeue", graph.getRequeueTime())
	}

	return cma, reconcileContinue, nil
}

func (d *managedClusterAddonConfigurationReconciler) mergeAddonConfig(
	mca *addonv1alpha1.ManagedClusterAddOn, desiredConfigMap addonConfigMap) *addonv1alpha1.ManagedClusterAddOn {
	mcaCopy := mca.DeepCopy()

	mergedConfigs := make(map[addonv1alpha1.ConfigGroupResource][]addonv1alpha1.ConfigReference)
	// remove configs that are not desired
	for _, config := range mcaCopy.Status.ConfigReferences {
		if _, ok := desiredConfigMap[config.ConfigGroupResource]; ok {
			if _, ok := mergedConfigs[config.ConfigGroupResource]; !ok {
				mergedConfigs[config.ConfigGroupResource] = []addonv1alpha1.ConfigReference{}
			}
			mergedConfigs[config.ConfigGroupResource] = append(mergedConfigs[config.ConfigGroupResource], config)
		}
	}

	mergedConfigsCopy := make(map[addonv1alpha1.ConfigGroupResource][]addonv1alpha1.ConfigReference)
	for gvk := range mergedConfigs {
		mergedConfigsCopy[gvk] = append(mergedConfigsCopy[gvk], mergedConfigs[gvk]...)
	}

	// append or update configs
	for _, configArray := range desiredConfigMap {
		seenConfigs := map[addonv1alpha1.ConfigReferent]struct{}{}
		for _, config := range configArray {
			// Initialize GVK
			if len(seenConfigs) == 0 {
				mergedConfigs[config.ConfigGroupResource] = []addonv1alpha1.ConfigReference{}
			}
			// Skip duplicates
			if _, ok := seenConfigs[config.DesiredConfig.ConfigReferent]; ok {
				continue
			}

			newConfig := addonv1alpha1.ConfigReference{
				ConfigGroupResource:    config.ConfigGroupResource,
				DesiredConfig:          config.DesiredConfig.DeepCopy(),
				ConfigReferent:         config.ConfigReferent,
				LastObservedGeneration: 0,
			}

			// Only reset observed generation if the config was previously observed
			for _, exConfig := range mergedConfigsCopy[config.ConfigGroupResource] {
				if exConfig.DesiredConfig != nil && (exConfig.DesiredConfig.ConfigReferent == config.DesiredConfig.ConfigReferent) {
					newConfig.LastObservedGeneration = exConfig.LastObservedGeneration
				}
			}

			mergedConfigs[config.ConfigGroupResource] = append(mergedConfigs[config.ConfigGroupResource], newConfig)
			seenConfigs[config.DesiredConfig.ConfigReferent] = struct{}{}
		}
	}

	configRefs := []addonv1alpha1.ConfigReference{}
	for gvk := range mergedConfigs {
		configRefs = append(configRefs, mergedConfigs[gvk]...)
	}

	mcaCopy.Status.ConfigReferences = configRefs
	return mcaCopy
}
