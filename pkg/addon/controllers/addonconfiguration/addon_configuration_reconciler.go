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

	var mergedConfigs []addonv1alpha1.ConfigReference
	// remove configs that are not desired
	for _, config := range mcaCopy.Status.ConfigReferences {
		if _, ok := desiredConfigMap[config.ConfigGroupResource]; ok {
			mergedConfigs = append(mergedConfigs, config)
		}
	}

	// append or update configs
	for _, config := range desiredConfigMap {
		var match bool
		for i := range mergedConfigs {
			if mergedConfigs[i].ConfigGroupResource != config.ConfigGroupResource {
				continue
			}

			match = true
			// set LastObservedGeneration to 0 when config name/namespace changes
			if mergedConfigs[i].DesiredConfig != nil && (mergedConfigs[i].DesiredConfig.ConfigReferent != config.DesiredConfig.ConfigReferent) {
				mergedConfigs[i].LastObservedGeneration = 0
			}
			mergedConfigs[i].ConfigReferent = config.ConfigReferent
			mergedConfigs[i].DesiredConfig = config.DesiredConfig.DeepCopy()
		}

		if !match {
			mergedConfigs = append(mergedConfigs, config)
		}
	}

	mcaCopy.Status.ConfigReferences = mergedConfigs
	return mcaCopy
}
