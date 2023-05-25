package manifests

import "embed"

//go:embed cluster-manager
var ClusterManagerManifestFiles embed.FS

//go:embed klusterlet/management
//go:embed klusterlet/managed
//go:embed klusterletkube111
var KlusterletManifestFiles embed.FS
