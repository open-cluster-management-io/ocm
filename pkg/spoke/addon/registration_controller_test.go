package addon

import (
	"testing"

	certificates "k8s.io/api/certificates/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/open-cluster-management/registration/pkg/clientcert"
)

func TestFilterCSREvents(t *testing.T) {
	clusterName := "cluster1"
	addonName := "addon1"
	signerName := "signer1"

	cases := []struct {
		name     string
		csr      *certificates.CertificateSigningRequest
		expected bool
	}{
		{
			name: "csr not from the managed cluster",
			csr:  &certificates.CertificateSigningRequest{},
		},
		{
			name: "csr not for the addon",
			csr:  &certificates.CertificateSigningRequest{},
		},
		{
			name: "csr with different signer name",
			csr:  &certificates.CertificateSigningRequest{},
		},
		{
			name: "valid csr",
			csr: &certificates.CertificateSigningRequest{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						// the labels are only hints. Anyone could set/modify them.
						clientcert.ClusterNameLabel: clusterName,
						clientcert.AddonNameLabel:   addonName,
					},
				},
				Spec: certificates.CertificateSigningRequestSpec{
					SignerName: signerName,
				},
			},
			expected: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			filterFunc := createCSREventFilterFunc(clusterName, addonName, signerName)
			actual := filterFunc(c.csr)
			if actual != c.expected {
				t.Errorf("Expected %v but got %v", c.expected, actual)
			}
		})
	}
}
