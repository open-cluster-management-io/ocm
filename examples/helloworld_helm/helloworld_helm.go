package helloworld_helm

import (
	"embed"

	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

const defaultExampleImage = "quay.io/open-cluster-management/helloworld-addon:latest"

//go:embed manifests
//go:embed manifests/charts/helloworld
//go:embed manifests/charts/helloworld/templates/_helpers.tpl
var FS embed.FS

const (
	AddonName = "helloworldhelm"
)

type global struct {
	ImagePullPolicy string            `json:"imagePullPolicy"`
	ImagePullSecret string            `json:"imagePullSecret"`
	ImageOverrides  map[string]string `json:"imageOverrides"`
	NodeSelector    map[string]string `json:"nodeSelector"`
	ProxyConfig     map[string]string `json:"proxyConfig"`
}
type userValues struct {
	ClusterNamespace string `json:"clusterNamespace"`
	Global           global `json:"global"`
}

func GetValues(cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn) (addonfactory.Values, error) {
	userJsonValues := userValues{
		ClusterNamespace: cluster.GetName(),
		Global: global{
			ImagePullPolicy: "IfNotPresent",
			ImageOverrides: map[string]string{
				"helloWorldHelm": defaultExampleImage,
			},
		},
	}
	values, err := addonfactory.JsonStructToValues(userJsonValues)
	if err != nil {
		return nil, err
	}
	return values, nil
}
