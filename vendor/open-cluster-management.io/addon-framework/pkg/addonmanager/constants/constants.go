package constants

import (
	"fmt"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

const (
	// InstallModeBuiltinValueKey is the key of the build in value to represent the addon install mode, addon developers
	// can use this built in value in manifests.
	InstallModeBuiltinValueKey = "InstallMode"
	InstallModeHosted          = "Hosted"
	InstallModeDefault         = "Default"
)

// DeployWorkNamePrefix returns the prefix of the work name for the addon
func DeployWorkNamePrefix(addonName string) string {
	return fmt.Sprintf("addon-%s-deploy", addonName)
}

// DeployHostingWorkNamePrefix returns the prefix of the work name on hosting cluster for the addon
func DeployHostingWorkNamePrefix(addonNamespace, addonName string) string {
	return fmt.Sprintf("%s-hosting-%s", DeployWorkNamePrefix(addonName), addonNamespace)
}

// PreDeleteHookWorkName return the name of pre-delete work for the addon
func PreDeleteHookWorkName(addonName string) string {
	return fmt.Sprintf("addon-%s-pre-delete", addonName)
}

// PreDeleteHookHostingWorkName return the name of pre-delete work on hosting cluster for the addon
func PreDeleteHookHostingWorkName(addonNamespace, addonName string) string {
	return fmt.Sprintf("%s-hosting-%s", PreDeleteHookWorkName(addonName), addonNamespace)
}

// GetHostedModeInfo returns addon installation mode and hosting cluster name.
func GetHostedModeInfo(addon *addonv1alpha1.ManagedClusterAddOn, _ *clusterv1.ManagedCluster) (string, string) {
	if len(addon.Annotations) == 0 {
		return InstallModeDefault, ""
	}
	hostingClusterName, ok := addon.Annotations[addonv1alpha1.HostingClusterNameAnnotationKey]
	if !ok {
		return InstallModeDefault, ""
	}

	return InstallModeHosted, hostingClusterName
}

// GetHostedManifestLocation returns the location of the manifest in Hosted mode, if it is invalid will return error
func GetHostedManifestLocation(labels, annotations map[string]string) (string, bool, error) {
	manifestLocation := annotations[addonv1alpha1.HostedManifestLocationAnnotationKey]

	// TODO: deprecate HostedManifestLocationLabelKey in the future release
	if manifestLocation == "" {
		manifestLocation = labels[addonv1alpha1.HostedManifestLocationLabelKey]
	}

	switch manifestLocation {
	case addonv1alpha1.HostedManifestLocationManagedValue,
		addonv1alpha1.HostedManifestLocationHostingValue,
		addonv1alpha1.HostedManifestLocationNoneValue:
		return manifestLocation, true, nil
	case "":
		return "", false, nil
	default:
		return "", true, fmt.Errorf("not supported manifest location: %s", manifestLocation)
	}
}
