package util

import (
	cryptorand "crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"time"

	certutil "k8s.io/client-go/util/cert"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/cert"
	"open-cluster-management.io/sdk-go/pkg/testing"
)

const RSAPrivateKeyBlockType = "RSA PRIVATE KEY"

type ServerCertPairs struct {
	CA            *x509.Certificate
	CAKey         *rsa.PrivateKey
	ServerTLSCert tls.Certificate
}

type ClientCertPairs struct {
	ClientCert []byte
	ClientKey  []byte
}

func NewServerCertPairs() (*ServerCertPairs, error) {
	caKey, err := rsa.GenerateKey(cryptorand.Reader, 2048)
	if err != nil {
		return nil, err
	}

	caCert, err := certutil.NewSelfSignedCACert(certutil.Config{CommonName: "open-cluster-management.io"}, caKey)
	if err != nil {
		return nil, err
	}

	serverKey, err := rsa.GenerateKey(cryptorand.Reader, 2048)
	if err != nil {
		return nil, err
	}

	serverCertDERBytes, err := x509.CreateCertificate(
		cryptorand.Reader,
		&x509.Certificate{
			Subject: pkix.Name{
				CommonName: "test-server",
			},
			SerialNumber: big.NewInt(1),
			NotBefore:    caCert.NotBefore,
			NotAfter:     caCert.NotAfter,
			KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
			ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
			DNSNames:     []string{"localhost"},
			IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		},
		caCert,
		serverKey.Public(),
		caKey,
	)
	if err != nil {
		return nil, err
	}

	serverCert, err := x509.ParseCertificate(serverCertDERBytes)
	if err != nil {
		return nil, err
	}

	serverTLSCert, err := tls.X509KeyPair(
		pem.EncodeToMemory(&pem.Block{
			Type:  certutil.CertificateBlockType,
			Bytes: serverCert.Raw,
		}), pem.EncodeToMemory(&pem.Block{
			Type:  RSAPrivateKeyBlockType,
			Bytes: x509.MarshalPKCS1PrivateKey(serverKey),
		}),
	)
	if err != nil {
		return nil, err
	}

	return &ServerCertPairs{
		CA:            caCert,
		CAKey:         caKey,
		ServerTLSCert: serverTLSCert,
	}, nil
}

func AppendCAToCertPool(caCert *x509.Certificate) (*x509.CertPool, error) {
	certPool, err := x509.SystemCertPool()
	if err != nil {
		return nil, err
	}

	caPEM := pem.EncodeToMemory(&pem.Block{
		Type:  certutil.CertificateBlockType,
		Bytes: caCert.Raw,
	})

	if ok := certPool.AppendCertsFromPEM(caPEM); !ok {
		return nil, fmt.Errorf("invalid CA")
	}

	return certPool, nil
}

func SignClientCert(caCert *x509.Certificate, caKey *rsa.PrivateKey, d time.Duration) (*ClientCertPairs, error) {
	clientKey, err := rsa.GenerateKey(cryptorand.Reader, 2048)
	if err != nil {
		return nil, err
	}

	now := time.Now()

	clientCertDERBytes, err := x509.CreateCertificate(
		cryptorand.Reader,
		&x509.Certificate{
			Subject: pkix.Name{
				CommonName: "test-client",
			},
			SerialNumber: big.NewInt(1),
			NotBefore:    now.UTC(),
			NotAfter:     now.Add(d).UTC(),
			KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
			ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		},
		caCert,
		clientKey.Public(),
		caKey,
	)
	if err != nil {
		return nil, err
	}

	clientCert, err := x509.ParseCertificate(clientCertDERBytes)
	if err != nil {
		return nil, err
	}

	return &ClientCertPairs{
		ClientCert: pem.EncodeToMemory(&pem.Block{
			Type:  certutil.CertificateBlockType,
			Bytes: clientCert.Raw,
		}),
		ClientKey: pem.EncodeToMemory(&pem.Block{
			Type:  RSAPrivateKeyBlockType,
			Bytes: x509.MarshalPKCS1PrivateKey(clientKey),
		}),
	}, nil
}

func WriteCertToTempFile(cert *x509.Certificate) (string, error) {
	// create temp file and write cert to it
	tmpFile, err := testing.WriteToTempFile("cert-", pem.EncodeToMemory(&pem.Block{
		Type:  certutil.CertificateBlockType,
		Bytes: cert.Raw,
	}))
	if err != nil {
		return "", err
	}

	return tmpFile.Name(), nil
}

func WriteTokenToTempFile(token string) (string, error) {
	// create temp file and write token to it
	tmpFile, err := testing.WriteToTempFile("token-", []byte(token))
	if err != nil {
		return "", err
	}

	return tmpFile.Name(), nil
}

func WriteKeyToTempFile(key any) (string, error) {
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return "", fmt.Errorf("key is not an RSA private key")
	}

	tmpFile, err := testing.WriteToTempFile("key-", pem.EncodeToMemory(&pem.Block{
		Type:  RSAPrivateKeyBlockType,
		Bytes: x509.MarshalPKCS1PrivateKey(rsaKey),
	}))
	if err != nil {
		return "", err
	}

	return tmpFile.Name(), nil
}

func ReloadCerts(clientCertFile, clientKeyFile string) cert.ReloadCerts {
	return func() (*cert.CertConfig, error) {
		certData, err := os.ReadFile(clientCertFile)
		if err != nil {
			return nil, err
		}

		keyData, err := os.ReadFile(clientKeyFile)
		if err != nil {
			return nil, err
		}

		return &cert.CertConfig{ClientCertData: certData, ClientKeyData: keyData}, nil
	}
}
