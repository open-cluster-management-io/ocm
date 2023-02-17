package addonmanagement

import (
	"fmt"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
)

const (
	clusterManagementAddonByPlacement = "clusterManagementAddonByPlacement"
	managedClusterAddonByName         = "managedClusterAddonByName"
)

func indexClusterManagementAddonByPlacement(obj interface{}) ([]string, error) {
	cma, ok := obj.(*addonv1alpha1.ClusterManagementAddOn)

	if !ok {
		return []string{}, fmt.Errorf("obj %T is not a ClusterManagementAddon", obj)
	}

	var keys []string
	if cma.Spec.InstallStrategy == nil || cma.Spec.InstallStrategy.Type == addonv1alpha1.AddonInstallStrategyManual {
		return keys, nil
	}

	for _, placement := range cma.Spec.InstallStrategy.Placements {
		key := fmt.Sprintf("%s/%s", placement.PlacementRef.Namespace, placement.PlacementRef.Name)
		keys = append(keys, key)
	}

	return keys, nil
}

func indexManagedClusterAddonByName(obj interface{}) ([]string, error) {
	mca, ok := obj.(*addonv1alpha1.ManagedClusterAddOn)

	if !ok {
		return []string{}, fmt.Errorf("obj %T is not a ManagedClusterAddon", obj)
	}

	return []string{mca.Name}, nil
}
