package clientcert

import (
	"crypto/x509/pkix"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	certificates "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/client-go/listers/certificates/v1"
	"k8s.io/client-go/tools/cache"
	certutil "k8s.io/client-go/util/cert"

	testinghelpers "open-cluster-management.io/registration/pkg/helpers/testing"
)

func TestIsCSRApproved(t *testing.T) {
	cases := []struct {
		name string
		csr  *certificates.CertificateSigningRequest

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
			indexer := cache.NewIndexer(
				cache.MetaNamespaceKeyFunc,
				cache.Indexers{
					cache.NamespaceIndex: cache.MetaNamespaceIndexFunc,
				})
			require.NoError(t, indexer.Add(c.csr))
			lister := v1.NewCertificateSigningRequestLister(indexer)
			ctrl := &v1CSRControl{
				hubCSRLister: lister,
			}
			csrApproved, err := ctrl.isApproved(c.csr.Name)
			assert.NoError(t, err)
			if csrApproved != c.csrApproved {
				t.Errorf("expected %t, but got %t", c.csrApproved, csrApproved)
			}
		})
	}
}

func TestHasValidHubKubeconfig(t *testing.T) {
	cases := []struct {
		name    string
		secret  *corev1.Secret
		subject *pkix.Name
		isValid bool
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
			name: "expired cert",
			secret: testinghelpers.NewHubKubeconfigSecret(testNamespace, testSecretName, "", testinghelpers.NewTestCert("test", -60*time.Second), map[string][]byte{
				KubeconfigFile: testinghelpers.NewKubeconfig(nil, nil),
			}),
		},
		{
			name: "invalid common name",
			secret: testinghelpers.NewHubKubeconfigSecret(testNamespace, testSecretName, "", testinghelpers.NewTestCert("test", 60*time.Second), map[string][]byte{
				KubeconfigFile: testinghelpers.NewKubeconfig(nil, nil),
			}),
			subject: &pkix.Name{
				CommonName: "wrong-common-name",
			},
		},
		{
			name: "valid kubeconfig",
			secret: testinghelpers.NewHubKubeconfigSecret(testNamespace, testSecretName, "", testinghelpers.NewTestCert("test", 60*time.Second), map[string][]byte{
				KubeconfigFile: testinghelpers.NewKubeconfig(nil, nil),
			}),
			subject: &pkix.Name{
				CommonName: "test",
			},
			isValid: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			isValid := HasValidHubKubeconfig(c.secret, c.subject)
			if isValid != c.isValid {
				t.Errorf("expected %t, but got %t", c.isValid, isValid)
			}
		})
	}
}

func TestIsCertificateValid(t *testing.T) {
	cases := []struct {
		name     string
		testCert *testinghelpers.TestCert
		subject  *pkix.Name
		isValid  bool
	}{
		{
			name:     "no cert",
			testCert: &testinghelpers.TestCert{},
		},
		{
			name:     "bad cert",
			testCert: &testinghelpers.TestCert{Cert: []byte("bad cert")},
		},
		{
			name:     "expired cert",
			testCert: testinghelpers.NewTestCert("test", -60*time.Second),
		},
		{
			name:     "invalid common name",
			testCert: testinghelpers.NewTestCert("test", 60*time.Second),
			subject: &pkix.Name{
				CommonName: "wrong-common-name",
			},
		},
		{
			name: "valid cert",
			testCert: testinghelpers.NewTestCertWithSubject(pkix.Name{
				CommonName: "test",
			}, 60*time.Second),
			subject: &pkix.Name{
				CommonName: "test",
			},
			isValid: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			isValid, _ := IsCertificateValid(c.testCert.Cert, c.subject)
			if isValid != c.isValid {
				t.Errorf("expected %t, but got %t", c.isValid, isValid)
			}
		})
	}
}

func TestGetCertValidityPeriod(t *testing.T) {
	certs := []byte{}
	firstCert := testinghelpers.NewTestCert("cluster0", 5*time.Second).Cert
	certs = append(certs, firstCert...)
	secondCert := testinghelpers.NewTestCert("cluster0", 10*time.Second).Cert
	certs = append(certs, secondCert...)
	firstCerts, _ := certutil.ParseCertsPEM(firstCert)
	secondCerts, _ := certutil.ParseCertsPEM(secondCert)
	notBefore := secondCerts[0].NotBefore
	notAfter := firstCerts[0].NotAfter

	cases := []struct {
		name        string
		secret      *corev1.Secret
		expectedErr string
		notBefore   time.Time
		notAfter    time.Time
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
			name:      "valid cert",
			secret:    testinghelpers.NewHubKubeconfigSecret(testNamespace, testSecretName, "", &testinghelpers.TestCert{Cert: certs}, map[string][]byte{}),
			notBefore: notBefore,
			notAfter:  notAfter,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			notBefore, notAfter, err := getCertValidityPeriod(c.secret)
			testinghelpers.AssertError(t, err, c.expectedErr)
			if err != nil {
				return
			}
			if !c.notBefore.Equal(*notBefore) {
				t.Errorf("expect %v, but got %v", c.notBefore, *notBefore)
			}
			if !c.notAfter.Equal(*notAfter) {
				t.Errorf("expect %v, but got %v", c.notAfter, *notAfter)
			}
		})
	}
}
