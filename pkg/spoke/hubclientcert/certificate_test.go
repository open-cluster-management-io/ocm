package hubclientcert

import (
	"crypto/x509"
	"testing"
	"time"

	testinghelpers "github.com/open-cluster-management/registration/pkg/helpers/testing"

	certificates "k8s.io/api/certificates/v1beta1"
	corev1 "k8s.io/api/core/v1"
	certutil "k8s.io/client-go/util/cert"
)

func TestCSRApproved(t *testing.T) {
	cases := []struct {
		name        string
		csr         *certificates.CertificateSigningRequest
		csrApproved bool
	}{
		{
			name: "pending csr",
			csr:  testinghelpers.NewCSR(testinghelpers.CSRHolder{}),
		},
		{
			name: "denied csr",
			csr:  testinghelpers.NewDeniedCSR(testinghelpers.CSRHolder{}),
		},
		{
			name:        "approved csr",
			csr:         testinghelpers.NewApprovedCSR(testinghelpers.CSRHolder{}),
			csrApproved: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			csrApproved := isCSRApproved(c.csr)
			if csrApproved != c.csrApproved {
				t.Errorf("expected %t, but got %t", c.csrApproved, csrApproved)
			}
		})
	}
}

func TestValidKubeconfig(t *testing.T) {
	cases := []struct {
		name       string
		secret     *corev1.Secret
		commonName string
		isValid    bool
	}{
		{
			name:   "no data",
			secret: testinghelpers.NewHubKubeconfigSecret(testNamespace, testSecretName, "", nil, nil),
		},
		{
			name:   "no kubeconfig",
			secret: testinghelpers.NewHubKubeconfigSecret(testNamespace, testSecretName, "", nil, map[string][]byte{}),
		},
		{
			name: "no key",
			secret: testinghelpers.NewHubKubeconfigSecret(testNamespace, testSecretName, "", nil, map[string][]byte{
				KubeconfigFile: testinghelpers.NewKubeconfig(nil, nil),
			}),
		},
		{
			name: "no cert",
			secret: testinghelpers.NewHubKubeconfigSecret(testNamespace, testSecretName, "", &testinghelpers.TestCert{Key: []byte("key")}, map[string][]byte{
				KubeconfigFile: testinghelpers.NewKubeconfig(nil, nil),
			}),
		},
		{
			name: "bad cert",
			secret: testinghelpers.NewHubKubeconfigSecret(testNamespace, testSecretName, "", &testinghelpers.TestCert{Key: []byte("key"), Cert: []byte("bad cert")}, map[string][]byte{
				KubeconfigFile: testinghelpers.NewKubeconfig(nil, nil),
			}),
		},
		{
			name: "unmatched common name",
			secret: testinghelpers.NewHubKubeconfigSecret(testNamespace, testSecretName, "", testinghelpers.NewTestCert("test", 60*time.Second), map[string][]byte{
				KubeconfigFile: testinghelpers.NewKubeconfig(nil, nil),
			}),
			commonName: "wrong-common-name",
		},
		{
			name: "valid hub config",
			secret: testinghelpers.NewHubKubeconfigSecret(testNamespace, testSecretName, "", testinghelpers.NewTestCert("test", 60*time.Second), map[string][]byte{
				KubeconfigFile: testinghelpers.NewKubeconfig(nil, nil),
			}),
			commonName: "test",
			isValid:    true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			isValid := hasValidKubeconfig(c.secret, c.commonName)
			if isValid != c.isValid {
				t.Errorf("expected %t, but got %t", c.isValid, isValid)
			}
		})
	}
}

func TestGetCertValidityPeriod(t *testing.T) {
	certs := []byte{}
	certs = append(certs, testinghelpers.NewTestCert("cluster0", 10*time.Second).Cert...)
	secondCert := testinghelpers.NewTestCert("cluster0", 5*time.Second).Cert
	certs = append(certs, secondCert...)
	expectedCerts, _ := certutil.ParseCertsPEM(secondCert)
	cases := []struct {
		name         string
		secret       *corev1.Secret
		expectedCert *x509.Certificate
		expectedErr  string
	}{
		{
			name:        "no data",
			secret:      testinghelpers.NewHubKubeconfigSecret(testNamespace, testSecretName, "", nil, nil),
			expectedErr: "no client certificate found in secret \"testns/testsecret\"",
		},
		{
			name:        "no cert",
			secret:      testinghelpers.NewHubKubeconfigSecret(testNamespace, testSecretName, "", nil, map[string][]byte{}),
			expectedErr: "no client certificate found in secret \"testns/testsecret\"",
		},
		{
			name:        "bad cert",
			secret:      testinghelpers.NewHubKubeconfigSecret(testNamespace, testSecretName, "", &testinghelpers.TestCert{Cert: []byte("bad cert")}, map[string][]byte{}),
			expectedErr: "unable to parse TLS certificates: data does not contain any valid RSA or ECDSA certificates",
		},
		{
			name:         "valid cert",
			secret:       testinghelpers.NewHubKubeconfigSecret(testNamespace, testSecretName, "", &testinghelpers.TestCert{Cert: certs}, map[string][]byte{}),
			expectedCert: expectedCerts[0],
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			notBefore, notAfter, err := getCertValidityPeriod(c.secret)
			testinghelpers.AssertError(t, err, c.expectedErr)
			if c.expectedCert == nil {
				return
			}
			if !c.expectedCert.NotBefore.Equal(*notBefore) {
				t.Errorf("expect %v, but got %v", expectedCerts[0].NotBefore, *notBefore)
			}
			if !c.expectedCert.NotAfter.Equal(*notAfter) {
				t.Errorf("expect %v, but got %v", expectedCerts[0].NotAfter, *notAfter)
			}
		})
	}
}

func TestGetClusterAgentNamesFromCertificate(t *testing.T) {
	cases := []struct {
		name                string
		certData            []byte
		expectedClusterName string
		expectedAgentName   string
		expectedErrorPrefix string
	}{
		{
			name:                "cert data is invalid",
			certData:            []byte("invalid cert"),
			expectedErrorPrefix: "unable to parse certificate:",
		},
		{
			name:     "cert with invalid commmon name",
			certData: testinghelpers.NewTestCert("test", 60*time.Second).Cert,
		},
		{
			name:                "valid cert with correct common name",
			certData:            testinghelpers.NewTestCert("system:open-cluster-management:cluster1:agent1", 60*time.Second).Cert,
			expectedClusterName: "cluster1",
			expectedAgentName:   "agent1",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterName, agentName, err := GetClusterAgentNamesFromCertificate(c.certData)
			testinghelpers.AssertErrorWithPrefix(t, err, c.expectedErrorPrefix)

			if clusterName != c.expectedClusterName {
				t.Errorf("expect %v, but got %v", c.expectedClusterName, clusterName)
			}

			if agentName != c.expectedAgentName {
				t.Errorf("expect %v, but got %v", c.expectedAgentName, agentName)
			}
		})
	}
}
