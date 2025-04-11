package manifests

import "embed"

//go:embed cluster-manager
var ClusterManagerManifestFiles embed.FS

//go:embed managed-cluster-policy
var ManagedClusterPolicyManifestFiles embed.FS

//go:embed klusterlet/management
//go:embed klusterlet/managed
var KlusterletManifestFiles embed.FS
