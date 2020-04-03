package bootstrap

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"time"

	certificates "k8s.io/api/certificates/v1beta1"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/util/certificate"
	"k8s.io/client-go/util/keyutil"
	"k8s.io/klog"

	certificatesclient "k8s.io/client-go/kubernetes/typed/certificates/v1beta1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
)

const (
	keyExtension  = ".key"
	certExtension = ".crt"
	pemExtension  = ".pem"
	currentPair   = "current"
	updatedPair   = "updated"

	currentAnnotation = "certification.open-cluster-management.io/current"
	tmpPrivateKeyFile = "spoke-cluster-client.key.tmp"
	subjectPrefix     = "system:open-cluster-management:"
)

// NewManager creates a certificate manager.
func NewManager(clientConfig *restclient.Config, agentName string, store certificate.Store) (certificate.Manager, error) {
	clusterName, err := getClusterName(agentName)
	if err != nil {
		return nil, err
	}

	newClientFn := func(current *tls.Certificate) (certificatesclient.CertificateSigningRequestInterface, error) {
		client, err := kubernetes.NewForConfig(clientConfig)
		if err != nil {
			return nil, err
		}
		return client.CertificatesV1beta1().CertificateSigningRequests(), nil
	}

	return certificate.NewManager(&certificate.Config{
		ClientFn: newClientFn,
		Template: &x509.CertificateRequest{
			Subject: pkix.Name{
				CommonName:   fmt.Sprintf("%s%s", subjectPrefix, agentName),
				Organization: []string{fmt.Sprintf("%s%s", subjectPrefix, clusterName)},
			},
		},
		Usages: []certificates.KeyUsage{
			// https://tools.ietf.org/html/rfc5280#section-4.2.1.3
			//
			// DigitalSignature allows the certificate to be used to verify
			// digital signatures including signatures used during TLS
			// negotiation.
			certificates.UsageDigitalSignature,
			// KeyEncipherment allows the cert/key pair to be used to encrypt
			// keys, including the symmetric keys negotiated during TLS setup
			// and used for data transfer..
			certificates.UsageKeyEncipherment,
			// ClientAuth allows the cert to be used by a TLS client to
			// authenticate itself to the TLS server.
			certificates.UsageClientAuth,
		},
		CertificateStore: store,
	})
}

// OnCertUpdateFunc is the calllback function which is invoked once certificate in store is updated
type OnCertUpdateFunc func(certData, keyData []byte) error

// SecretStore extends certificate.Store and supports persistance of temporary private key
type SecretStore interface {
	certificate.Store

	GetOrGenerateTmpPrivateKey() ([]byte, error)
	RemoveTmpPrivateKey() error
}

// secretStore is a concrete implementation of a Store that is based on
// storing the cert/key pairs in the designated secret.
type secretStore struct {
	secretNamespace string
	secretName      string
	pairNamePrefix  string
	certData        []byte
	keyData         []byte
	coreClient      corev1client.CoreV1Interface

	onCertUpdateFunc OnCertUpdateFunc
}

// NewSecretStore returns a concrete implementation of SecretStore.
func NewSecretStore(
	secretKey string,
	pairNamePrefix string,
	coreClient corev1client.CoreV1Interface,
	certData, keyData []byte,
	onCertUpdateFunc OnCertUpdateFunc) (SecretStore, error) {
	namespace, name, err := splitMetaNamespaceKey(secretKey)
	if err != nil {
		return nil, fmt.Errorf("invalid secret name %q: %v", secretKey, err)
	}

	s := secretStore{
		secretNamespace:  namespace,
		secretName:       name,
		pairNamePrefix:   pairNamePrefix,
		certData:         certData,
		keyData:          keyData,
		coreClient:       coreClient,
		onCertUpdateFunc: onCertUpdateFunc,
	}

	return &s, nil
}

