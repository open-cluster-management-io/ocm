package addonconfiguration

import (
	"context"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"

	addonv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	addonclient "open-cluster-management.io/api/client/addon/clientset/versioned"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	"open-cluster-management.io/ocm/pkg/common/helpers"
)

type managedClusterAddonConfigurationReconciler struct {
	addonClient addonclient.Interface
}

func (d *managedClusterAddonConfigurationReconciler) reconcile(
	ctx context.Context, cma *addonv1beta1.ClusterManagementAddOn, graph *configurationGraph) (*addonv1beta1.ClusterManagementAddOn, reconcileState, error) {
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
	mca *addonv1beta1.ManagedClusterAddOn, desiredConfigMap addonConfigMap) *addonv1beta1.ManagedClusterAddOn {
	mcaCopy := mca.DeepCopy()

	type configKey struct {
		gr      addonv1beta1.ConfigGroupResource
		refName string
		refNs   string
	}

	// Build a lookup from old status to preserve LastAppliedConfig and LastObservedGeneration
	oldStatusByKey := map[configKey]addonv1beta1.ConfigReference{}
	for _, configRef := range mcaCopy.Status.ConfigReferences {
		if configRef.DesiredConfig == nil {
			continue
		}
		key := configKey{
			gr:      configRef.ConfigGroupResource,
			refName: configRef.DesiredConfig.ConfigReferent.Name,
			refNs:   configRef.DesiredConfig.ConfigReferent.Namespace,
		}
		oldStatusByKey[key] = configRef
	}

	// Iterate desiredConfigMap in sorted GR order — this is the source of truth for ordering.
	// Within each GR, the slice order from the desired config map is preserved.
	configRefs := []addonv1beta1.ConfigReference{}
	for _, gr := range desiredConfigMap.orderedKeys() {
		for _, configRef := range desiredConfigMap[gr] {
			key := configKey{
				gr:      gr,
				refName: configRef.DesiredConfig.ConfigReferent.Name,
				refNs:   configRef.DesiredConfig.ConfigReferent.Namespace,
			}
			if old, ok := oldStatusByKey[key]; ok {
				// Preserve rollout tracking fields, update desired config
				old.DesiredConfig = configRef.DesiredConfig.DeepCopy()
				configRefs = append(configRefs, old)
			} else {
				configRefs = append(configRefs, configRef)
			}
		}
	}

	mcaCopy.Status.ConfigReferences = configRefs
	return mcaCopy
}

// setCondition updates the configured condition for the addon
func (d *managedClusterAddonConfigurationReconciler) setCondition(
	addon *addonv1beta1.ManagedClusterAddOn, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
		Type:    addonv1beta1.ManagedClusterAddOnConditionConfigured,
		Status:  status,
		Reason:  reason,
		Message: message,
	})
}

// patchAddonStatus patches the status of the addon
func (d *managedClusterAddonConfigurationReconciler) patchAddonStatus(
	ctx context.Context, newaddon *addonv1beta1.ManagedClusterAddOn, oldaddon *addonv1beta1.ManagedClusterAddOn) error {
	patcher := patcher.NewPatcher[
		*addonv1beta1.ManagedClusterAddOn, addonv1beta1.ManagedClusterAddOnSpec, addonv1beta1.ManagedClusterAddOnStatus](
		d.addonClient.AddonV1beta1().ManagedClusterAddOns(newaddon.Namespace))

	_, err := patcher.PatchStatus(ctx, newaddon, newaddon.Status, oldaddon.Status)
	return err
}
