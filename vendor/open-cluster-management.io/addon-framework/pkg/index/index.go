package index

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/util/sets"

	addonv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"

	"open-cluster-management.io/addon-framework/pkg/addonmanager/constants"
)

const (
	ManagedClusterAddonByNamespace              = "managedClusterAddonByNamespace"
	ManagedClusterAddonByName                   = "managedClusterAddonByName"
	ManagedClusterAddonByHostedMode             = "managedClusterAddonByHostedMode"
	ManagedClusterAddonByDeclaredHostingCluster = "managedClusterAddonByDeclaredHostingCluster"
	ManagedClusterByHostingCluster              = "managedClusterByHostingCluster"
	HostedModeIndexKey                          = "Hosted"
)

//nolint:revive
func IndexManagedClusterAddonByNamespace(obj interface{}) ([]string, error) {
	mca, ok := obj.(*addonv1beta1.ManagedClusterAddOn)

	if !ok {
		return []string{}, fmt.Errorf("obj %T is not a ManagedClusterAddon", obj)
	}

	return []string{mca.Namespace}, nil
}

//nolint:revive
func IndexManagedClusterAddonByName(obj interface{}) ([]string, error) {
	mca, ok := obj.(*addonv1beta1.ManagedClusterAddOn)
	if !ok {
		return nil, fmt.Errorf("obj %T is not a ManagedClusterAddon", obj)
	}

	return []string{mca.Name}, nil
}

//nolint:revive
func IndexManagedClusterAddonByHostedMode(obj interface{}) ([]string, error) {
	mca, ok := obj.(*addonv1beta1.ManagedClusterAddOn)
	if !ok {
		return nil, fmt.Errorf("obj %T is not a ManagedClusterAddon", obj)
	}

	if mca.Annotations[addonv1beta1.InstallModeAnnotationKey] != constants.InstallModeHosted {
		return nil, nil
	}

	return []string{HostedModeIndexKey}, nil
}

//nolint:revive
func IndexManagedClusterAddonByDeclaredHostingCluster(obj interface{}) ([]string, error) {
	mca, ok := obj.(*addonv1beta1.ManagedClusterAddOn)
	if !ok {
		return nil, fmt.Errorf("obj %T is not a ManagedClusterAddon", obj)
	}

	hostingClusterName := mca.Annotations[addonv1beta1.HostingClusterNameAnnotationKey]
	if hostingClusterName == "" {
		return nil, nil
	}
	return []string{hostingClusterName}, nil
}

//nolint:revive
func IndexManagedClusterByHostingCluster(obj interface{}) ([]string, error) {
	cluster, ok := obj.(*clusterv1.ManagedCluster)
	if !ok {
		return nil, fmt.Errorf("obj %T is not a ManagedCluster", obj)
	}

	for _, claim := range cluster.Status.ClusterClaims {
		if claim.Name == constants.HostingClusterClaimName && claim.Value != "" {
			return []string{claim.Value}, nil
		}
	}

	return nil, nil
}

const (
	ManifestWorkByAddon           = "manifestWorkByAddon"
	ManifestWorkByHostedAddon     = "manifestWorkByHostedAddon"
	ManifestWorkHookByHostedAddon = "manifestWorkHookByHostedAddon"
)

//nolint:revive
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

//nolint:revive
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

//nolint:revive
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

	addonName, ok := work.Labels[addonv1beta1.AddonLabelKey]
	if !ok {
		return "", "", false
	}

	addonNamespace := work.Labels[addonv1beta1.AddonNamespaceLabelKey]

	isHook := strings.HasPrefix(work.Name, constants.PreDeleteHookWorkName(addonName))

	return addonName, addonNamespace, isHook
}

const (
	AddonByConfig = "addonByConfig"
)

//nolint:revive
func IndexAddonByConfig(obj interface{}) ([]string, error) {
	addon, ok := obj.(*addonv1beta1.ManagedClusterAddOn)
	if !ok {
		return nil, fmt.Errorf("obj is supposed to be a ManagedClusterAddOn, but is %T", obj)
	}

	getIndex := func(config addonv1beta1.ConfigSpecHash, gr addonv1beta1.ConfigGroupResource) string {
		if config.Namespace != "" {
			return fmt.Sprintf("%s/%s/%s/%s", gr.Group, gr.Resource, config.Namespace, config.Name)
		}

		return fmt.Sprintf("%s/%s/%s", gr.Group, gr.Resource, config.Name)
	}

	configNames := []string{}
	for _, configReference := range addon.Status.ConfigReferences {
		if configReference.DesiredConfig == nil || configReference.DesiredConfig.Name == "" {
			// bad config reference, ignore
			continue
		}

		configNames = append(configNames, getIndex(*configReference.DesiredConfig, configReference.ConfigGroupResource))
	}

	return configNames, nil
}

const (
	ClusterManagementAddonByConfig = "clusterManagementAddonByConfig"
)

//nolint:revive
func IndexClusterManagementAddonByConfig(obj interface{}) ([]string, error) {
	cma, ok := obj.(*addonv1beta1.ClusterManagementAddOn)
	if !ok {
		return nil, fmt.Errorf("obj is supposed to be a ClusterManagementAddOn, but is %T", obj)
	}

	getIndex := func(gr addonv1beta1.ConfigGroupResource, configSpecHash addonv1beta1.ConfigSpecHash) string {
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
