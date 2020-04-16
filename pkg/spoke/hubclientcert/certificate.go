package hubclientcert

import (
	"errors"
	"fmt"
	"time"

	certificates "k8s.io/api/certificates/v1beta1"
	corev1 "k8s.io/api/core/v1"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	certutil "k8s.io/client-go/util/cert"
	"k8s.io/klog"
)

// HasValidKubeconfig checks if there exists a valid kubeconfig in the given secret
// Returns true if the conditions below are met:
//   1. KubeconfigFile exists
//   2. TLSKeyFile exists
//   3. TLSCertFile exists and the certificate is not expired
func hasValidKubeconfig(secret *corev1.Secret) bool {
	if secret.Data == nil {
		klog.V(4).Infof("No kubeconfig found in secret %q", secret.Namespace+"/"+secret.Name)
		return false
	}

	if _, ok := secret.Data[KubeconfigFile]; !ok {
		klog.V(4).Infof("No %q found in secret %q", KubeconfigFile, secret.Namespace+"/"+secret.Name)
		return false
	}

	if _, ok := secret.Data[TLSKeyFile]; !ok {
		klog.V(4).Infof("No %q found in secret %q", TLSKeyFile, secret.Namespace+"/"+secret.Name)
		return false
	}

	certData, ok := secret.Data[TLSCertFile]
	if !ok {
		klog.V(4).Infof("No %q found in secret %q", TLSCertFile, secret.Namespace+"/"+secret.Name)
		return false
	}

	valid, err := IsCertificateValid(certData)
	if err != nil {
		klog.V(4).Infof("unable to validate certificate in secret %s: %v", secret.Namespace+"/"+secret.Name, err)
		return false
	}

	return valid
}

// LoadClientConfig loads client config from a kubeconfig file
func LoadClientConfig(kubeconfigPath string) (*restclient.Config, error) {
	// clientcmd.RESTConfigFromKubeConfig() and clientcmd.NewClientConfigFromBytes()
	// do not handle very well when the kubeconfig file references cert/key files
	// with relative file paths
	loader := &clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfigPath}
	loadedConfig, err := loader.Load()
	if err != nil {
		return nil, err
	}

	return clientcmd.NewNonInteractiveClientConfig(
		*loadedConfig,
		loadedConfig.CurrentContext,
		&clientcmd.ConfigOverrides{},
		loader,
	).ClientConfig()
}

// IsCertificateValid return true if all certs in client certificate are not expired.
// Otherwise return false
func IsCertificateValid(certData []byte) (bool, error) {
	certs, err := certutil.ParseCertsPEM(certData)
	if err != nil {
		return false, errors.New("unable to parse certificate")
	}

	if len(certs) == 0 {
		return false, errors.New("No cert found in certificate")
	}

	now := time.Now()
	// make sure no cert in the certificate chain expired
	for _, cert := range certs {
		if now.After(cert.NotAfter) {
			klog.V(4).Infof("Part of the certificate is expired: %v", cert.NotAfter)
			return false, nil
		}
	}

	return true, nil
}

// getCertValidityPeriod returns the validity period of the client certificate in the secret
func getCertValidityPeriod(secret *corev1.Secret) (*time.Time, *time.Time, error) {
	if secret.Data == nil {
		return nil, nil, fmt.Errorf("no client certificate found in secret %q", secret.Namespace+"/"+secret.Name)
	}

	certData, ok := secret.Data[TLSCertFile]
	if !ok {
		return nil, nil, fmt.Errorf("no client certificate found in secret %q", secret.Namespace+"/"+secret.Name)
	}

	certs, err := certutil.ParseCertsPEM(certData)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to parse TLS certificates: %v", err)
	}

	if len(certs) == 0 {
		return nil, nil, errors.New("No cert found in certificate")
	}

	// find out the validity period for all certs in the certificate chain
	var notBefore, notAfter *time.Time
	for index, cert := range certs {
		if index == 0 {
			notBefore = &cert.NotBefore
			notAfter = &cert.NotAfter
			continue
		}

		if notBefore.Before(cert.NotBefore) {
			notBefore = &cert.NotBefore
		}

		if notAfter.After(cert.NotAfter) {
			notAfter = &cert.NotAfter
		}
	}

	return notBefore, notAfter, nil
}

// buildKubeconfig builds a kubeconfig based on a rest config template with a cert/key pair
func buildKubeconfig(clientConfig *restclient.Config, certPath, keyPath string) clientcmdapi.Config {
	// Build kubeconfig.
	kubeconfig := clientcmdapi.Config{
		// Define a cluster stanza based on the bootstrap kubeconfig.
		Clusters: map[string]*clientcmdapi.Cluster{"default-cluster": {
			Server:                   clientConfig.Host,
			InsecureSkipTLSVerify:    false,
			CertificateAuthorityData: clientConfig.CAData,
		}},
		// Define auth based on the obtained client cert.
		AuthInfos: map[string]*clientcmdapi.AuthInfo{"default-auth": {
			ClientCertificate: certPath,
			ClientKey:         keyPath,
		}},
		// Define a context that connects the auth info and cluster, and set it as the default
		Contexts: map[string]*clientcmdapi.Context{"default-context": {
			Cluster:   "default-cluster",
			AuthInfo:  "default-auth",
			Namespace: "configuration",
		}},
		CurrentContext: "default-context",
	}

	return kubeconfig
}

func isCSRApproved(csr *certificates.CertificateSigningRequest) bool {
	approved := false
	for _, condition := range csr.Status.Conditions {
		if condition.Type == certificates.CertificateDenied {
			return false
		} else if condition.Type == certificates.CertificateApproved {
			approved = true
		}
	}

	return approved
}
