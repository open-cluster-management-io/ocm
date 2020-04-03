package bootstrap

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/client-go/transport"
	certutil "k8s.io/client-go/util/cert"
	"k8s.io/klog"
)

// writeKubeconfig writes client config as kubeconfig into the given secret
func writeKubeconfig(clientConfig *restclient.Config, certData, keyData []byte, kubeconfigSecret string, coreClient corev1client.CoreV1Interface) error {
	namespace, name, err := splitMetaNamespaceKey(kubeconfigSecret)
	if err != nil {
		return fmt.Errorf("invalid secret name: %s", kubeconfigSecret)
	}

	kubeconfigData, err := buildKubeconfig(clientConfig, certData, keyData)
	if err != nil {
		return err
	}

	found := true
	secret, err := coreClient.Secrets(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		if kerrors.IsNotFound(err) {
			found = false
			secret = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      name,
				},
			}
		} else {
			return err
		}
	}

	if secret.Data == nil {
		secret.Data = make(map[string][]byte)
	}

	// do nothing if kubeconfigData is not changed at all
	if data, exists := secret.Data[kubeconfigSecretDataKey]; exists {
		if reflect.DeepEqual(data, kubeconfigData) {
			return nil
		}
	}

	secret.Data[kubeconfigSecretDataKey] = kubeconfigData

	if found {
		// update secret
		_, err = coreClient.Secrets(namespace).Update(context.Background(), secret, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("unable to write kubeconfig into secret %q: %v", kubeconfigSecret, err)
		}
	} else {
		// create seccret
		_, err = coreClient.Secrets(namespace).Create(context.Background(), secret, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("unable to create secret %q: %v", kubeconfigSecret, err)
		}
	}

	klog.V(4).Infof("write kubeconfig into secret %q", kubeconfigSecret)
	return nil
}

// buildKubeconfig builds kubeconfig based on rest config and a cert/key pair
func buildKubeconfig(clientConfig *restclient.Config, certData, keyData []byte) ([]byte, error) {
	// Get the CA data from the bootstrap client config.
	caFile, caData := clientConfig.CAFile, []byte{}
	if len(caFile) == 0 {
		caData = clientConfig.CAData
	}

	// Build kubeconfig.
	kubeconfig := clientcmdapi.Config{
		// Define a cluster stanza based on the bootstrap kubeconfig.
		Clusters: map[string]*clientcmdapi.Cluster{"default-cluster": {
			Server:                   clientConfig.Host,
			InsecureSkipTLSVerify:    clientConfig.Insecure,
			CertificateAuthority:     caFile,
			CertificateAuthorityData: caData,
		}},
		// Define auth based on the obtained client cert.
		AuthInfos: map[string]*clientcmdapi.AuthInfo{"default-auth": {
			ClientCertificateData: certData,
			ClientKeyData:         keyData,
		}},
		// Define a context that connects the auth info and cluster, and set it as the default
		Contexts: map[string]*clientcmdapi.Context{"default-context": {
			Cluster:   "default-cluster",
			AuthInfo:  "default-auth",
			Namespace: "default",
		}},
		CurrentContext: "default-context",
	}

	return clientcmd.Write(kubeconfig)
}

// loadKubeconfig loads kubeconfig from given secret
func loadKubeconfig(secretKey string, coreClient corev1client.CoreV1Interface) ([]byte, bool, error) {
	namespace, name, err := splitMetaNamespaceKey(secretKey)
	if err != nil {
		return nil, false, fmt.Errorf("invalid secret name: %s", secretKey)
	}

	secret, err := coreClient.Secrets(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		if kerrors.IsNotFound(err) {
			return nil, false, nil
		}
		return nil, false, err
	}

	if secret.Data == nil {
		return nil, false, nil
	}

	if data, exists := secret.Data[kubeconfigSecretDataKey]; exists {
		return data, true, nil
	}

	return nil, false, nil
}

// loadRESTClientConfig loads client config from given secret
func loadRESTClientConfig(secretKey string, coreClient corev1client.CoreV1Interface) (*restclient.Config, error) {
	kubeconfigData, exists, err := loadKubeconfig(secretKey, coreClient)
	if err != nil {
		return nil, err
	}

	if exists {
		return clientcmd.RESTConfigFromKubeConfig(kubeconfigData)
	}

	return nil, fmt.Errorf("either secret %q or data key %q not found", secretKey, kubeconfigSecretDataKey)
}

// isClientConfigStillValid checks the provided kubeconfig to see if it has a valid
// client certificate. It returns true if the kubeconfig is valid, or an error if bootstrapping
// should stop immediately.
func isClientConfigStillValid(kubeconfigData []byte, agentName string) (bool, error) {
	certs, err := getCertsFromKubeconfig(kubeconfigData)
	if err != nil {
		utilruntime.HandleError(err)
		return false, nil
	}

	now := time.Now()
	for _, cert := range certs {
		if now.After(cert.NotAfter) {
			utilruntime.HandleError(fmt.Errorf("part of the client certificate in kubeconfig is expired: %s", cert.NotAfter))
			return false, nil
		}

		if agentName == "" {
			continue
		}

		if !strings.HasPrefix(cert.Subject.CommonName, subjectPrefix) {
			continue
		}

		commonName := fmt.Sprintf("%s%s", subjectPrefix, agentName)
		if cert.Subject.CommonName != commonName {
			utilruntime.HandleError(fmt.Errorf("part of the client certificate has wrong common name: %s", cert.Subject.CommonName))
			return false, nil
		}
	}
	return true, nil
}

// getAgentNameFromKubeconfig returns agent name in the client certificate from kubeconfig
func getAgentNameFromKubeconfig(kubeconfigData []byte) (string, error) {
	certs, err := getCertsFromKubeconfig(kubeconfigData)
	if err != nil {
		return "", err
	}

	for _, cert := range certs {
		if !strings.HasPrefix(cert.Subject.CommonName, subjectPrefix) {
			continue
		}

		return cert.Subject.CommonName[len(subjectPrefix):], nil
	}

	return "", errors.New("no agent name found in certificate from kubeconfig")
}

// getCertsFromKubeconfig returns all certificates found in tls configuration of client configuration
func getCertsFromKubeconfig(kubeconfigData []byte) ([]*x509.Certificate, error) {
	clientConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigData)
	if err != nil {
		return nil, fmt.Errorf("unable to create client config from kubeconfig: %v", err)
	}

	transportConfig, err := clientConfig.TransportConfig()
	if err != nil {
		return nil, fmt.Errorf("unable to load transport configuration from kubeconfig: %v", err)
	}

	// has side effect of populating transport config data fields
	if _, err := transport.TLSConfigFor(transportConfig); err != nil {
		return nil, fmt.Errorf("unable to load TLS configuration from kubeconfig: %v", err)
	}

	certs, err := certutil.ParseCertsPEM(transportConfig.TLS.CertData)
	if err != nil {
		return nil, fmt.Errorf("unable to load TLS certificates from kubeconfig: %v", err)
	}

	if len(certs) == 0 {
		return nil, errors.New("unable to read TLS certificates from kubeconfig")
	}

	return certs, nil
}
