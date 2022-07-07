package constants

import "fmt"

const (
	// AddonLabel is the label for addon
	AddonLabel = "open-cluster-management.io/addon-name"

	// ClusterLabel is the label for cluster
	ClusterLabel = "open-cluster-management.io/cluster-name"

	// PreDeleteHookLabel is the label for a hook object
	PreDeleteHookLabel = "open-cluster-management.io/addon-pre-delete"

	// PreDeleteHookFinalizer is the finalizer for an addon which has deployed hook objects
	PreDeleteHookFinalizer = "cluster.open-cluster-management.io/addon-pre-delete"

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
)

// DeployWorkName return the name of work for the addon
func DeployWorkName(addonName string) string {
	return fmt.Sprintf("addon-%s-deploy", addonName)
}
