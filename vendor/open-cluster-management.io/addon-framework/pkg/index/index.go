package index

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/util/sets"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	workapiv1 "open-cluster-management.io/api/work/v1"

	"open-cluster-management.io/addon-framework/pkg/addonmanager/constants"
)

const (
	ManagedClusterAddonByNamespace = "managedClusterAddonByNamespace"
)

func IndexManagedClusterAddonByNamespace(obj interface{}) ([]string, error) {
	mca, ok := obj.(*addonv1alpha1.ManagedClusterAddOn)

	if !ok {
		return []string{}, fmt.Errorf("obj %T is not a ManagedClusterAddon", obj)
	}

	return []string{mca.Namespace}, nil
}

const (
	ManifestWorkByAddon           = "manifestWorkByAddon"
	ManifestWorkByHostedAddon     = "manifestWorkByHostedAddon"
	ManifestWorkHookByHostedAddon = "manifestWorkHookByHostedAddon"
)

func IndexManifestWorkByAddon(obj interface{}) ([]string, error) {
	work, ok := obj.(*workapiv1.ManifestWork)
	if !ok {
		return []string{}, fmt.Errorf("obj is supposed to be a ManifestWork, but is %T", obj)
	}

	addonName, addonNamespace, isHook := extractAddonFromWork(work)

	if len(addonName) == 0 || len(addonNamespace) > 0 || isHook {
		return []string{}, nil
	}

	return []string{fmt.Sprintf("%s/%s", work.Namespace, addonName)}, nil
}

func IndexManifestWorkByHostedAddon(obj interface{}) ([]string, error) {
	work, ok := obj.(*workapiv1.ManifestWork)
	if !ok {
		return []string{}, fmt.Errorf("obj is supposed to be a ManifestWork, but is %T", obj)
	}

	addonName, addonNamespace, isHook := extractAddonFromWork(work)

	if len(addonName) == 0 || len(addonNamespace) == 0 || isHook {
		return []string{}, nil
	}

	return []string{fmt.Sprintf("%s/%s", addonNamespace, addonName)}, nil
}

func IndexManifestWorkHookByHostedAddon(obj interface{}) ([]string, error) {
	work, ok := obj.(*workapiv1.ManifestWork)
	if !ok {
		return []string{}, fmt.Errorf("obj is supposed to be a ManifestWork, but is %T", obj)
	}

	addonName, addonNamespace, isHook := extractAddonFromWork(work)

	if len(addonName) == 0 || len(addonNamespace) == 0 || !isHook {
		return []string{}, nil
	}

	return []string{fmt.Sprintf("%s/%s", addonNamespace, addonName)}, nil
}

func extractAddonFromWork(work *workapiv1.ManifestWork) (string, string, bool) {
	if len(work.Labels) == 0 {
		return "", "", false
	}

	addonName, ok := work.Labels[addonv1alpha1.AddonLabelKey]
	if !ok {
		return "", "", false
	}

	addonNamespace := work.Labels[addonv1alpha1.AddonNamespaceLabelKey]

	isHook := false
	if strings.HasPrefix(work.Name, constants.PreDeleteHookWorkName(addonName)) {
		isHook = true
	}

	return addonName, addonNamespace, isHook
}

const (
	AddonByConfig = "addonByConfig"
)

func IndexAddonByConfig(obj interface{}) ([]string, error) {
	addon, ok := obj.(*addonv1alpha1.ManagedClusterAddOn)
	if !ok {
		return nil, fmt.Errorf("obj is supposed to be a ManagedClusterAddOn, but is %T", obj)
	}

	getIndex := func(config addonv1alpha1.ConfigReference) string {
		if config.Namespace != "" {
			return fmt.Sprintf("%s/%s/%s/%s", config.Group, config.Resource, config.Namespace, config.Name)
		}

		return fmt.Sprintf("%s/%s/%s", config.Group, config.Resource, config.Name)
	}

	configNames := []string{}
	for _, configReference := range addon.Status.ConfigReferences {
		if configReference.Name == "" {
			// bad config reference, ignore
			continue
		}

		configNames = append(configNames, getIndex(configReference))
	}

	return configNames, nil
}

const (
	ClusterManagementAddonByConfig = "clusterManagementAddonByConfig"
)

func IndexClusterManagementAddonByConfig(obj interface{}) ([]string, error) {
	cma, ok := obj.(*addonv1alpha1.ClusterManagementAddOn)
	if !ok {
		return nil, fmt.Errorf("obj is supposed to be a ClusterManagementAddOn, but is %T", obj)
	}

	getIndex := func(gr addonv1alpha1.ConfigGroupResource, configSpecHash addonv1alpha1.ConfigSpecHash) string {
		if configSpecHash.Namespace != "" {
			return fmt.Sprintf("%s/%s/%s/%s", gr.Group, gr.Resource, configSpecHash.Namespace, configSpecHash.Name)
		}

		return fmt.Sprintf("%s/%s/%s", gr.Group, gr.Resource, configSpecHash.Name)
	}

	configNames := sets.New[string]()
	for _, defaultConfigRef := range cma.Status.DefaultConfigReferences {
		if defaultConfigRef.DesiredConfig == nil || defaultConfigRef.DesiredConfig.Name == "" {
			// bad config reference, ignore
			continue
		}

		configNames.Insert(getIndex(defaultConfigRef.ConfigGroupResource, *defaultConfigRef.DesiredConfig))
	}

	for _, installProgression := range cma.Status.InstallProgressions {
		for _, configReference := range installProgression.ConfigReferences {
			if configReference.DesiredConfig == nil || configReference.DesiredConfig.Name == "" {
				// bad config reference, ignore
				continue
			}

			configNames.Insert(getIndex(configReference.ConfigGroupResource, *configReference.DesiredConfig))
		}
	}

	return configNames.UnsortedList(), nil
}
