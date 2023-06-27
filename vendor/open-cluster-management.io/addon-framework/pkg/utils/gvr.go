package utils

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
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
	return ContainGR(BuiltInAddOnConfigGVRs, group, resource)
}
