package utils

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
)

var AddOnDeploymentConfigGVR = schema.GroupVersionResource{
	Group:    "addon.open-cluster-management.io",
	Version:  "v1alpha1",
	Resource: "addondeploymentconfigs",
}

var AddOnTemplateGVR = schema.GroupVersionResource{
	Group:    "addon.open-cluster-management.io",
	Version:  "v1alpha1",
	Resource: "addontemplates",
}

var BuiltInAddOnConfigGVRs = map[schema.GroupVersionResource]bool{
	AddOnDeploymentConfigGVR: true,
	AddOnTemplateGVR:         true,
}

// ContainGR returns true if the given group resource is in the given map
func ContainGR(gvrs map[schema.GroupVersionResource]bool, group, resource string) bool {
	for gvr := range gvrs {
		if gvr.Group == group && gvr.Resource == resource {
			return true
		}
	}
	return false
}

// FilterOutTheBuiltInAddOnConfigGVRs returns a new slice of GroupVersionResource that does not contain
// the built-in addOn config GVRs
func FilterOutTheBuiltInAddOnConfigGVRs(
	gvrs map[schema.GroupVersionResource]bool) map[schema.GroupVersionResource]bool {

	newGVRs := make(map[schema.GroupVersionResource]bool)
	for gvr := range gvrs {
		if !isBuiltInAddOnConfigGVR(gvr.Group, gvr.Resource) {
			newGVRs[gvr] = true
		}
	}
	return newGVRs
}

func isBuiltInAddOnConfigGVR(group, resource string) bool {
	for gvr := range BuiltInAddOnConfigGVRs {
		if gvr.Group == group && gvr.Resource == resource {
			return true
		}
	}
	return false
}

// GetSpecHash returns the sha256 hash of the spec field of the given object
func GetSpecHash(obj *unstructured.Unstructured) (string, error) {
	spec, ok := obj.Object["spec"]
	if !ok {
		return "", fmt.Errorf("object has no spec field")
	}

	specBytes, err := json.Marshal(spec)
	if err != nil {
		return "", err
	}

	hash := sha256.Sum256(specBytes)

	return fmt.Sprintf("%x", hash), nil
}

// GetTemplateSpecHash returns the sha256 hash of the spec field of the addon template
func GetTemplateSpecHash(template *addonapiv1alpha1.AddOnTemplate) (string, error) {
	unstructuredTemplate, err := runtime.DefaultUnstructuredConverter.ToUnstructured(template)
	if err != nil {
		return "", err
	}
	specHash, err := GetSpecHash(&unstructured.Unstructured{
		Object: unstructuredTemplate,
	})
	if err != nil {
		return specHash, err
	}
	return specHash, nil
}

// AddonTemplateConfigRef return the first addon template config
func AddonTemplateConfigRef(
	configReferences []addonapiv1alpha1.ConfigReference) (bool, addonapiv1alpha1.ConfigReference) {
	for _, config := range configReferences {
		if config.Group == AddOnTemplateGVR.Group && config.Resource == AddOnTemplateGVR.Resource {
			return true, config
		}
	}
	return false, addonapiv1alpha1.ConfigReference{}
}

// SupportAddOnTemplate return true if the given ClusterManagementAddOn supports the AddOnTemplate
func SupportAddOnTemplate(cma *addonapiv1alpha1.ClusterManagementAddOn) bool {
	if cma == nil {
		return false
	}

	for _, config := range cma.Spec.SupportedConfigs {
		if config.Group == AddOnTemplateGVR.Group && config.Resource == AddOnTemplateGVR.Resource {
			return true
		}
	}
	return false
}
