package controllers

const (
	// ManifestWorkFinalizer is the name of the finalizer added to manifestworks. It is used to ensure
	// all maintained resources of a manifestwork are deleted before the manifestwork itself is deleted
	ManifestWorkFinalizer = "cluster.open-cluster-management.io/manifest-work-cleanup"
)
