package helpers

import (
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/klog/v2"
)

// reservedLabelDomain is the label key domain OCM owns; the spoke agent must not set labels in it.
const reservedLabelDomain = "open-cluster-management.io"

// FilterClusterLabels keeps only the labels the spoke is meant to set on the
// ManagedCluster, dropping any key in the OCM reserved domain. It is input hygiene
// for the operator-provided labels, not a hub-side authorization check.
func FilterClusterLabels(labels map[string]string) map[string]string {
	clusterLabels := make(map[string]string)
	if labels == nil {
		return clusterLabels
	}

	for k, v := range labels {
		if isReservedLabelKey(k) {
			klog.Warningf("label %q is reserved by open-cluster-management and will be ignored", k)
			continue
		}
		// Drop labels that are not valid Kubernetes labels so a single bad entry
		// cannot block the ManagedCluster creation during bootstrap.
		if errs := validation.IsQualifiedName(k); len(errs) > 0 {
			klog.Warningf("label key %q is not a valid label key and will be ignored: %s", k, strings.Join(errs, "; "))
			continue
		}
		if errs := validation.IsValidLabelValue(v); len(errs) > 0 {
			klog.Warningf("label value of %q is not a valid label value and will be ignored: %s", k, strings.Join(errs, "; "))
			continue
		}
		clusterLabels[k] = v
	}

	return clusterLabels
}

func isReservedLabelKey(key string) bool {
	domain, _, found := strings.Cut(key, "/")
	if !found {
		return false
	}
	return domain == reservedLabelDomain || strings.HasSuffix(domain, "."+reservedLabelDomain)
}
