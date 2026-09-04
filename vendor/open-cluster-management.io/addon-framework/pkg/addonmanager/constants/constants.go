package constants

import (
	"fmt"

	addonv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

const (
	// InstallModeBuiltinValueKey is the key of the build in value to represent the addon install mode, addon developers
	// can use this built in value in manifests.
	InstallModeBuiltinValueKey = "InstallMode"
	InstallModeHosted          = "Hosted"
	InstallModeDefault         = "Default"
	// HostingClusterClaimName is the reserved ClusterClaim used to self-report a cluster's host.
	HostingClusterClaimName = "hosting-cluster.open-cluster-management.io"
	// HostedModeAutoDiscoveryAnnotationKey is a fallback for hubs that don't round-trip
	// spec.hostedModeAutoDiscovery yet (see autoDiscoveryEnabled).
	HostedModeAutoDiscoveryAnnotationKey = "addon.open-cluster-management.io/hosted-mode-auto-discovery"
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

// GetHostedModeInfo returns addon installation mode and hosting cluster name. Hosted mode is
// selected by a resolved hosting cluster. The install-mode annotation is handled by the
// auto-discovery resolver and does not opt addon implementations into Hosted mode by itself.
func GetHostedModeInfo(addon *addonv1beta1.ManagedClusterAddOn, _ *clusterv1.ManagedCluster) (string, string) {
	if len(addon.Annotations) == 0 {
		return InstallModeDefault, ""
	}
	if hostingClusterName, ok := addon.Annotations[addonv1beta1.HostingClusterNameAnnotationKey]; ok {
		return InstallModeHosted, hostingClusterName
	}
	return InstallModeDefault, ""
}

// GetHostedManifestLocation returns the location of the manifest in Hosted mode, if it is invalid will return error
func GetHostedManifestLocation(labels, annotations map[string]string) (string, bool, error) {
	manifestLocation := annotations[addonv1beta1.HostedManifestLocationAnnotationKey]

	// TODO: deprecate HostedManifestLocationLabelKey in the future release
	if manifestLocation == "" {
		manifestLocation = labels[addonv1beta1.HostedManifestLocationAnnotationKey]
	}

	switch manifestLocation {
	case addonv1beta1.HostedManifestLocationManagedValue,
		addonv1beta1.HostedManifestLocationHostingValue,
		addonv1beta1.HostedManifestLocationNoneValue:
		return manifestLocation, true, nil
	case "":
		return "", false, nil
	default:
		return "", true, fmt.Errorf("not supported manifest location: %s", manifestLocation)
	}
}
