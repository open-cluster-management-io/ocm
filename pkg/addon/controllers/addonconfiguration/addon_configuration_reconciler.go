package addonconfiguration

import (
	"context"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"

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
	configured := sets.Set[string]{}

	// Update the config references and set the "configured" condition to true for addons that are ready for rollout.
	// These addons are part of the current rollout batch according to the strategy.
	for _, addon := range graph.getAddonsToUpdate() {
		// update mca config references in status
		newAddon := d.mergeAddonConfig(addon.mca, addon.desiredConfigs)
		// update mca configured condition to true
		d.setCondition(newAddon, metav1.ConditionTrue, "ConfigurationsConfigured", "Configurations configured")

		err := d.patchAddonStatus(ctx, newAddon, addon.mca)
		if err != nil {
			errs = append(errs, err)
		}

		configured.Insert(addon.mca.Namespace)
	}

	// Set the "configured" condition to false for addons whose configurations have not been synced yet
	// but are waiting for rollout.
	for _, addon := range graph.getAddonsToApply() {
		// Skip addons that have already been configured.
		if configured.Has(addon.mca.Namespace) {
			continue
		}
		newAddon := addon.mca.DeepCopy()
		d.setCondition(newAddon, metav1.ConditionFalse, "ConfigurationsNotConfigured", "Configurations updated and not configured yet")

		err := d.patchAddonStatus(ctx, newAddon, addon.mca)
		if err != nil {
			errs = append(errs, err)
		}
	}

	// Set the "configured" condition to true for addons that have successfully completed rollout.
	// This includes:
	// a. Addons without any configurations that have had their rollout status set to success in setRolloutStatus().
	// b. Addons with configurations and already rollout successfully. In upgrade scenario, when the
	// addon configurations do not change while addon components upgrade, should set condition to true.
	for _, addon := range graph.getAddonsSucceeded() {
		newAddon := addon.mca.DeepCopy()
		d.setCondition(newAddon, metav1.ConditionTrue, "ConfigurationsConfigured", "Configurations configured")

		err := d.patchAddonStatus(ctx, newAddon, addon.mca)
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
	// First go through the configReferences listed in mca status,
	// if the existing config (gvk + namespace + name) is also in the desiredConfigMap, append it to mergedConfigs,
	// this will save the LastAppliedConfig and LastObservedGeneration from mca status.
	for _, configRef := range mcaCopy.Status.ConfigReferences {
		gr := configRef.ConfigGroupResource
		if _, ok := mergedConfigs[gr]; !ok {
			mergedConfigs[gr] = []addonv1alpha1.ConfigReference{}
		}
		if _, ok := desiredConfigMap.containsConfig(gr, configRef.DesiredConfig.ConfigReferent); ok {
			mergedConfigs[gr] = append(mergedConfigs[gr], configRef)
		}
	}

	// Then go through the desiredConfigMap, for each configReference,
	// if the desired config (gvk + namespace + name) is aleady in the mergedConfigs,
	// update the ConfigReferent and DesiredConfig (including desired spechash) to mergedConfigs.
	// else just append it to the mergedConfigs.
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

// setCondition updates the configured condition for the addon
func (d *managedClusterAddonConfigurationReconciler) setCondition(
	addon *addonv1alpha1.ManagedClusterAddOn, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
		Type:    addonv1alpha1.ManagedClusterAddOnConditionConfigured,
		Status:  status,
		Reason:  reason,
		Message: message,
	})
}

// patchAddonStatus patches the status of the addon
func (d *managedClusterAddonConfigurationReconciler) patchAddonStatus(
	ctx context.Context, newaddon *addonv1alpha1.ManagedClusterAddOn, oldaddon *addonv1alpha1.ManagedClusterAddOn) error {
	patcher := patcher.NewPatcher[
		*addonv1alpha1.ManagedClusterAddOn, addonv1alpha1.ManagedClusterAddOnSpec, addonv1alpha1.ManagedClusterAddOnStatus](
		d.addonClient.AddonV1alpha1().ManagedClusterAddOns(newaddon.Namespace))

	_, err := patcher.PatchStatus(ctx, newaddon, newaddon.Status, oldaddon.Status)
	return err
}
