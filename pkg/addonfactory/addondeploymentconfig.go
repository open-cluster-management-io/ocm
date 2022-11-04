package addonfactory

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

var AddOnDeploymentConfigGVR = schema.GroupVersionResource{
	Group:    "addon.open-cluster-management.io",
	Version:  "v1alpha1",
	Resource: "addondeploymentconfigs",
}

// AddOnDeloymentConfigToValuesFunc transform the AddOnDeploymentConfig object into Values object
// The transformation logic depends on the defintion of the addon template
type AddOnDeloymentConfigToValuesFunc func(config addonapiv1alpha1.AddOnDeploymentConfig) (Values, error)

// AddOnDeloymentConfigGetter has a method to return a AddOnDeploymentConfig object
type AddOnDeloymentConfigGetter interface {
	Get(ctx context.Context, namespace, name string) (*addonapiv1alpha1.AddOnDeploymentConfig, error)
}

type defaultAddOnDeloymentConfigGetter struct {
	addonClient addonv1alpha1client.Interface
}

func (g *defaultAddOnDeloymentConfigGetter) Get(
	ctx context.Context, namespace, name string) (*addonapiv1alpha1.AddOnDeploymentConfig, error) {
	return g.addonClient.AddonV1alpha1().AddOnDeploymentConfigs(namespace).Get(ctx, name, metav1.GetOptions{})
}

// NewAddOnDeloymentConfigGetter returns a AddOnDeloymentConfigGetter with addon client
func NewAddOnDeloymentConfigGetter(addonClient addonv1alpha1client.Interface) AddOnDeloymentConfigGetter {
	return &defaultAddOnDeloymentConfigGetter{addonClient: addonClient}
}

// GetAddOnDeloymentConfigValues uses AddOnDeloymentConfigGetter to get the AddOnDeloymentConfig object, then
// uses AddOnDeloymentConfigToValuesFunc to transform the AddOnDeloymentConfig object to Values object
// If there are mutiple AddOnDeloymentConfig objects in the AddOn ConfigReferences, the big index object will
// override the one from small index
func GetAddOnDeloymentConfigValues(
	getter AddOnDeloymentConfigGetter, toValuesFuncs ...AddOnDeloymentConfigToValuesFunc) GetValuesFunc {
	return func(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) (Values, error) {
		var lastValues = Values{}
		for _, config := range addon.Status.ConfigReferences {
			if config.ConfigGroupResource.Group != AddOnDeploymentConfigGVR.Group ||
				config.ConfigGroupResource.Resource != AddOnDeploymentConfigGVR.Resource {
				continue
			}

			addOnDeloymentConfig, err := getter.Get(context.Background(), config.Namespace, config.Name)
			if err != nil {
				return nil, err
			}

			for _, toValuesFunc := range toValuesFuncs {
				values, err := toValuesFunc(*addOnDeloymentConfig)
				if err != nil {
					return nil, err
				}
				lastValues = MergeValues(lastValues, values)
			}
		}

		return lastValues, nil
	}
}

// ToAddOnDeloymentConfigValues transform the AddOnDeploymentConfig object into Values object that is a plain value map
// for example: the spec of one AddOnDeploymentConfig is:
// {
//	customizedVariables: [{name: "Image", value: "img"}, {name: "ImagePullPolicy", value: "Always"}],
//  nodePlacement: {nodeSelector: {"host": "ssd"}, tolerations: {"key": "test"}},
// }
// after transformed, the key set of Values object will be: {"Image", "ImagePullPolicy", "NodeSelector", "Tolerations"}
func ToAddOnDeloymentConfigValues(config addonapiv1alpha1.AddOnDeploymentConfig) (Values, error) {
	values, err := ToAddOnCustomizedVariableValues(config)
	if err != nil {
		return nil, err
	}

	if config.Spec.NodePlacement != nil {
		values["NodeSelector"] = config.Spec.NodePlacement.NodeSelector
		values["Tolerations"] = config.Spec.NodePlacement.Tolerations
	}

	return values, nil
}

// ToAddOnNodePlacementValues only transform the AddOnDeploymentConfig NodePlacement part into Values object that has
// a specific for helm chart values
// for example: the spec of one AddOnDeploymentConfig is:
// {
//  nodePlacement: {nodeSelector: {"host": "ssd"}, tolerations: {"key":"test"}},
// }
// after transformed, the Values will be:
// map[global:map[nodeSelector:map[host:ssd]] tolerations:[map[key:test]]]
func ToAddOnNodePlacementValues(config addonapiv1alpha1.AddOnDeploymentConfig) (Values, error) {
	if config.Spec.NodePlacement == nil {
		return nil, nil
	}

	type global struct {
		NodeSelector map[string]string `json:"nodeSelector"`
	}

	jsonStruct := struct {
		Tolerations []corev1.Toleration `json:"tolerations"`
		Global      global              `json:"global"`
	}{
		Tolerations: config.Spec.NodePlacement.Tolerations,
		Global: global{
			NodeSelector: config.Spec.NodePlacement.NodeSelector,
		},
	}

	values, err := JsonStructToValues(jsonStruct)
	if err != nil {
		return nil, err
	}

	return values, nil
}

// ToAddOnCustomizedVariables only transform the CustomizedVariables in the spec of AddOnDeploymentConfig into Values object.
// for example: the spec of one AddOnDeploymentConfig is:
// {
//  customizedVariables: [{name: "a", value: "x"}, {name: "b", value: "y"}],
// }
// after transformed, the Values will be:
// map[a:x b:y]
func ToAddOnCustomizedVariableValues(config addonapiv1alpha1.AddOnDeploymentConfig) (Values, error) {
	values := Values{}
	for _, variable := range config.Spec.CustomizedVariables {
		values[variable.Name] = variable.Value
	}

	return values, nil
}
