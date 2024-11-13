package aws_irsa

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/cache"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	managedclusterv1lister "open-cluster-management.io/api/client/cluster/listers/cluster/v1"

	"open-cluster-management.io/ocm/pkg/registration/register"
)

func TestIsCRApproved(t *testing.T) {
	cases := []struct {
		name       string
		cr         string
		crApproved bool
	}{}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			indexer := cache.NewIndexer(
				cache.MetaNamespaceKeyFunc,
				cache.Indexers{
					cache.NamespaceIndex: cache.MetaNamespaceIndexFunc,
				})
			require.NoError(t, indexer.Add(c.cr))
			lister := managedclusterv1lister.NewManagedClusterLister(indexer)
			ctrl := &v1AWSIRSAControl{
				hubManagedClusterLister: lister,
			}
			crApproved, err := ctrl.isApproved(c.name)
			assert.NoError(t, err)
			if crApproved != c.crApproved {
				t.Errorf("expected %t, but got %t", c.crApproved, crApproved)
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

			registerImpl := &AWSIRSADriver{}
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
