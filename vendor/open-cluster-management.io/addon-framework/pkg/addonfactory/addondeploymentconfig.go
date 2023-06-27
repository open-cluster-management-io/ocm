package addonfactory

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

	"open-cluster-management.io/addon-framework/pkg/utils"
)

// AddOnDeloymentConfigToValuesFunc transform the AddOnDeploymentConfig object into Values object
// The transformation logic depends on the definition of the addon template
// Deprecated: use AddOnDeploymentConfigToValuesFunc instead.
type AddOnDeloymentConfigToValuesFunc func(config addonapiv1alpha1.AddOnDeploymentConfig) (Values, error)

// AddOnDeloymentConfigGetter has a method to return a AddOnDeploymentConfig object
// Deprecated: use AddOnDeploymentConfigGetter instead.
type AddOnDeloymentConfigGetter interface {
	Get(ctx context.Context, namespace, name string) (*addonapiv1alpha1.AddOnDeploymentConfig, error)
}

type defaultAddOnDeploymentConfigGetter struct {
	addonClient addonv1alpha1client.Interface
}

func (g *defaultAddOnDeploymentConfigGetter) Get(
	ctx context.Context, namespace, name string) (*addonapiv1alpha1.AddOnDeploymentConfig, error) {
	return g.addonClient.AddonV1alpha1().AddOnDeploymentConfigs(namespace).Get(ctx, name, metav1.GetOptions{})
}

// NewAddOnDeloymentConfigGetter returns a AddOnDeloymentConfigGetter with addon client
// Deprecated: use NewAddOnDeploymentConfigGetter instead.
func NewAddOnDeloymentConfigGetter(addonClient addonv1alpha1client.Interface) AddOnDeloymentConfigGetter {
	return &defaultAddOnDeploymentConfigGetter{addonClient: addonClient}
}

