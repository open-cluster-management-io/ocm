package utils

import addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"

func MergeRelatedObjects(modified *bool, objs *[]addonapiv1alpha1.ObjectReference, obj addonapiv1alpha1.ObjectReference) {
	if *objs == nil {
		*objs = []addonapiv1alpha1.ObjectReference{}
	}

	for _, o := range *objs {
		if o.Group == obj.Group && o.Resource == obj.Resource && o.Name == obj.Name && o.Namespace == obj.Namespace {
			return
		}
	}

	*objs = append(*objs, obj)
	*modified = true
}