// Current returns the current certificate in the store
func (s *secretStore) Current() (*tls.Certificate, error) {
	found := true
	secret, err := s.coreClient.Secrets(s.secretNamespace).Get(context.Background(), s.secretName, metav1.GetOptions{})
	if err != nil {
		if kerrors.IsNotFound(err) {
			found = false
		} else {
			return nil, fmt.Errorf("unable to get secret %q: %v", s.secretNamespace+"/"+s.secretName, err)
		}
	}

	if found {
		cert, exists, err := getCurrentCertificate(secret)
		if err != nil {
			return nil, err
		}
		if exists {
			return cert, nil
		}
	}

	if s.certData != nil && s.keyData != nil {
		klog.V(4).Info("Loading cert/key pair from PEM blocks.")
		return parseCertFromPEMBlocks(s.certData, s.keyData)
	}

	c := s.pairNamePrefix + certExtension
	k := s.pairNamePrefix + keyExtension
	certData, certExists := secret.Data[c]
	keyData, keyExists := secret.Data[k]
	if certExists && keyExists {
		klog.V(4).Infof("Loading cert/key pair: %s, %s.", c, k)
		return parseCertFromPEMBlocks(certData, keyData)
	}

	noKeyErr := certificate.NoCertKeyError(fmt.Sprintf("no cert/key available in secret: %s/%s",
		s.secretNamespace, s.secretName))
	return nil, &noKeyErr
}

// getCurrentCertificate returns the current certificate stored in secret
func getCurrentCertificate(secret *corev1.Secret) (*tls.Certificate, bool, error) {
	current, ok := secret.Annotations[currentAnnotation]
	if !ok {
		return nil, false, nil
	}

	if pemBlock, exists := secret.Data[current]; exists {
		klog.Infof("Loading cert/key pair: %s.", current)
		cert, err := parseCertFromPEMBlocks(pemBlock, pemBlock)
		return cert, true, err
	}

	return nil, false, fmt.Errorf("unable to find PEM Block for current certification: %s", current)
}

// parseCertFromPEMBlocks parses and returns tls certificate from cert/key pem blocks
func parseCertFromPEMBlocks(certPEMBlock, keyPEMBlock []byte) (*tls.Certificate, error) {
	cert, err := tls.X509KeyPair(certPEMBlock, keyPEMBlock)
	if err != nil {
		return nil, fmt.Errorf("could not convert PEM block into cert/key pair: %v", err)
	}
	certs, err := x509.ParseCertificates(cert.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("unable to parse certificate data: %v", err)
	}
	cert.Leaf = certs[0]
	return &cert, nil
}

// Update updates the current certificate in store
func (s *secretStore) Update(certData, keyData []byte) (*tls.Certificate, error) {
	found := true
	secret, err := s.coreClient.Secrets(s.secretNamespace).Get(context.Background(), s.secretName, metav1.GetOptions{})
	if err != nil {
		if kerrors.IsNotFound(err) {
			found = false
			secret = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: s.secretNamespace,
					Name:      s.secretName,
				},
			}
		} else if err != nil {
			return nil, err
		}
	}

	// build pem block for certificate
	var buffer bytes.Buffer
	certBlock, _ := pem.Decode(certData)
	if certBlock == nil {
		return nil, errors.New("invalid certificate data")
	}
	pem.Encode(&buffer, certBlock)
	keyBlock, _ := pem.Decode(keyData)
	if keyBlock == nil {
		return nil, errors.New("invalid key data")
	}
	pem.Encode(&buffer, keyBlock)

	pemBlock := buffer.Bytes()
	cert, err := parseCertFromPEMBlocks(pemBlock, pemBlock)
	if err != nil {
		return nil, err
	}

	// save the certificate in secret and make the annotation 'current' point to it
	ts := time.Now().Format("2006-01-02-15-04-05")
	pemBlockKey := s.filename(ts)
	if secret.Data == nil {
		secret.Data = make(map[string][]byte)
	}
	secret.Data[pemBlockKey] = pemBlock

	if secret.Annotations == nil {
		secret.Annotations = make(map[string]string)
	}
	secret.Annotations[currentAnnotation] = pemBlockKey

	if found {
		// update secret
		_, err = s.coreClient.Secrets(s.secretNamespace).Update(context.Background(), secret, metav1.UpdateOptions{})
		if err != nil {
			return nil, fmt.Errorf("unable to update certificate in secret %q: %v", s.secretNamespace+"/"+s.secretName, err)
		}
	} else {
		// create seccret
		_, err = s.coreClient.Secrets(s.secretNamespace).Create(context.Background(), secret, metav1.CreateOptions{})
		if err != nil {
			return nil, fmt.Errorf("unable to create secret %q: %v", s.secretNamespace+"/"+s.secretName, err)
		}
	}

	// call callback function once cert is updated
	if s.onCertUpdateFunc == nil {
		return cert, nil
	}

	return cert, s.onCertUpdateFunc(certData, keyData)
}

