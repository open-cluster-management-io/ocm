package addonconfiguration

import (
	"context"
	"encoding/json"
	"fmt"

	jsonpatch "github.com/evanphx/json-patch"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/klog/v2"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
)

type managedClusterAddonConfigurationReconciler struct {
	addonClient addonv1alpha1client.Interface
}

func (d *managedClusterAddonConfigurationReconciler) reconcile(
	ctx context.Context, cma *addonv1alpha1.ClusterManagementAddOn, graph *configurationGraph) (*addonv1alpha1.ClusterManagementAddOn, reconcileState, error) {
	var errs []error

	for _, addon := range graph.getAddonsToUpdate() {
		mca := d.mergeAddonConfig(addon.mca, addon.desiredConfigs)
		err := d.patchAddonStatus(ctx, mca, addon.mca)
		if err != nil {
			errs = append(errs, err)
		}
	}

	return cma, reconcileContinue, utilerrors.NewAggregate(errs)
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

func (d *managedClusterAddonConfigurationReconciler) patchAddonStatus(ctx context.Context, new, old *addonv1alpha1.ManagedClusterAddOn) error {
	if equality.Semantic.DeepEqual(new.Status, old.Status) {
		return nil
	}

	oldData, err := json.Marshal(&addonv1alpha1.ManagedClusterAddOn{
		Status: addonv1alpha1.ManagedClusterAddOnStatus{
			Namespace:        old.Status.Namespace,
			ConfigReferences: old.Status.ConfigReferences,
		},
	})
	if err != nil {
		return err
	}

	newData, err := json.Marshal(&addonv1alpha1.ManagedClusterAddOn{
		ObjectMeta: metav1.ObjectMeta{
			UID:             new.UID,
			ResourceVersion: new.ResourceVersion,
		},
		Status: addonv1alpha1.ManagedClusterAddOnStatus{
			Namespace:        new.Status.Namespace,
			ConfigReferences: new.Status.ConfigReferences,
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
	_, err = d.addonClient.AddonV1alpha1().ManagedClusterAddOns(new.Namespace).Patch(
		ctx, new.Name, types.MergePatchType, patchBytes, metav1.PatchOptions{}, "status")
	return err
}
