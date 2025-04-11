package csr

import (
	"crypto/x509/pkix"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	certificates "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/client-go/listers/certificates/v1"
	"k8s.io/client-go/tools/cache"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	certutil "k8s.io/client-go/util/cert"
	"k8s.io/klog/v2/ktesting"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	testinghelpers "open-cluster-management.io/ocm/pkg/registration/helpers/testing"
	"open-cluster-management.io/ocm/pkg/registration/register"
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
			csrApproved, err := ctrl.IsApproved(c.csr.Name)
			assert.NoError(t, err)
			if csrApproved != c.csrApproved {
				t.Errorf("expected %t, but got %t", c.csrApproved, csrApproved)
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
			name: "invalid organization",
			testCert: testinghelpers.NewTestCertWithSubject(pkix.Name{
				CommonName:   "test",
				Organization: []string{"org_foo"},
			}, 60*time.Second),
			subject: &pkix.Name{
				CommonName:   "test",
				Organization: []string{"org_bar"},
			},
			isValid: false,
		},
		{
			name: "invalid organization unit",
			testCert: testinghelpers.NewTestCertWithSubject(pkix.Name{
				CommonName:         "test",
				Organization:       []string{"org"},
				OrganizationalUnit: []string{"ou_foo"},
			}, 60*time.Second),
			subject: &pkix.Name{
				CommonName:         "test",
				Organization:       []string{"org"},
				OrganizationalUnit: []string{"ou_bar"},
			},
			isValid: false,
		},
		{
			name: "valid cert",
			testCert: testinghelpers.NewTestCertWithSubject(pkix.Name{
				CommonName:         "test",
				Organization:       []string{"org"},
				OrganizationalUnit: []string{"ou"},
			}, 60*time.Second),
			subject: &pkix.Name{
				CommonName:         "test",
				Organization:       []string{"org"},
				OrganizationalUnit: []string{"ou"},
			},
			isValid: true,
		},
		{
			name: "valid cert different order",
			testCert: testinghelpers.NewTestCertWithSubject(pkix.Name{
				CommonName:         "test",
				Organization:       []string{"org", "org2"},
				OrganizationalUnit: []string{"ou", "ou2"},
			}, 60*time.Second),
			subject: &pkix.Name{
				CommonName:         "test",
				Organization:       []string{"org2", "org"},
				OrganizationalUnit: []string{"ou2", "ou"},
			},
			isValid: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			logger, _ := ktesting.NewTestContext(t)
			isValid, _ := IsCertificateValid(logger, c.testCert.Cert, c.subject)
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
			name: "bad cert",
			secret: testinghelpers.NewHubKubeconfigSecret(
				testNamespace, testSecretName, "",
				&testinghelpers.TestCert{Cert: []byte("bad cert")}, map[string][]byte{}),
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
			testingcommon.AssertError(t, err, c.expectedErr)
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

func TestBuildKubeconfig(t *testing.T) {
	cases := []struct {
		name           string
		server         string
		proxyURL       string
		caData         []byte
		clientCertFile string
		clientKeyFile  string
	}{
		{
			name:           "without proxy",
			server:         "https://127.0.0.1:6443",
			caData:         []byte("fake-ca-bundle"),
			clientCertFile: "tls.crt",
			clientKeyFile:  "tls.key",
		},
		{
			name:           "with proxy",
			server:         "https://127.0.0.1:6443",
			caData:         []byte("fake-ca-bundle-with-proxy-ca"),
			proxyURL:       "https://127.0.0.1:3129",
			clientCertFile: "tls.crt",
			clientKeyFile:  "tls.key",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bootstrapKubeconfig := &clientcmdapi.Config{
				Clusters: map[string]*clientcmdapi.Cluster{
					"default-cluster": {
						Server:                   c.server,
						InsecureSkipTLSVerify:    false,
						CertificateAuthorityData: c.caData,
						ProxyURL:                 c.proxyURL,
					}},
				// Define a context that connects the auth info and cluster, and set it as the default
				Contexts: map[string]*clientcmdapi.Context{register.DefaultKubeConfigContext: {
					Cluster:   "default-cluster",
					AuthInfo:  register.DefaultKubeConfigAuth,
					Namespace: "configuration",
				}},
				CurrentContext: register.DefaultKubeConfigContext,
				AuthInfos: map[string]*clientcmdapi.AuthInfo{
					register.DefaultKubeConfigAuth: {
						ClientCertificate: c.clientCertFile,
						ClientKey:         c.clientKeyFile,
					},
				},
			}

			registerImpl := &CSRDriver{}
			kubeconfig := registerImpl.BuildKubeConfigFromTemplate(bootstrapKubeconfig)
			currentContext, ok := kubeconfig.Contexts[kubeconfig.CurrentContext]
			if !ok {
				t.Errorf("current context %q not found: %v", kubeconfig.CurrentContext, kubeconfig)
			}

			cluster, ok := kubeconfig.Clusters[currentContext.Cluster]
			if !ok {
				t.Errorf("cluster %q not found: %v", currentContext.Cluster, kubeconfig)
			}

			if cluster.Server != c.server {
				t.Errorf("expected server %q, but got %q", c.server, cluster.Server)
			}

			if cluster.ProxyURL != c.proxyURL {
				t.Errorf("expected proxy URL %q, but got %q", c.proxyURL, cluster.ProxyURL)
			}

			if !reflect.DeepEqual(cluster.CertificateAuthorityData, c.caData) {
				t.Errorf("expected ca data %v, but got %v", c.caData, cluster.CertificateAuthorityData)
			}

			authInfo, ok := kubeconfig.AuthInfos[currentContext.AuthInfo]
			if !ok {
				t.Errorf("auth info %q not found: %v", currentContext.AuthInfo, kubeconfig)
			}

			if authInfo.ClientCertificate != c.clientCertFile {
				t.Errorf("expected client certificate %q, but got %q", c.clientCertFile, authInfo.ClientCertificate)
			}

			if authInfo.ClientKey != c.clientKeyFile {
				t.Errorf("expected client key %q, but got %q", c.clientKeyFile, authInfo.ClientKey)
			}
		})
	}
}
