package manifests

import "embed"

//go:embed cluster-manager
var ClusterManagerManifestFiles embed.FS

//go:embed klusterlet
var KlusterletManifestFiles embed.FS

//go:embed klusterletkube111
var Klusterlet111ManifestFiles embed.FS
