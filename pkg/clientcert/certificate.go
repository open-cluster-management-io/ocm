package clientcert

import (
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"strings"
	"time"

	certificatesv1beta1 "k8s.io/api/certificates/v1beta1"
	corev1 "k8s.io/api/core/v1"
	restclient "k8s.io/client-go/rest"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	certutil "k8s.io/client-go/util/cert"
	"k8s.io/klog/v2"
)

// HasValidClientCertificate checks if there exists a valid client certificate in the given secret
// Returns true if all the conditions below are met:
//   1. KubeconfigFile exists when hasKubeconfig is true
//   2. TLSKeyFile exists
//   3. TLSCertFile exists and the certificate is not expired
//   4. If subject is specified, it matches the subject in the certificate stored in TLSCertFile
func HasValidHubKubeconfig(secret *corev1.Secret, subject *pkix.Name) bool {
	if len(secret.Data) == 0 {
		klog.V(4).Infof("No data found in secret %q", secret.Namespace+"/"+secret.Name)
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

	valid, err := IsCertificateValid(certData, subject)
	if err != nil {
		klog.V(4).Infof("Unable to validate certificate in secret %s: %v", secret.Namespace+"/"+secret.Name, err)
		return false
	}

	return valid
}

// IsCertificateValid return true if
// 1) All certs in client certificate are not expired.
// 2) At least one cert matches the given subject if specified
func IsCertificateValid(certData []byte, subject *pkix.Name) (bool, error) {
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

	if subject == nil {
		return true, nil
	}

	// check subject of certificates
	for _, cert := range certs {
		if cert.Subject.CommonName != subject.CommonName {
			continue
		}
		return true, nil
	}

	klog.V(4).Infof("Certificate is not issued for subject (cn=%s; o=%s; ou=%s)",
		subject.CommonName, strings.Join(subject.Organization, ","), strings.Join(subject.OrganizationalUnit, ","))
	return false, nil
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
		return nil, nil, fmt.Errorf("unable to parse TLS certificates: %w", err)
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

// BuildKubeconfig builds a kubeconfig based on a rest config template with a cert/key pair
func BuildKubeconfig(clientConfig *restclient.Config, certPath, keyPath string) clientcmdapi.Config {
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

// isCSRApproved returns true if the given csr has been approved
func isCSRApproved(csr *certificatesv1beta1.CertificateSigningRequest) bool {
	approved := false
	for _, condition := range csr.Status.Conditions {
		if condition.Type == certificatesv1beta1.CertificateDenied {
			return false
		} else if condition.Type == certificatesv1beta1.CertificateApproved {
			approved = true
		}
	}

	return approved
}
