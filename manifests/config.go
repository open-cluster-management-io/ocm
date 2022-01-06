package manifests

type HubConfig struct {
	ClusterManagerName             string
	ClusterManagerNamespace        string
	RegistrationImage              string
	RegistrationAPIServiceCABundle string
	WorkImage                      string
	WorkAPIServiceCABundle         string
	PlacementImage                 string
	Replica                        int32
}
