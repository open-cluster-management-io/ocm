package helmaddonfactory

import (
	"encoding/json"

	"github.com/fatih/structs"
	"k8s.io/klog/v2"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

// GetValuesFromAddonAnnotation get the values in the annotation of addon cr.
// the key of the annotation is `addon.open-cluster-management.io/helmchart-values`, the value is a json string which has the values.
// for example: "addon.open-cluster-management.io/helmchart-values": `{"NodeSelector":{"host":"ssd"},"Image":"quay.io/helloworld:2.4"}`
func GetValuesFromAddonAnnotation(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) (Values, error) {
	values := map[string]interface{}{}
	annotations := addon.GetAnnotations()
	if len(annotations[annotationValuesName]) == 0 {
		return values, nil
	}

	err := json.Unmarshal([]byte(annotations[annotationValuesName]), &values)
	if err != nil {
		klog.Error("failed to unmarshal the values from annotation of addon cr %v. err:%v", annotations[annotationValuesName], err)
		return values, err
	}

	return values, nil
}

// MergeValues merges the 2 given Values to a Values.
// the values of b will override that in a for the same fields.
func MergeValues(a, b Values) Values {
	out := make(map[string]interface{}, len(a))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		if v, ok := v.(map[string]interface{}); ok {
			if bv, ok := out[k]; ok {
				if bv, ok := bv.(map[string]interface{}); ok {
					out[k] = MergeValues(bv, v)
					continue
				}
			}
		}
		out[k] = v
	}
	return out
}

// StructToValues converts the given struct to a Values
func StructToValues(a interface{}) Values {
	return structs.Map(a)
}
