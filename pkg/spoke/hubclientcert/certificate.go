package hubclientcert

import (
	"context"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"time"

	certificates "k8s.io/api/certificates/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	csrclient "k8s.io/client-go/kubernetes/typed/certificates/v1beta1"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	certutil "k8s.io/client-go/util/cert"
	"k8s.io/client-go/util/keyutil"
	"k8s.io/klog"
)

// HasValidKubeconfig checks if there exists a valid kubeconfig in the given secret
// Returns ture if the conditions below are met:
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

	valid, err := IsCertificatetValid(certData)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("unable to validate certificate in secret %s", secret.Namespace+"/"+secret.Name))
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

// IsCertificatetValid return true if all certs in client certificate are not expired.
// Otherwise return false
func IsCertificatetValid(certData []byte) (bool, error) {
	certs, err := certutil.ParseCertsPEM(certData)
	if err != nil {
		return false, errors.New("unable to parse certificate")
	}

	if len(certs) == 0 {
		klog.V(4).Info("No cert found in certificate")
		return false, nil
	}

	now := time.Now()
	for _, cert := range certs {
		if now.After(cert.NotAfter) {
			klog.V(4).Infof("Part of the certificate is expired: %v", cert.NotAfter)
			return false, nil
		}
	}

	return true, nil
}

// getCertLeaf returns the cert leaf with the given common name
func getCertLeaf(secret *corev1.Secret, commonName string) (*x509.Certificate, error) {
	if secret.Data == nil {
		return nil, fmt.Errorf("no client certificate found in secret %q", secret.Namespace+"/"+secret.Name)
	}

	certData, ok := secret.Data[TLSCertFile]
	if !ok {
		return nil, fmt.Errorf("no client certificate found in secret %q", secret.Namespace+"/"+secret.Name)
	}

	certs, err := certutil.ParseCertsPEM(certData)
	if err != nil {
		return nil, fmt.Errorf("unable to parse TLS certificates: %v", err)
	}

	for _, cert := range certs {
		if cert.Subject.CommonName == commonName {
			return cert, nil
		}
	}

	return nil, fmt.Errorf("no cert leaf found in cert with common name %q", commonName)
}

// buildKubeconfig builds a kubeconfig based on a rest config template with a cert/key pair
func buildKubeconfig(clientConfig *restclient.Config, certPath, keyPath string) clientcmdapi.Config {
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
			ClientCertificate: certPath,
			ClientKey:         keyPath,
		}},
		// Define a context that connects the auth info and cluster, and set it as the default
		Contexts: map[string]*clientcmdapi.Context{"default-context": {
			Cluster:   "default-cluster",
			AuthInfo:  "default-auth",
			Namespace: "default",
		}},
		CurrentContext: "default-context",
	}

	return kubeconfig
}

func createCSR(client csrclient.CertificateSigningRequestInterface, privateKeyData []byte, clusterName, agentName string) (string, error) {
	subject := &pkix.Name{
		Organization: []string{fmt.Sprintf("%s%s", subjectPrefix, clusterName)},
		CommonName:   fmt.Sprintf("%s%s:%s", subjectPrefix, clusterName, agentName),
	}

	privateKey, err := keyutil.ParsePrivateKeyPEM(privateKeyData)
	if err != nil {
		return "", fmt.Errorf("invalid private key for certificate request: %v", err)
	}
	csrData, err := certutil.MakeCSR(privateKey, subject, nil, nil)
	if err != nil {
		return "", fmt.Errorf("unable to generate certificate request: %v", err)
	}

	signerName := certificates.KubeAPIServerClientSignerName

	csr := &certificates.CertificateSigningRequest{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("%s-", clusterName),
			Labels: map[string]string{
				clusterNameAnnotation: clusterName,
			},
		},
		Spec: certificates.CertificateSigningRequestSpec{
			Request: csrData,
			Usages: []certificates.KeyUsage{
				certificates.UsageDigitalSignature,
				certificates.UsageKeyEncipherment,
				certificates.UsageClientAuth,
			},
			SignerName: &signerName,
		},
	}

	req, err := client.Create(context.TODO(), csr, metav1.CreateOptions{})
	if err != nil {
		return "", err
	}
	return req.Name, nil
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
