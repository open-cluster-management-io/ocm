package hubclientcert

import (
	"crypto/x509"
	"errors"
	"fmt"
	"strings"
	"time"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	certutil "k8s.io/client-go/util/cert"
)

// isClientConfigStillValid checks the provided kubeconfig to see if it has a valid
// client certificate. It returns true if the kubeconfig is valid.
func isClientCertificateStillValid(certData []byte, clusterName string) (bool, error) {
	certs, err := certutil.ParseCertsPEM(certData)
	if err != nil {
		return false, fmt.Errorf("unable to parse TLS certificates: %v", err)
	}

	if len(certs) == 0 {
		return false, nil
	}

	now := time.Now()
	for _, cert := range certs {
		if now.After(cert.NotAfter) {
			utilruntime.HandleError(fmt.Errorf("part of the client certificate in kubeconfig is expired: %s", cert.NotAfter))
			return false, nil
		}

		if clusterName == "" {
			continue
		}

		if !strings.HasPrefix(cert.Subject.CommonName, subjectPrefix) {
			continue
		}

		prefix := fmt.Sprintf("%s%s:", subjectPrefix, clusterName)
		if !strings.HasPrefix(cert.Subject.CommonName, prefix) {
			utilruntime.HandleError(fmt.Errorf("part of the client certificate has wrong common name: %s", cert.Subject.CommonName))
			return false, nil
		}
	}

	return true, nil
}

// getClusterAgentNamesFromCertificates returns cluster name and agent name parsed
// from commmon name in client certificate.
func getClusterAgentNamesFromCertificates(certData []byte) (clusterName, agentName string, err error) {
	certs, err := certutil.ParseCertsPEM(certData)
	if err != nil {
		return "", "", fmt.Errorf("unable to parse TLS certificates: %v", err)
	}

	if len(certs) == 0 {
		return "", "", errors.New("unable to get agent name from client certificates")
	}

	for _, cert := range certs {
		if !strings.HasPrefix(cert.Subject.CommonName, subjectPrefix) {
			continue
		}

		names := strings.Split(cert.Subject.CommonName[len(subjectPrefix):], ":")
		if len(names) != 2 {
			return "", "", fmt.Errorf("invalid common name %q in certificate", cert.Subject.CommonName)
		}

		return names[0], names[1], nil
	}

	return "", "", errors.New("unable to get agent name from client certificates")
}

// getCertLeaf returns the cert leaf with the given common name
func getCertLeaf(certData []byte, commonName string) (*x509.Certificate, error) {
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
