package util

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	clientcmdlatest "k8s.io/client-go/tools/clientcmd/api/latest"
)

func NewKubeConfig(config *rest.Config) []byte {
	configData, _ := runtime.Encode(clientcmdlatest.Codec, &clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{"test-cluster": {
			Server:                config.Host,
			InsecureSkipTLSVerify: true,
		}},
		Contexts: map[string]*clientcmdapi.Context{"test-context": {
			Cluster:  "test-cluster",
			AuthInfo: "test-user",
		}},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{
			"test-user": {
				ClientCertificateData: config.CertData,
				ClientKeyData:         config.KeyData,
			},
		},
		CurrentContext: "test-context",
	})
	return configData
}

func CreateKubeconfigFile(clientConfig *rest.Config, filename string) error {
	// Build kubeconfig.
	kubeconfig := clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{"default-cluster": {
			Server:                   clientConfig.Host,
			InsecureSkipTLSVerify:    clientConfig.Insecure,
			CertificateAuthorityData: clientConfig.CAData,
		}},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{"default-auth": {
			ClientCertificate:     clientConfig.CertFile,
			ClientCertificateData: clientConfig.CertData,
			ClientKey:             clientConfig.KeyFile,
			ClientKeyData:         clientConfig.KeyData,
		}},
		Contexts: map[string]*clientcmdapi.Context{"default-context": {
			Cluster:   "default-cluster",
			AuthInfo:  "default-auth",
			Namespace: "configuration",
		}},
		CurrentContext: "default-context",
	}

	return clientcmd.WriteToFile(kubeconfig, filename)
}
