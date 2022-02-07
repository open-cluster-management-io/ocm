package constants

const (
	// AddonLabel is the label for addon
	AddonLabel = "open-cluster-management.io/addon-name"

	// ClusterLabel is the label for cluster
	ClusterLabel = "open-cluster-management.io/cluster-name"

	// PreDeleteHookLabel is the label for a hook object
	PreDeleteHookLabel = "open-cluster-management.io/addon-pre-delete"

	// PreDeleteHookFinalizer is the finalizer for an addon which has deployed hook objects
	PreDeleteHookFinalizer = "cluster.open-cluster-management.io/addon-pre-delete"
)
