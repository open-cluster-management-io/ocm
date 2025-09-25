package utils

import (
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
)

// ManagedClusterAddOnFilterFunc is a function type that filters ManagedClusterAddOn objects.
// It returns true if the ManagedClusterAddOn should be processed, false otherwise.
// This is used to selectively process only certain types of addons based on custom criteria.
type ManagedClusterAddOnFilterFunc func(mca *addonapiv1alpha1.ManagedClusterAddOn) bool

// AllowAllAddOns is a filter function that accepts all ManagedClusterAddOn objects.
// This function always returns true, making it suitable as a no-op filter when
// no filtering is desired.
func AllowAllAddOns(mca *addonapiv1alpha1.ManagedClusterAddOn) bool {
	return true
}

// FilterTemplateBasedAddOns is a filter function that only accepts ManagedClusterAddOn
// objects that are based on AddOnTemplate resources. It checks the status.configReferences
// to determine if any configuration reference points to an addontemplates resource.
func FilterTemplateBasedAddOns(mca *addonapiv1alpha1.ManagedClusterAddOn) bool {
	if mca == nil {
		return false
	}

	// Check if any of the config references is an addontemplates resource
	for _, configRef := range mca.Status.ConfigReferences {
		if configRef.Group == "addon.open-cluster-management.io" && configRef.Resource == "addontemplates" &&
			configRef.DesiredConfig != nil && len(configRef.DesiredConfig.SpecHash) > 0 {
			return true

		}
	}

	return false
}
