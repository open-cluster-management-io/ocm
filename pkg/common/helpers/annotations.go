package helpers

import (
	"strings"

	"k8s.io/klog/v2"

	operatorv1 "open-cluster-management.io/api/operator/v1"
)

func FilterClusterAnnotations(annotations map[string]string) map[string]string {
	clusterAnnotations := make(map[string]string)
	if annotations == nil {
		return clusterAnnotations
	}

	for k, v := range annotations {
		if strings.HasPrefix(k, operatorv1.ClusterAnnotationsKeyPrefix) {
			clusterAnnotations[k] = v
		} else {
			klog.Warningf("annotation %q is not prefixed with %q, it will be ignored", k, operatorv1.ClusterAnnotationsKeyPrefix)
		}
	}

	return clusterAnnotations
}
