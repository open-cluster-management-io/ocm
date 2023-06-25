package templateagent

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	"open-cluster-management.io/addon-framework/pkg/utils"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
)

// AddonTemplateConfigRef return the first addon template config
func AddonTemplateConfigRef(
	configReferences []addonapiv1alpha1.ConfigReference) (bool, addonapiv1alpha1.ConfigReference) {
	for _, config := range configReferences {
		if config.Group == utils.AddOnTemplateGVR.Group && config.Resource == utils.AddOnTemplateGVR.Resource {
			return true, config
		}
	}
	return false, addonapiv1alpha1.ConfigReference{}
}

// GetTemplateSpecHash returns the sha256 hash of the spec field of the addon template
func GetTemplateSpecHash(template *addonapiv1alpha1.AddOnTemplate) (string, error) {
	unstructuredTemplate, err := runtime.DefaultUnstructuredConverter.ToUnstructured(template)
	if err != nil {
		return "", err
	}
	specHash, err := utils.GetSpecHash(&unstructured.Unstructured{
		Object: unstructuredTemplate,
	})
	if err != nil {
		return specHash, err
	}
	return specHash, nil
}

// SupportAddOnTemplate return true if the given ClusterManagementAddOn supports the AddOnTemplate
func SupportAddOnTemplate(cma *addonapiv1alpha1.ClusterManagementAddOn) bool {
	if cma == nil {
		return false
	}

	for _, config := range cma.Spec.SupportedConfigs {
		if config.Group == utils.AddOnTemplateGVR.Group && config.Resource == utils.AddOnTemplateGVR.Resource {
			return true
		}
	}
	return false
}
