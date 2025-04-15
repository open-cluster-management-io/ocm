package templateagent

import (
	"encoding/json"
	"fmt"
	"sort"

	"k8s.io/klog/v2"

	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

// ToAddOnNodePlacementPrivateValues only transform the AddOnDeploymentConfig NodePlacement part into Values object
// with a specific key, this value would be used by the addon template controller
func ToAddOnNodePlacementPrivateValues(config addonapiv1alpha1.AddOnDeploymentConfig) (addonfactory.Values, error) {
	if config.Spec.NodePlacement == nil {
		return nil, nil
	}

	return addonfactory.Values{
		NodePlacementPrivateValueKey: config.Spec.NodePlacement,
	}, nil
}

// ToAddOnRegistriesPrivateValues only transform the AddOnDeploymentConfig Registries part into Values object
// with a specific key, this value would be used by the addon template controller
func ToAddOnRegistriesPrivateValues(config addonapiv1alpha1.AddOnDeploymentConfig) (addonfactory.Values, error) {
	if config.Spec.Registries == nil {
		return nil, nil
	}

	return addonfactory.Values{
		RegistriesPrivateValueKey: config.Spec.Registries,
	}, nil
}

func ToAddOnInstallNamespacePrivateValues(config addonapiv1alpha1.AddOnDeploymentConfig) (addonfactory.Values, error) {
	if len(config.Spec.AgentInstallNamespace) == 0 {
		return nil, nil
	}
	return addonfactory.Values{
		InstallNamespacePrivateValueKey: config.Spec.AgentInstallNamespace,
	}, nil
}

func ToAddOnProxyPrivateValues(config addonapiv1alpha1.AddOnDeploymentConfig) (addonfactory.Values, error) {
	proxyConfig := config.Spec.ProxyConfig
	if len(proxyConfig.HTTPProxy) == 0 &&
		len(proxyConfig.HTTPSProxy) == 0 &&
		len(proxyConfig.NoProxy) == 0 &&
		len(proxyConfig.CABundle) == 0 {
		return nil, nil
	}
	return addonfactory.Values{
		ProxyPrivateValueKey: proxyConfig,
	}, nil
}

func ToAddOnResourceRequirementsPrivateValues(config addonapiv1alpha1.AddOnDeploymentConfig) (addonfactory.Values, error) {
	if config.Spec.ResourceRequirements == nil {
		return nil, nil
	}
	requirements, err := addonfactory.GetRegexResourceRequirements(config.Spec.ResourceRequirements)
	if err != nil {
		return nil, err
	}
	return addonfactory.Values{
		ResourceRequirementsPrivateValueKey: requirements,
	}, nil
}

type keyValuePair struct {
	name  string
	value string
}

type orderedValues []keyValuePair

func (a *CRDTemplateAgentAddon) getValues(
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn,
	template *addonapiv1alpha1.AddOnTemplate,
) (orderedValues, map[string]interface{}, map[string]interface{}, error) {

	// preset values are combined by default values and builtin values
	presetValues := make([]keyValuePair, 0)
	// override values are values that users can use in the template
	overrideValues := map[string]interface{}{}
	// private values are values that are not exposed to users
	privateValues := map[string]interface{}{}

	defaultSortedKeys, defaultValues, err := a.getDefaultValues(cluster, addon, template)
	if err != nil {
		return presetValues, overrideValues, privateValues, err
	}
	overrideValues = addonfactory.MergeValues(defaultValues, overrideValues)

	for i := 0; i < len(a.getValuesFuncs); i++ {
		if a.getValuesFuncs[i] != nil {
			userValues, err := a.getValuesFuncs[i](cluster, addon)
			if err != nil {
				return nil, nil, nil, err
			}

			publicValues := map[string]interface{}{}
			for k, v := range userValues {
				if _, ok := PrivateValuesKeys[k]; ok {
					privateValues[k] = v
					continue
				}
				publicValues[k] = v
			}

			overrideValues = addonfactory.MergeValues(overrideValues, publicValues)
		}
	}
	builtinSortedKeys, builtinValues, err := a.getBuiltinValues(cluster, addon, privateValues)
	if err != nil {
		return presetValues, overrideValues, privateValues, nil
	}
	// builtinValues contains CLUSTER_NAME and INSTALL_NAMESPACE, and it should override overrideValues if
	// the contained values are also set in the overrideValues, since these values should not be set externally.
	overrideValues = addonfactory.MergeValues(overrideValues, builtinValues)

	for k, v := range overrideValues {
		_, ok := v.(string)
		if !ok {
			return nil, nil, nil, fmt.Errorf("only support string type for variables, invalid key %s", k)
		}
	}

	keys := append(defaultSortedKeys, builtinSortedKeys...) //nolint:gocritic

	for _, key := range keys {
		presetValues = append(presetValues, keyValuePair{
			name:  key,
			value: overrideValues[key].(string),
		})
	}
	return presetValues, overrideValues, privateValues, nil
}

func (a *CRDTemplateAgentAddon) getBuiltinValues(
	cluster *clusterv1.ManagedCluster,
	_ *addonapiv1alpha1.ManagedClusterAddOn,
	privateValues map[string]interface{}) ([]string, addonfactory.Values, error) {
	builtinValues := templateCRDBuiltinValues{}
	builtinValues.ClusterName = cluster.GetName()

	namespace, ok := privateValues[InstallNamespacePrivateValueKey]
	if ok && namespace != nil {
		builtinValues.InstallNamespace = namespace.(string)
	}

	value, err := addonfactory.JsonStructToValues(builtinValues)
	if err != nil {
		return nil, nil, err
	}
	return a.sortValueKeys(value), value, nil
}

func (a *CRDTemplateAgentAddon) getDefaultValues(
	_ *clusterv1.ManagedCluster,
	_ *addonapiv1alpha1.ManagedClusterAddOn,
	template *addonapiv1alpha1.AddOnTemplate) ([]string, addonfactory.Values, error) {
	defaultValues := templateCRDDefaultValues{}

	// TODO: hubKubeConfigSecret depends on the signer configuration in registration, and the registration is an array.
	if template.Spec.Registration != nil {
		defaultValues.HubKubeConfigPath = hubKubeconfigPath()
	}

	value, err := addonfactory.JsonStructToValues(defaultValues)
	if err != nil {
		return nil, nil, err
	}
	return a.sortValueKeys(value), value, nil
}

func (a *CRDTemplateAgentAddon) sortValueKeys(value addonfactory.Values) []string {
	keys := make([]string, 0)
	for k := range value {
		keys = append(keys, k)
	}

	sort.Strings(keys)
	return keys
}

func hubKubeconfigPath() string {
	return "/managed/hub-kubeconfig/kubeconfig"
}

func GetAddOnRegistriesPrivateValuesFromClusterAnnotation(
	logger klog.Logger,
	cluster *clusterv1.ManagedCluster,
	_ *addonapiv1alpha1.ManagedClusterAddOn) (addonfactory.Values, error) {
	values := map[string]interface{}{}
	annotations := cluster.GetAnnotations()
	logger.V(4).Info("Try to get image registries from annotation",
		"annotationKey", clusterv1.ClusterImageRegistriesAnnotationKey,
		"annotationValue", annotations[clusterv1.ClusterImageRegistriesAnnotationKey])
	if len(annotations[clusterv1.ClusterImageRegistriesAnnotationKey]) == 0 {
		return values, nil
	}
	type ImageRegistries struct {
		Registries []addonapiv1alpha1.ImageMirror `json:"registries"`
	}

	imageRegistries := ImageRegistries{}
	err := json.Unmarshal([]byte(annotations[clusterv1.ClusterImageRegistriesAnnotationKey]), &imageRegistries)
	if err != nil {
		logger.Error(err, "Failed to unmarshal the annotation",
			"annotationKey", clusterv1.ClusterImageRegistriesAnnotationKey,
			"annotationValue", annotations[clusterv1.ClusterImageRegistriesAnnotationKey])
		return values, err
	}

	if len(imageRegistries.Registries) == 0 {
		return values, nil
	}

	logger.V(4).Info("Image registries values", "registries", imageRegistries.Registries)
	return addonfactory.Values{
		RegistriesPrivateValueKey: imageRegistries.Registries,
	}, nil
}
