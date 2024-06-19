package register

import (
	"reflect"
	"testing"

	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func TestBaseKubeConfigFromBootStrap(t *testing.T) {
	server1 := "https://127.0.0.1:6443"
	server2 := "https://api.cluster1.example.com:6443"
	caData1 := []byte("fake-ca-data1")
	caData2 := []byte("fake-ca-data2")
	proxyURL := "https://127.0.0.1:3129"

	cases := []struct {
		name             string
		kubeconfig       *clientcmdapi.Config
		expectedServer   string
		expectedCAData   []byte
		expectedProxyURL string
	}{
		{
			name: "without proxy url",
			kubeconfig: &clientcmdapi.Config{
				Clusters: map[string]*clientcmdapi.Cluster{
					"test-cluster": {
						Server:                   server1,
						CertificateAuthorityData: caData1,
					}},
				// Define a context that connects the auth info and cluster, and set it as the default
				Contexts: map[string]*clientcmdapi.Context{DefaultKubeConfigContext: {
					Cluster:   "test-cluster",
					AuthInfo:  DefaultKubeConfigAuth,
					Namespace: "configuration",
				}},
				CurrentContext: DefaultKubeConfigContext,
				AuthInfos: map[string]*clientcmdapi.AuthInfo{
					DefaultKubeConfigAuth: {
						ClientCertificate: "tls.crt",
						ClientKey:         "tls.key",
					},
				},
			},
			expectedServer:   server1,
			expectedCAData:   caData1,
			expectedProxyURL: "",
		},
		{
			name: "with proxy url",
			kubeconfig: &clientcmdapi.Config{
				Clusters: map[string]*clientcmdapi.Cluster{
					"test-cluster": {
						Server:                   server2,
						CertificateAuthorityData: caData2,
						ProxyURL:                 proxyURL,
					}},
				// Define a context that connects the auth info and cluster, and set it as the default
				Contexts: map[string]*clientcmdapi.Context{DefaultKubeConfigContext: {
					Cluster:   "test-cluster",
					AuthInfo:  DefaultKubeConfigAuth,
					Namespace: "configuration",
				}},
				CurrentContext: DefaultKubeConfigContext,
				AuthInfos: map[string]*clientcmdapi.AuthInfo{
					DefaultKubeConfigAuth: {
						ClientCertificate: "tls.crt",
						ClientKey:         "tls.key",
					},
				},
			},
			expectedServer:   server2,
			expectedCAData:   caData2,
			expectedProxyURL: proxyURL,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kubeConfig, err := BaseKubeConfigFromBootStrap(c.kubeconfig)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			cluster := kubeConfig.Contexts[DefaultKubeConfigContext].Cluster

			if cluster != "test-cluster" {
				t.Errorf("expect context cluster %s, but %s", "test-cluster",
					kubeConfig.Contexts[DefaultKubeConfigContext].Cluster)
			}

			if c.expectedServer != kubeConfig.Clusters[cluster].Server {
				t.Errorf("expect server %s, but %s", c.expectedServer, kubeConfig.Clusters[cluster].Server)
			}

			if c.expectedProxyURL != kubeConfig.Clusters[cluster].ProxyURL {
				t.Errorf("expect proxy url %s, but %s", c.expectedProxyURL, proxyURL)
			}

			if !reflect.DeepEqual(c.expectedCAData, kubeConfig.Clusters[cluster].CertificateAuthorityData) {
				t.Errorf("expect ca data %v, but %v", c.expectedCAData, kubeConfig.Clusters[cluster].CertificateAuthorityData)
			}
		})
	}
}
