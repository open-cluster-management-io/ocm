package hubclientcert

import (
	"crypto/x509"
	"errors"
	"fmt"
	"strings"
	"time"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
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

func getAgentNameFromCertificates(certData []byte) (string, error) {
	certs, err := certutil.ParseCertsPEM(certData)
	if err != nil {
		return "", fmt.Errorf("unable to parse TLS certificates: %v", err)
	}

	if len(certs) == 0 {
		return "", errors.New("unable to get agent name from client certificates")
	}

	for _, cert := range certs {
		if !strings.HasPrefix(cert.Subject.CommonName, subjectPrefix) {
			continue
		}

		return cert.Subject.CommonName[len(subjectPrefix):], nil
	}

	return "", errors.New("unable to get agent name from client certificates")
}

func nextRotationDeadline(leaf *x509.Certificate) time.Time {
	notAfter := leaf.NotAfter
	totalDuration := float64(notAfter.Sub(leaf.NotBefore))
	deadline := leaf.NotBefore.Add(jitteryDuration(totalDuration))

	return deadline
}

func jitteryDuration(totalDuration float64) time.Duration {
	return wait.Jitter(time.Duration(totalDuration), 0.2) - time.Duration(totalDuration*0.3)
}

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
