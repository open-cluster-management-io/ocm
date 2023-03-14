package utils

import (
	"crypto/x509/pkix"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/crypto"
	"github.com/stretchr/testify/assert"
	certificatesv1 "k8s.io/api/certificates/v1"
	certificatesv1beta1 "k8s.io/api/certificates/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	certutil "k8s.io/client-go/util/cert"
	"k8s.io/client-go/util/keyutil"
	"open-cluster-management.io/addon-framework/pkg/agent"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

func newCSRWithSigner(signer, commonName, clusterName string, orgs ...string) *certificatesv1.CertificateSigningRequest {
	csr := newCSR(commonName, clusterName, orgs...)
	csr.Spec.SignerName = signer
	return csr
}

func newCSR(commonName string, clusterName string, orgs ...string) *certificatesv1.CertificateSigningRequest {
	clientKey, _ := keyutil.MakeEllipticPrivateKeyPEM()
	privateKey, _ := keyutil.ParsePrivateKeyPEM(clientKey)

	request, _ := certutil.MakeCSR(privateKey, &pkix.Name{CommonName: commonName, Organization: orgs}, []string{"test.localhost"}, nil)

	return &certificatesv1.CertificateSigningRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
		Spec: certificatesv1.CertificateSigningRequestSpec{
			Usages: []certificatesv1.KeyUsage{
				certificatesv1.UsageClientAuth,
			},
			Username: "system:open-cluster-management:" + clusterName,
			Request:  request,
		},
	}
}

func newCluster(name string) *clusterv1.ManagedCluster {
	return &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
}

func newAddon(name, namespace string) *addonapiv1alpha1.ManagedClusterAddOn {
	return &addonapiv1alpha1.ManagedClusterAddOn{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
}

func TestDefaultSigner(t *testing.T) {
	caConfig, err := crypto.MakeSelfSignedCAConfig("test", 10)
	if err != nil {
		t.Errorf("Failed to generate self signed CA config: %v", err)
	}

	ca, key, err := caConfig.GetPEMBytes()
	if err != nil {
		t.Errorf("Failed to get ca cert/key: %v", err)
	}

	signer := DefaultSignerWithExpiry(key, ca, 24*time.Hour)

	cert := signer(newCSR("test", "cluster1"))
	if cert == nil {
		t.Errorf("Expect cert to be signed")
	}

	certs, err := crypto.CertsFromPEM(cert)
	if err != nil {
		t.Errorf("Failed to parse cert: %v", err)
	}

	if len(certs) != 1 {
		t.Errorf("Expect 1 cert signed but got %d", len(certs))
	}

	if certs[0].Issuer.CommonName != "test" {
		t.Errorf("CommonName is not correct")
	}
}

func TestDefaultCSRApprover(t *testing.T) {
	cases := []struct {
		name     string
		csr      *certificatesv1.CertificateSigningRequest
		cluster  *clusterv1.ManagedCluster
		addon    *addonapiv1alpha1.ManagedClusterAddOn
		approved bool
	}{
		{
			name:     "approve csr",
			csr:      newCSR(agent.DefaultUser("cluster1", "addon1", "test"), "cluster1", agent.DefaultGroups("cluster1", "addon1")...),
			cluster:  newCluster("cluster1"),
			addon:    newAddon("addon1", "cluster1"),
			approved: true,
		},
		{
			name:     "requester is not correct",
			csr:      newCSR(agent.DefaultUser("cluster1", "addon1", "test"), "cluster2", agent.DefaultGroups("cluster1", "addon1")...),
			cluster:  newCluster("cluster1"),
			addon:    newAddon("addon1", "cluster1"),
			approved: false,
		},
		{
			name:     "common name is not correct",
			csr:      newCSR("test", "cluster1", agent.DefaultGroups("cluster1", "addon1")...),
			cluster:  newCluster("cluster1"),
			addon:    newAddon("addon1", "cluster1"),
			approved: false,
		},
		{
			name:     "group is not correct",
			csr:      newCSR(agent.DefaultUser("cluster1", "addon1", "test"), "cluster1", "group1"),
			cluster:  newCluster("cluster1"),
			addon:    newAddon("addon1", "cluster1"),
			approved: false,
		},
	}

	approver := DefaultCSRApprover("test")
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			approved := approver(c.cluster, c.addon, c.csr)
			if approved != c.approved {
				t.Errorf("Expected approve is %t, but got %t", c.approved, approved)
			}
		})
	}
}

func TestUnionApprover(t *testing.T) {
	approveAll := func(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn, csr *certificatesv1.CertificateSigningRequest) bool {
		return true
	}

	approveNone := func(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn, csr *certificatesv1.CertificateSigningRequest) bool {
		return false
	}

	cases := []struct {
		name        string
		signer      string
		approveFunc map[string]agent.CSRApproveFunc
		approved    bool
	}{
		{
			name:        "approve all",
			signer:      "a",
			approveFunc: map[string]agent.CSRApproveFunc{"a": approveAll, "b": approveAll},
			approved:    true,
		},
		{
			name:        "approve none",
			signer:      "b",
			approveFunc: map[string]agent.CSRApproveFunc{"a": approveAll, "b": approveNone},
			approved:    false,
		},
		{
			name:        "not match signer",
			signer:      "c",
			approveFunc: map[string]agent.CSRApproveFunc{"a": approveAll, "b": approveAll},
			approved:    false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			approver := UnionCSRApprover(c.approveFunc)
			approved := approver(
				newCluster("cluster1"),
				newAddon("addon1", "cluster1"),
				newCSRWithSigner(c.signer, agent.DefaultUser("cluster1", "addon1", "test"), "cluster1", "group1"),
			)
			if approved != c.approved {
				t.Errorf("Expected approve is %t, but got %t", c.approved, approved)
			}
		})
	}
}

func TestIsCSRSupported(t *testing.T) {
	cases := []struct {
		apiResources    []*metav1.APIResourceList
		expectedV1      bool
		expectedV1beta1 bool
		expectedError   error
	}{
		{
			apiResources: []*metav1.APIResourceList{
				{
					GroupVersion: certificatesv1.SchemeGroupVersion.String(),
					APIResources: []metav1.APIResource{
						{
							Name: "certificatesigningrequests",
							Kind: "CertificateSigningRequest",
						},
					},
				},
				{
					GroupVersion: certificatesv1beta1.SchemeGroupVersion.String(),
					APIResources: []metav1.APIResource{
						{
							Name: "certificatesigningrequests",
							Kind: "CertificateSigningRequest",
						},
					},
				},
			},
			expectedV1:      true,
			expectedV1beta1: true,
		},
		{
			apiResources: []*metav1.APIResourceList{
				{
					GroupVersion: certificatesv1beta1.SchemeGroupVersion.String(),
					APIResources: []metav1.APIResource{
						{
							Name: "certificatesigningrequests",
							Kind: "CertificateSigningRequest",
						},
					},
				},
			},
			expectedV1:      false,
			expectedV1beta1: true,
		},
	}
	for _, c := range cases {
		fakeClient := fake.NewSimpleClientset()
		fakeClient.Resources = c.apiResources
		v1Supported, v1beta1Supported, err := IsCSRSupported(fakeClient)
		assert.Equal(t, c.expectedV1, v1Supported)
		assert.Equal(t, c.expectedV1beta1, v1beta1Supported)
		assert.Equal(t, c.expectedError, err)
	}
}
