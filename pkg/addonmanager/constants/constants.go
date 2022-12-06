package constants

import "fmt"

const (
	// AddonLabel is the label for addon
	AddonLabel = "open-cluster-management.io/addon-name"
	// AddonNamespaceLabel is the label for addon namespace
	AddonNamespaceLabel = "open-cluster-management.io/addon-namespace"

	// ClusterLabel is the label for cluster
	ClusterLabel = "open-cluster-management.io/cluster-name"

	// PreDeleteHookLabel is the label for a hook object
	PreDeleteHookLabel = "open-cluster-management.io/addon-pre-delete"

	// PreDeleteHookFinalizer is the finalizer for an addon which has deployed hook objects
	PreDeleteHookFinalizer = "cluster.open-cluster-management.io/addon-pre-delete"

	// HostingPreDeleteHookFinalizer is the finalizer for an addon which has deployed hook objects on hosting cluster
	HostingPreDeleteHookFinalizer = "cluster.open-cluster-management.io/hosting-addon-pre-delete"

	// AddonManifestApplied is a condition type representing whether the manifest of an addon
	// is applied correctly.
	AddonManifestApplied = "ManifestApplied"

	// AddonManifestAppliedReasonWorkApplyFailed is the reason of condition AddonManifestApplied indicating
	// the failuer of apply manifestwork of the manifests
	AddonManifestAppliedReasonWorkApplyFailed = "ManifestWorkApplyFailed"

	// AddonManifestAppliedReasonManifestsApplied is the reason of condition AddonManifestApplied indicating
	// the manifests is applied on the managedcluster.
	AddonManifestAppliedReasonManifestsApplied = "AddonManifestApplied"

	// AddonManifestAppliedReasonManifestsApplyFailed is the reason of condition AddonManifestApplied indicating
	// the failure to apply manifests on the managedcluster
	AddonManifestAppliedReasonManifestsApplyFailed = "AddonManifestAppliedFailed"

	// AddonHookManifestCompleted is a condition type representing whether the addon hook is completed.
	AddonHookManifestCompleted = "HookManifestCompleted"

	// InstallModeBuiltinValueKey is the key of the build in value to represent the addon install mode, addon developers
	// can use this built in value in manifests.
	InstallModeBuiltinValueKey = "InstallMode"
	InstallModeHosted          = "Hosted"
	InstallModeDefault         = "Default"

	// HostingClusterNameAnnotationKey is the annotation key for indicating the hosting cluster name
	HostingClusterNameAnnotationKey = "addon.open-cluster-management.io/hosting-cluster-name"

	// HostedManifestLocationLabelKey is the label key for indicating where the manifest should be deployed in Hosted
	// mode
	HostedManifestLocationLabelKey = "addon.open-cluster-management.io/hosted-manifest-location"

	// HostedManifestLocationManagedLabelValue indicates the manifest will be deployed on the managed cluster in Hosted
	// mode, it is the default value of a manifest in Hosted mode
	HostedManifestLocationManagedLabelValue = "managed"
	// HostedManifestLocationHostingLabelValue indicates the manifest will be deployed on the hosting cluster in Hosted
	// mode
	HostedManifestLocationHostingLabelValue = "hosting"
	// HostedManifestLocationNoneLabelValue indicates the manifest will not be deployed in Hosted mode
	HostedManifestLocationNoneLabelValue = "none"

	// HostingManifestFinalizer is the finalizer for an addon which has deployed manifests on the external
	// hosting cluster in Hosted mode
	HostingManifestFinalizer = "cluster.open-cluster-management.io/hosting-manifests-cleanup"

	// AddonHostingManifestApplied is a condition type representing whether the manifest of an addon
	// is applied on the hosting cluster correctly.
	AddonHostingManifestApplied = "HostingManifestApplied"

	// HostingClusterValid is a condition type representing whether the hosting cluster is valid in Hosted mode
	HostingClusterValidity = "HostingClusterValidity"

	// HostingClusterValidityReasonValid is the reason of condition HostingClusterValidity indicating the hosting
	// cluster is valid
	HostingClusterValidityReasonValid = "HostingClusterValid"

	// HostingClusterValidityReasonInvalid is the reason of condition HostingClusterValidity indicating the hosting
	// cluster is invalid
	HostingClusterValidityReasonInvalid = "HostingClusterInvalid"

	// DisableAddonAutomaticInstallationAnnotationKey is the annotation key for disabling the functionality of
	// installing addon automatically
	DisableAddonAutomaticInstallationAnnotationKey = "addon.open-cluster-management.io/disable-automatic-installation"

	// AnnotationDeletionOrphan is an annotation for the manifest which will not be cleaned up
	// after the addon manifestWork is deleted.
	AnnotationDeletionOrphan = "addon.open-cluster-management.io/deletion-orphan"
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
func GetHostedModeInfo(annotations map[string]string) (string, string) {
	hostingClusterName, ok := annotations[HostingClusterNameAnnotationKey]
	if !ok {
		return InstallModeDefault, ""
	}

	return InstallModeHosted, hostingClusterName
}

// GetHostedManifestLocation returns the location of the manifest in Hosted mode, if it is invalid will return error
func GetHostedManifestLocation(labels map[string]string) (string, bool, error) {
	manifestLocation, ok := labels[HostedManifestLocationLabelKey]
	if !ok {
		return "", false, nil
	}

	switch manifestLocation {
	case HostedManifestLocationManagedLabelValue,
		HostedManifestLocationHostingLabelValue,
		HostedManifestLocationNoneLabelValue:
		return manifestLocation, true, nil
	default:
		return "", true, fmt.Errorf("not supported manifest location: %s", manifestLocation)
	}
}