// GetAddOnDeloymentConfigValues uses AddOnDeloymentConfigGetter to get the AddOnDeploymentConfig object, then
// uses AddOnDeloymentConfigToValuesFunc to transform the AddOnDeploymentConfig object to Values object
// If there are multiple AddOnDeploymentConfig objects in the AddOn ConfigReferences, the big index object will
// override the one from small index
// Deprecated: use GetAddOnDeploymentConfigValues instead.
func GetAddOnDeloymentConfigValues(
	getter AddOnDeloymentConfigGetter, toValuesFuncs ...AddOnDeloymentConfigToValuesFunc) GetValuesFunc {
	return func(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) (Values, error) {
		var lastValues = Values{}
		for _, config := range addon.Status.ConfigReferences {
			if config.ConfigGroupResource.Group != utils.AddOnDeploymentConfigGVR.Group ||
				config.ConfigGroupResource.Resource != utils.AddOnDeploymentConfigGVR.Resource {
				continue
			}

			addOnDeploymentConfig, err := getter.Get(context.Background(), config.Namespace, config.Name)
			if err != nil {
				return nil, err
			}

			for _, toValuesFunc := range toValuesFuncs {
				values, err := toValuesFunc(*addOnDeploymentConfig)
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
//
//	{
//		customizedVariables: [{name: "Image", value: "img"}, {name: "ImagePullPolicy", value: "Always"}],
//		nodePlacement: {nodeSelector: {"host": "ssd"}, tolerations: {"key": "test"}},
//	}
//
// after transformed, the key set of Values object will be: {"Image", "ImagePullPolicy", "NodeSelector", "Tolerations"}
// Deprecated: use ToAddOnDeploymentConfigValues instead.
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
//
//	{
//	 nodePlacement: {nodeSelector: {"host": "ssd"}, tolerations: {"key":"test"}},
//	}
//
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

// ToAddOnCustomizedVariableValues only transform the CustomizedVariables in the spec of AddOnDeploymentConfig into Values object.
// for example: the spec of one AddOnDeploymentConfig is:
//
//	{
//	 customizedVariables: [{name: "a", value: "x"}, {name: "b", value: "y"}],
//	}
//
// after transformed, the Values will be:
// map[a:x b:y]
func ToAddOnCustomizedVariableValues(config addonapiv1alpha1.AddOnDeploymentConfig) (Values, error) {
	values := Values{}
	for _, variable := range config.Spec.CustomizedVariables {
		values[variable.Name] = variable.Value
	}

	return values, nil
}

// AddOnDeploymentConfigToValuesFunc transform the AddOnDeploymentConfig object into Values object
// The transformation logic depends on the definition of the addon template
type AddOnDeploymentConfigToValuesFunc func(config addonapiv1alpha1.AddOnDeploymentConfig) (Values, error)

// AddOnDeploymentConfigGetter has a method to return a AddOnDeploymentConfig object
type AddOnDeploymentConfigGetter interface {
	Get(ctx context.Context, namespace, name string) (*addonapiv1alpha1.AddOnDeploymentConfig, error)
}

// NewAddOnDeploymentConfigGetter returns a AddOnDeploymentConfigGetter with addon client
func NewAddOnDeploymentConfigGetter(addonClient addonv1alpha1client.Interface) AddOnDeploymentConfigGetter {
	return &defaultAddOnDeploymentConfigGetter{addonClient: addonClient}
}

// GetAddOnDeploymentConfigValues uses AddOnDeploymentConfigGetter to get the AddOnDeploymentConfig object, then
// uses AddOnDeploymentConfigToValuesFunc to transform the AddOnDeploymentConfig object to Values object
// If there are multiple AddOnDeploymentConfig objects in the AddOn ConfigReferences, the big index object will
// override the one from small index
func GetAddOnDeploymentConfigValues(
	getter AddOnDeploymentConfigGetter, toValuesFuncs ...AddOnDeploymentConfigToValuesFunc) GetValuesFunc {
	return func(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) (Values, error) {
		var lastValues = Values{}
		for _, config := range addon.Status.ConfigReferences {
			if config.ConfigGroupResource.Group != utils.AddOnDeploymentConfigGVR.Group ||
				config.ConfigGroupResource.Resource != utils.AddOnDeploymentConfigGVR.Resource {
				continue
			}

			addOnDeploymentConfig, err := getter.Get(context.Background(), config.Namespace, config.Name)
			if err != nil {
				return nil, err
			}

			for _, toValuesFunc := range toValuesFuncs {
				values, err := toValuesFunc(*addOnDeploymentConfig)
				if err != nil {
					return nil, err
				}
				lastValues = MergeValues(lastValues, values)
			}
		}

		return lastValues, nil
	}
}

// ToAddOnDeploymentConfigValues transform the AddOnDeploymentConfig object into Values object that is a plain value map
// for example: the spec of one AddOnDeploymentConfig is:
//
//	{
//		customizedVariables: [{name: "Image", value: "img"}, {name: "ImagePullPolicy", value: "Always"}],
//		nodePlacement: {nodeSelector: {"host": "ssd"}, tolerations: {"key": "test"}},
//	}
//
// after transformed, the key set of Values object will be: {"Image", "ImagePullPolicy", "NodeSelector", "Tolerations"}
func ToAddOnDeploymentConfigValues(config addonapiv1alpha1.AddOnDeploymentConfig) (Values, error) {
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

// ToImageOverrideValuesFunc return a func that can use the AddOnDeploymentConfig.spec.Registries to override image,
// then return the overridden value with key imageKey.
//
// for example: the spec of one AddOnDeploymentConfig is:
// { registries: [{source: "quay.io/open-cluster-management/addon-agent", mirror: "quay.io/ocm/addon-agent"}]}
// the imageKey is "helloWorldImage", the image is "quay.io/open-cluster-management/addon-agent:v1"
// after transformed, the Values object will be: {"helloWorldImage": "quay.io/ocm/addon-agent:v1"}
//
// Note: the imageKey can support the nested key, for example: "global.imageOverrides.helloWorldImage", the output
// will be: {"global": {"imageOverrides": {"helloWorldImage": "quay.io/ocm/addon-agent:v1"}}}
func ToImageOverrideValuesFunc(imageKey, image string) AddOnDeploymentConfigToValuesFunc {
	return func(config addonapiv1alpha1.AddOnDeploymentConfig) (Values, error) {
		if len(imageKey) == 0 {
			return nil, fmt.Errorf("imageKey is empty")
		}
		if len(image) == 0 {
			return nil, fmt.Errorf("image is empty")
		}

		nestedMap := make(map[string]interface{})

		keys := strings.Split(imageKey, ".")
		currentMap := nestedMap

		for i := 0; i < len(keys)-1; i++ {
			key := keys[i]
			nextMap := make(map[string]interface{})
			currentMap[key] = nextMap
			currentMap = nextMap
		}

		lastKey := keys[len(keys)-1]
		currentMap[lastKey] = image

		if config.Spec.Registries != nil {
			currentMap[lastKey] = OverrideImage(config.Spec.Registries, image)
		}

		return nestedMap, nil
	}
}

// OverrideImage checks whether the source configured in registries can match the imagedName, if yes will use the
// mirror value in the registries to override the imageName
func OverrideImage(registries []addonapiv1alpha1.ImageMirror, imageName string) string {
	if len(registries) == 0 {
		return imageName
	}
	overrideImageName := imageName
	for i := 0; i < len(registries); i++ {
		registry := registries[i]
		name := overrideImageDirectly(registry.Source, registry.Mirror, imageName)
		if name != imageName {
			overrideImageName = name
		}
	}
	return overrideImageName
}

func overrideImageDirectly(source, mirror, imageName string) string {
	source = strings.TrimSuffix(source, "/")
	mirror = strings.TrimSuffix(mirror, "/")
	imageSegments := strings.Split(imageName, "/")
	imageNameTag := imageSegments[len(imageSegments)-1]
	if source == "" {
		if mirror == "" {
			return imageNameTag
		}
		return fmt.Sprintf("%s/%s", mirror, imageNameTag)
	}

	if !strings.HasPrefix(imageName, source) {
		return imageName
	}

	trimSegment := strings.TrimPrefix(imageName, source)
	return fmt.Sprintf("%s%s", mirror, trimSegment)
}
