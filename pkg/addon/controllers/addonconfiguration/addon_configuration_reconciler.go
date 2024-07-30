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

	mergedConfigs := make(addonConfigMap)
	// remove configs that are not desired
	for _, configRef := range mcaCopy.Status.ConfigReferences {
		gr := configRef.ConfigGroupResource
		if _, ok := mergedConfigs[gr]; !ok {
			mergedConfigs[gr] = []addonv1alpha1.ConfigReference{}
		}
		if _, ok := desiredConfigMap.containsConfig(gr, configRef.DesiredConfig.ConfigReferent); ok {
			mergedConfigs[gr] = append(mergedConfigs[gr], configRef)
		}
	}

	// append or update configs
	for gr, configReferences := range desiredConfigMap {
		for _, configRef := range configReferences {
			if _, ok := mergedConfigs[gr]; !ok {
				mergedConfigs[gr] = []addonv1alpha1.ConfigReference{}
			}

			referent := configRef.DesiredConfig.ConfigReferent
			if i, exist := mergedConfigs.containsConfig(gr, referent); exist {
				mergedConfigs[gr][i].ConfigReferent = configRef.ConfigReferent
				mergedConfigs[gr][i].DesiredConfig = configRef.DesiredConfig.DeepCopy()
			} else {
				mergedConfigs[gr] = append(mergedConfigs[gr], configRef)
			}
		}
	}

	// sort by gvk and set the final config references
	configRefs := []addonv1alpha1.ConfigReference{}
	for _, gvk := range mergedConfigs.orderedKeys() {
		configRefs = append(configRefs, mergedConfigs[gvk]...)
	}
	mcaCopy.Status.ConfigReferences = configRefs
	return mcaCopy
}
