package helpers

import (
	"embed"
	"net/url"
	"regexp"

	"github.com/openshift/library-go/pkg/assets"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	certificatesv1 "k8s.io/api/certificates/v1"
	certificatesv1beta1 "k8s.io/api/certificates/v1beta1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/restmapper"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

// Check whether a CSR is in terminal state
func IsCSRInTerminalState(status *certificatesv1.CertificateSigningRequestStatus) bool {
	for _, c := range status.Conditions {
		if c.Type == certificatesv1.CertificateApproved {
			return true
		}
		if c.Type == certificatesv1.CertificateDenied {
			return true
		}
	}
	return false
}

// Isv1beta1CSRInTerminalState checks whether a CSR is in terminal state for v1beta1 version.
func Isv1beta1CSRInTerminalState(status *certificatesv1beta1.CertificateSigningRequestStatus) bool {
	for _, c := range status.Conditions {
		if c.Type == certificatesv1beta1.CertificateApproved {
			return true
		}
		if c.Type == certificatesv1beta1.CertificateDenied {
			return true
		}
	}
	return false
}

// IsValidHTTPSURL validate whether a URL is https URL
func IsValidHTTPSURL(serverURL string) bool {
	if serverURL == "" {
		return false
	}

	parsedServerURL, err := url.Parse(serverURL)
	if err != nil {
		return false
	}

	if parsedServerURL.Scheme != "https" {
		return false
	}

	return true
}

func ManagedClusterAssetFn(fs embed.FS, managedClusterName string) resourceapply.AssetFunc {
	return func(name string) ([]byte, error) {
		config := struct {
			ManagedClusterName string
		}{
			ManagedClusterName: managedClusterName,
		}

		template, err := fs.ReadFile(name)
		if err != nil {
			return nil, err
		}
		return assets.MustCreateAssetFromTemplate(name, template, config).Data, nil
	}
}

// FindTaintByKey returns a taint if the managed cluster has a taint with the given key.
func FindTaintByKey(managedCluster *clusterv1.ManagedCluster, key string) *clusterv1.Taint {
	if managedCluster == nil {
		return nil
	}
	for _, taint := range managedCluster.Spec.Taints {
		if key != taint.Key {
			continue
		}
		return &taint
	}
	return nil
}

func IsTaintEqual(taint1, taint2 clusterv1.Taint) bool {
	// Ignore the comparison of time
	return taint1.Key == taint2.Key && taint1.Value == taint2.Value && taint1.Effect == taint2.Effect
}

// AddTaints add taints to the specified slice, if it did not already exist.
// Return a boolean indicating whether the slice has been updated.
func AddTaints(taints *[]clusterv1.Taint, taint clusterv1.Taint) bool {
	if taints == nil || *taints == nil {
		*taints = make([]clusterv1.Taint, 0)
	}
	if FindTaint(*taints, taint) != nil {
		return false
	}
	*taints = append(*taints, taint)
	return true
}

func RemoveTaints(taints *[]clusterv1.Taint, targets ...clusterv1.Taint) (updated bool) {
	if taints == nil || len(*taints) == 0 || len(targets) == 0 {
		return false
	}

	newTaints := make([]clusterv1.Taint, 0)
	for _, v := range *taints {
		if FindTaint(targets, v) == nil {
			newTaints = append(newTaints, v)
		}
	}
	updated = len(*taints) != len(newTaints)
	*taints = newTaints
	return updated
}

func FindTaint(taints []clusterv1.Taint, taint clusterv1.Taint) *clusterv1.Taint {
	for i := range taints {
		if IsTaintEqual(taints[i], taint) {
			return &taints[i]
		}
	}

	return nil
}

// IsCSRSupported checks whether the cluster supports v1 or v1beta1 csr api.
func IsCSRSupported(nativeClient kubernetes.Interface) (bool, bool, error) {
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(nativeClient.Discovery()))
	mappings, err := mapper.RESTMappings(schema.GroupKind{
		Group: certificatesv1.GroupName,
		Kind:  "CertificateSigningRequest",
	})
	if err != nil {
		return false, false, err
	}
	v1CSRSupported := false
	for _, mapping := range mappings {
		if mapping.GroupVersionKind.Version == "v1" {
			v1CSRSupported = true
		}
	}
	v1beta1CSRSupported := false
	for _, mapping := range mappings {
		if mapping.GroupVersionKind.Version == "v1beta1" {
			v1beta1CSRSupported = true
		}
	}
	return v1CSRSupported, v1beta1CSRSupported, nil
}

// IsEksArnWellFormed checks if the EKS cluster ARN is well-formed
// Example of a well-formed ARN: arn:aws:eks:us-west-2:123456789012:cluster/my-cluster
func IsEksArnWellFormed(eksArn string) bool {
	pattern := "^arn:aws:eks:([a-zA-Z0-9-]+):(\\d{12}):cluster/([a-zA-Z0-9-]+)$"
	matched, err := regexp.MatchString(pattern, eksArn)
	if err != nil {
		return false
	}
	return matched
}