// GetOrGenerateTmpPrivateKey returns temporary private key if it exists; otherwise
// generates a new private key and returns.
func (s *secretStore) GetOrGenerateTmpPrivateKey() ([]byte, error) {
	found := true
	secret, err := s.coreClient.Secrets(s.secretNamespace).Get(context.Background(), s.secretName, metav1.GetOptions{})
	if err != nil {
		if kerrors.IsNotFound(err) {
			found = false
			secret = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: s.secretNamespace,
					Name:      s.secretName,
				},
			}
		} else if err != nil {
			return nil, err
		}
	}

	// reuse existing private key if exists
	if tmpPrivateKey, ok := secret.Data[tmpPrivateKeyFile]; ok {
		klog.V(4).Info("Reuse existing private key")
		return tmpPrivateKey, nil
	}

	// otherwise, create a new private key
	tmpPrivateKey, err := keyutil.MakeEllipticPrivateKeyPEM()
	if err != nil {
		return nil, err
	}

	// save private key in secret
	if secret.Data == nil {
		secret.Data = make(map[string][]byte)
	}
	secret.Data[tmpPrivateKeyFile] = tmpPrivateKey

	if found {
		// update secret
		_, err = s.coreClient.Secrets(s.secretNamespace).Update(context.Background(), secret, metav1.UpdateOptions{})
		if err != nil {
			return nil, fmt.Errorf("unable to save temporary private key in secret %q: %v", s.secretNamespace+"/"+s.secretName, err)
		}
	} else {
		// create seccret
		_, err = s.coreClient.Secrets(s.secretNamespace).Create(context.Background(), secret, metav1.CreateOptions{})
		if err != nil {
			return nil, fmt.Errorf("unable to create secret %q: %v", s.secretNamespace+"/"+s.secretName, err)
		}
	}

	klog.V(4).Info("Create a new private key")
	return tmpPrivateKey, err
}

// RemoveTmpPrivateKey remove the temporary private key from store if it exists
func (s *secretStore) RemoveTmpPrivateKey() error {
	secret, err := s.coreClient.Secrets(s.secretNamespace).Get(context.Background(), s.secretName, metav1.GetOptions{})
	if err != nil {
		if kerrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("unable to get secret %q: %v", s.secretNamespace+"/"+s.secretName, err)
	}

	// remove the temporary private key if exists
	if _, ok := secret.Data[tmpPrivateKeyFile]; ok {
		delete(secret.Data, tmpPrivateKeyFile)
		_, err = s.coreClient.Secrets(s.secretNamespace).Update(context.Background(), secret, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("unable to remove temporary private key from secret %q: %v", s.secretNamespace+"/"+s.secretName, err)
		}
	}

	return nil
}

// filename returns the file name of certificate as data key in secret
func (s *secretStore) filename(qualifier string) string {
	return s.pairNamePrefix + "-" + qualifier + pemExtension
}
