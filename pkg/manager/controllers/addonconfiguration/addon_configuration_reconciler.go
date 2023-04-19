package addonconfiguration

import (
	"context"
	"encoding/json"
	"fmt"

	jsonpatch "github.com/evanphx/json-patch"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"

	"open-cluster-management.io/addon-framework/pkg/index"
)

type managedClusterAddonConfigurationReconciler struct {
	addonClient                addonv1alpha1client.Interface
	managedClusterAddonIndexer cache.Indexer
	getClustersByPlacement     func(name, namespace string) ([]string, error)
}

func (d *managedClusterAddonConfigurationReconciler) buildConfigurationGraph(cma *addonv1alpha1.ClusterManagementAddOn) (*configurationGraph, error) {
	graph := newGraph(cma.Spec.SupportedConfigs, cma.Status.DefaultConfigReferences)
	addons, err := d.managedClusterAddonIndexer.ByIndex(index.ManagedClusterAddonByName, cma.Name)
	if err != nil {
		return graph, err
	}

	// add all existing addons to the default at first
	for _, addonObject := range addons {
		addon := addonObject.(*addonv1alpha1.ManagedClusterAddOn)
		graph.addAddonNode(addon)
	}

	if cma.Spec.InstallStrategy.Type == "" || cma.Spec.InstallStrategy.Type == addonv1alpha1.AddonInstallStrategyManual {
		return graph, nil
	}

	// check each install strategy in status and override the default configs.
	var errs []error
	for _, installProgression := range cma.Status.InstallProgressions {
		clusters, err := d.getClustersByPlacement(installProgression.PlacementRef.Name, installProgression.PlacementRef.Namespace)
		if errors.IsNotFound(err) {
			klog.V(2).Infof("placement %s/%s is not found for addon %s", installProgression.PlacementRef.Namespace, installProgression.PlacementRef.Name, cma.Name)
			continue
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}

		graph.addPlacementNode(installProgression.ConfigReferences, clusters)
	}

	return graph, utilerrors.NewAggregate(errs)
}

func (d *managedClusterAddonConfigurationReconciler) reconcile(
	ctx context.Context, cma *addonv1alpha1.ClusterManagementAddOn) (*addonv1alpha1.ClusterManagementAddOn, reconcileState, error) {

	var errs []error
	// build the configuration graph
	graph, err := d.buildConfigurationGraph(cma)
	if err != nil {
		errs = append(errs, err)
	}

	for _, addon := range graph.addonToUpdate() {
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
			if mergedConfigs[i].DesiredConfig == nil {
				continue
			}
			if mergedConfigs[i].ConfigGroupResource != config.ConfigGroupResource {
				continue
			}

			match = true
			// set LastObservedGeneration to 0 when config name/namespace changes
			if mergedConfigs[i].DesiredConfig.ConfigReferent != config.DesiredConfig.ConfigReferent {
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
