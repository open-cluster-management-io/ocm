package hubclientcert

import (
	"context"
	"crypto"
	"crypto/sha512"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"reflect"
	"time"

	certificates "k8s.io/api/certificates/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	certificatesclient "k8s.io/client-go/kubernetes/typed/certificates/v1beta1"
	certificatesv1beta1 "k8s.io/client-go/kubernetes/typed/certificates/v1beta1"
	certutil "k8s.io/client-go/util/cert"
	"k8s.io/client-go/util/keyutil"
	"k8s.io/klog"
)

const (
	clusterNameAnnotation = "open-cluster-management.io/cluster-name"
)

// requestNodeCertificate will create a bootstrap certificate signing request for cluster
func requestClusterCertificate(client certificatesv1beta1.CertificateSigningRequestInterface, privateKeyData []byte, clusterName, agentName string, renewal bool) (string, error) {
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

	usages := []certificates.KeyUsage{
		certificates.UsageDigitalSignature,
		certificates.UsageKeyEncipherment,
		certificates.UsageClientAuth,
	}

	var name string
	if !renewal {
		// The Signer interface contains the Public() method to get the public key.
		signer, ok := privateKey.(crypto.Signer)
		if !ok {
			return "", fmt.Errorf("private key does not implement crypto.Signer")
		}

		name, err = digestedName(signer.Public(), subject, usages)
		if err != nil {
			return "", err
		}
	}

	req, created, err := RequestCertificate(client, csrData, name, certificates.KubeAPIServerClientSignerName, usages, privateKey, clusterName)
	if err != nil {
		return "", err
	}
	if created {
		klog.V(4).Infof("Create csr for cluster %q: %s", clusterName, name)
	} else {
		klog.V(4).Infof("Reuse existing csr for cluster %q: %s", clusterName, name)
	}

	return req.Name, nil
}

// This digest should include all the relevant pieces of the CSR we care about.
// We can't directly hash the serialized CSR because of random padding that we
// regenerate every loop and we include usages which are not contained in the
// CSR. This needs to be kept up to date as we add new fields to the cluster
// certificates and with ensureCompatible.
func digestedName(publicKey interface{}, subject *pkix.Name, usages []certificates.KeyUsage) (string, error) {
	hash := sha512.New512_256()

	// Here we make sure two different inputs can't write the same stream
	// to the hash. This delimiter is not in the base64.URLEncoding
	// alphabet so there is no way to have spill over collisions. Without
	// it 'CN:foo,ORG:bar' hashes to the same value as 'CN:foob,ORG:ar'
	const delimiter = '|'
	encode := base64.RawURLEncoding.EncodeToString

	write := func(data []byte) {
		hash.Write([]byte(encode(data)))
		hash.Write([]byte{delimiter})
	}

	publicKeyData, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return "", err
	}
	write(publicKeyData)

	write([]byte(subject.CommonName))
	for _, v := range subject.Organization {
		write([]byte(v))
	}
	for _, v := range usages {
		write([]byte(v))
	}

	return fmt.Sprintf("cluster-csr-%s", encode(hash.Sum(nil))), nil
}

// RequestCertificate will either use an existing (if this process has run
// before but not to completion) or create a certificate signing request using the
// PEM encoded CSR and send it to API server, then it will watch the object's
// status, once approved by API server, it will return the API server's issued
// certificate (pem-encoded). If there is any errors, or the watch timeouts, it
// will return an error.
func RequestCertificate(client certificatesclient.CertificateSigningRequestInterface, csrData []byte, name string, signerName string, usages []certificates.KeyUsage, privateKey interface{}, clusterName string) (req *certificates.CertificateSigningRequest, created bool, err error) {
	csr := &certificates.CertificateSigningRequest{
		// Username, UID, Groups will be injected by API server.
		TypeMeta: metav1.TypeMeta{Kind: "CertificateSigningRequest"},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				clusterNameAnnotation: clusterName,
			},
		},
		Spec: certificates.CertificateSigningRequestSpec{
			Request:    csrData,
			Usages:     usages,
			SignerName: &signerName,
		},
	}
	if len(csr.Name) == 0 {
		csr.GenerateName = "csr-"
	}

	req, err = client.Create(context.TODO(), csr, metav1.CreateOptions{})
	switch {
	case err == nil:
	case errors.IsAlreadyExists(err) && len(name) > 0:
		req, err = client.Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			return nil, false, formatError("cannot retrieve certificate signing request: %v", err)
		}
		if err := ensureCompatible(req, csr, privateKey); err != nil {
			return nil, false, fmt.Errorf("csr for this cluster already exists but is not valid: %v", err)
		}
		return req, false, nil
	default:
		return nil, false, formatError("cannot create certificate signing request: %v", err)
	}
	return req, true, nil
}

// ensureCompatible ensures that a CSR object is compatible with an original CSR
func ensureCompatible(new, orig *certificates.CertificateSigningRequest, privateKey interface{}) error {
	newCSR, err := parseCSR(new)
	if err != nil {
		return fmt.Errorf("unable to parse new csr: %v", err)
	}
	origCSR, err := parseCSR(orig)
	if err != nil {
		return fmt.Errorf("unable to parse original csr: %v", err)
	}
	if !reflect.DeepEqual(newCSR.Subject, origCSR.Subject) {
		return fmt.Errorf("csr subjects differ: new: %#v, orig: %#v", newCSR.Subject, origCSR.Subject)
	}
	if new.Spec.SignerName != nil && orig.Spec.SignerName != nil && *new.Spec.SignerName != *orig.Spec.SignerName {
		return fmt.Errorf("csr signerNames differ: new %q, orig: %q", *new.Spec.SignerName, *orig.Spec.SignerName)
	}
	signer, ok := privateKey.(crypto.Signer)
	if !ok {
		return fmt.Errorf("privateKey is not a signer")
	}
	newCSR.PublicKey = signer.Public()
	if err := newCSR.CheckSignature(); err != nil {
		return fmt.Errorf("error validating signature new CSR against old key: %v", err)
	}
	if len(new.Status.Certificate) > 0 {
		certs, err := certutil.ParseCertsPEM(new.Status.Certificate)
		if err != nil {
			return fmt.Errorf("error parsing signed certificate for CSR: %v", err)
		}
		now := time.Now()
		for _, cert := range certs {
			if now.After(cert.NotAfter) {
				return fmt.Errorf("one of the certificates for the CSR has expired: %s", cert.NotAfter)
			}
		}
	}
	return nil
}

// parseCSR extracts the CSR from the API object and decodes it.
func parseCSR(obj *certificates.CertificateSigningRequest) (*x509.CertificateRequest, error) {
	// extract PEM from request object
	block, _ := pem.Decode(obj.Spec.Request)
	if block == nil || block.Type != "CERTIFICATE REQUEST" {
		return nil, fmt.Errorf("PEM block type must be CERTIFICATE REQUEST")
	}
	return x509.ParseCertificateRequest(block.Bytes)
}

// formatError preserves the type of an API message but alters the message. Expects
// a single argument format string, and returns the wrapped error.
func formatError(format string, err error) error {
	if s, ok := err.(errors.APIStatus); ok {
		se := &errors.StatusError{ErrStatus: s.Status()}
		se.ErrStatus.Message = fmt.Sprintf(format, se.ErrStatus.Message)
		return se
	}
	return fmt.Errorf(format, err)
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
