package templateagent

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	"open-cluster-management.io/addon-framework/pkg/utils"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
)

// AddonTemplateConfigRef return the first addon template config
func AddonTemplateConfigRef(
	configReferences []addonapiv1alpha1.ConfigReference) (bool, addonapiv1alpha1.ConfigReference) {
	return utils.GetAddOnConfigRef(configReferences, utils.AddOnTemplateGVR.Group, utils.AddOnTemplateGVR.Resource)
}

// GetAddOnTemplateSpecHash returns the sha256 hash of the spec field of the addon template
func GetAddOnTemplateSpecHash(template *addonapiv1alpha1.AddOnTemplate) (string, error) {
	if template == nil {
		return "", fmt.Errorf("addon template is nil")
	}
	unstructuredTemplate, err := runtime.DefaultUnstructuredConverter.ToUnstructured(template)
	if err != nil {
		return "", err
	}
	return utils.GetSpecHash(&unstructured.Unstructured{
		Object: unstructuredTemplate,
	})
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
