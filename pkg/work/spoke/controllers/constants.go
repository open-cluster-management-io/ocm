package controllers

const (
	// ManifestWorkFinalizer is the name of the finalizer added to manifestworks. It is used to ensure
	// related appliedmanifestwork of a manifestwork are deleted before the manifestwork itself is deleted
	ManifestWorkFinalizer = "cluster.open-cluster-management.io/manifest-work-cleanup"
	// AppliedManifestWorkFinalizer is the name of the finalizer added to appliedmanifestwork. It is to
	// ensure all resource relates to appliedmanifestwork is deleted before appliedmanifestwork itself
	// is deleted.
	AppliedManifestWorkFinalizer = "cluster.open-cluster-management.io/applied-manifest-work-cleanup"
)
