package helpers

import (
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"

	certificatesv1 "k8s.io/api/certificates/v1"
	"k8s.io/client-go/util/keyutil"
	"k8s.io/klog/v2"
)

type CSRSignerFunc func(csr *certificatesv1.CertificateSigningRequest) []byte

var serialNumberLimit = new(big.Int).Lsh(big.NewInt(1), 128)

// CSRSignerWithExpiry generates a signer func for addon agent to sign the csr using caKey and caData with expiry date.
func CSRSignerWithExpiry(caKey, caData []byte, duration time.Duration) CSRSignerFunc {
	return func(csr *certificatesv1.CertificateSigningRequest) []byte {
		blockTlsCrt, _ := pem.Decode(caData)
		if blockTlsCrt == nil {
			klog.Errorf("Failed to decode cert")
			return nil
		}
		certs, err := x509.ParseCertificates(blockTlsCrt.Bytes)
		if err != nil {
			klog.Errorf("Failed to parse cert: %v", err)
			return nil
		}

		key, err := keyutil.ParsePrivateKeyPEM(caKey)
		if err != nil {
			klog.Errorf("Failed to parse key: %v", err)
			return nil
		}

		data, err := signCSR(csr, certs[0], key, duration)
		if err != nil {
			klog.Errorf("Failed to sign csr: %v", err)
			return nil
		}
		return data
	}
}

func signCSR(csr *certificatesv1.CertificateSigningRequest, caCert *x509.Certificate, caKey any, duration time.Duration) ([]byte, error) {
	certExpiryDuration := duration
	durationUntilExpiry := time.Until(caCert.NotAfter)
	if durationUntilExpiry <= 0 {
		return nil, fmt.Errorf("signer has expired, expired time: %v", caCert.NotAfter)
	}
	if durationUntilExpiry < certExpiryDuration {
		certExpiryDuration = durationUntilExpiry
	}

	request, err := parseCSR(csr.Spec.Request)
	if err != nil {
		return nil, err
	}

	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return nil, fmt.Errorf("unable to generate a serial number for %s: %v", request.Subject.CommonName, err)
	}

	tmpl := &x509.Certificate{
		SerialNumber:       serialNumber,
		Subject:            request.Subject,
		DNSNames:           request.DNSNames,
		IPAddresses:        request.IPAddresses,
		EmailAddresses:     request.EmailAddresses,
		URIs:               request.URIs,
		PublicKeyAlgorithm: request.PublicKeyAlgorithm,
		PublicKey:          request.PublicKey,
		Extensions:         request.Extensions,
		ExtraExtensions:    request.ExtraExtensions,
		// Hard code the usage since it cannot be specified in registration process
		KeyUsage: x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
			x509.ExtKeyUsageClientAuth,
		},
	}

	now := time.Now()
	tmpl.NotBefore = now
	tmpl.NotAfter = now.Add(certExpiryDuration)

	der, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, request.PublicKey, caKey)
	if err != nil {
		return nil, fmt.Errorf("failed to sign certificate: %v", err)
	}

	return pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: der,
	}), nil
}

func parseCSR(pemBytes []byte) (*x509.CertificateRequest, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil || block.Type != "CERTIFICATE REQUEST" {
		return nil, fmt.Errorf("PEM block type must be CERTIFICATE REQUEST")
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return nil, err
	}
	return csr, nil
}
