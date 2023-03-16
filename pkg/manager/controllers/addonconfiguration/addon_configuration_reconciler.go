package addonconfiguration

import (
	"context"
	"encoding/json"
	"fmt"

	jsonpatch "github.com/evanphx/json-patch"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	clusterlisterv1beta1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1beta1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"

	"open-cluster-management.io/addon-framework/pkg/index"
)

type managedClusterAddonConfigurationReconciler struct {
	addonClient                addonv1alpha1client.Interface
	managedClusterAddonIndexer cache.Indexer
	placementLister            clusterlisterv1beta1.PlacementLister
	placementDecisionLister    clusterlisterv1beta1.PlacementDecisionLister
}

func (d *managedClusterAddonConfigurationReconciler) buildConfigurationGraph(cma *addonv1alpha1.ClusterManagementAddOn) (*configurationGraph, error) {
	graph := newGraph(cma.Spec.SupportedConfigs)
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

	// check each install strategy and override the default configs.
	var errs []error
	for _, strategy := range cma.Spec.InstallStrategy.Placements {
		clusters, err := d.getClustersByPlacementRef(strategy.PlacementRef)
		if errors.IsNotFound(err) {
			klog.V(2).Infof("placement %s/%s is not found for addon %s", strategy.PlacementRef.Namespace, strategy.PlacementRef.Name, cma.Name)
			continue
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}

		graph.addPlacementNode(strategy.Configs, clusters)
	}

	return graph, utilerrors.NewAggregate(errs)
}

func (d *managedClusterAddonConfigurationReconciler) getClustersByPlacementRef(ref addonv1alpha1.PlacementRef) ([]string, error) {
	var clusters []string
	_, err := d.placementLister.Placements(ref.Namespace).Get(ref.Name)
	if err != nil {
		return clusters, err
	}

	decisionSelector := labels.SelectorFromSet(labels.Set{
		clusterv1beta1.PlacementLabel: ref.Name,
	})
	decisions, err := d.placementDecisionLister.PlacementDecisions(ref.Namespace).List(decisionSelector)
	if err != nil {
		return clusters, err
	}

	for _, d := range decisions {
		for _, sd := range d.Status.Decisions {
			clusters = append(clusters, sd.ClusterName)
		}
	}

	return clusters, nil
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
	for _, config := range mca.Status.ConfigReferences {
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
			if mergedConfigs[i].ConfigReferent != config.ConfigReferent {
				mergedConfigs[i].ConfigReferent = config.ConfigReferent
				mergedConfigs[i].LastObservedGeneration = 0
			}
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

	klog.Infof("Patching addon %s/%s status with %s", new.Namespace, new.Name, string(patchBytes))
	_, err = d.addonClient.AddonV1alpha1().ManagedClusterAddOns(new.Namespace).Patch(
		ctx, new.Name, types.MergePatchType, patchBytes, metav1.PatchOptions{}, "status")
	return err
}
