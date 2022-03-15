package utils

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	certificatesv1 "k8s.io/api/certificates/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/restmapper"
	"k8s.io/klog/v2"
	"open-cluster-management.io/addon-framework/pkg/agent"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

var serialNumberLimit = new(big.Int).Lsh(big.NewInt(1), 128)

// DefaultSignerWithExpiry generates a signer func for addon agent to sign the csr using caKey and caData with expiry date.
func DefaultSignerWithExpiry(caKey, caData []byte, duration time.Duration) agent.CSRSignerFunc {
	return func(csr *certificatesv1.CertificateSigningRequest) []byte {
		blockTlsCrt, _ := pem.Decode(caData) // note: the second return value is not error for pem.Decode; it's ok to omit it.
		certs, err := x509.ParseCertificates(blockTlsCrt.Bytes)
		if err != nil {
			klog.Errorf("Failed to parse cert: %v", err)
			return nil
		}

		blockTlsKey, _ := pem.Decode(caKey)

		// For now only PKCS#1 is supported which assures the private key algorithm is RSA.
		// TODO: Compatiblity w/ PKCS#8 key e.g. EC algorithm
		key, err := x509.ParsePKCS1PrivateKey(blockTlsKey.Bytes)
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

func signCSR(csr *certificatesv1.CertificateSigningRequest, caCert *x509.Certificate, caKey *rsa.PrivateKey, duration time.Duration) ([]byte, error) {
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
		return nil, errors.New("PEM block type must be CERTIFICATE REQUEST")
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return nil, err
	}
	return csr, nil
}

// DefaultCSRApprover approve the csr when addon agent uses default group and default user to sign csr.
func DefaultCSRApprover(agentName string) agent.CSRApproveFunc {
	return func(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn, csr *certificatesv1.CertificateSigningRequest) bool {
		defaultGroups := agent.DefaultGroups(cluster.Name, addon.Name)

		defaultUser := agent.DefaultUser(cluster.Name, addon.Name, agentName)

		// check org field and commonName field
		block, _ := pem.Decode(csr.Spec.Request)
		if block == nil || block.Type != "CERTIFICATE REQUEST" {
			klog.Infof("CSR Approve Check Falied csr %q was not recognized: PEM block type is not CERTIFICATE REQUEST", csr.Name)
			return false
		}

		x509cr, err := x509.ParseCertificateRequest(block.Bytes)
		if err != nil {
			klog.Infof("CSR Approve Check Falied csr %q was not recognized: %v", csr.Name, err)
			return false
		}

		requestingOrgs := sets.NewString(x509cr.Subject.Organization...)
		if requestingOrgs.Len() != 3 {
			klog.Infof("CSR Approve Check Falied csr %q org is not equal to 3", csr.Name)
			return false
		}

		for _, group := range defaultGroups {
			if !requestingOrgs.Has(group) {
				klog.Infof("CSR Approve Check Falied csr requesting orgs doesn't contain %s", group)
				return false
			}
		}

		// check commonName field
		if defaultUser != x509cr.Subject.CommonName {
			klog.Infof("CSR Approve Check Falied commonName not right; request %s get %s", x509cr.Subject.CommonName, defaultUser)
			return false
		}

		// check user name
		if strings.HasPrefix(csr.Spec.Username, "system:open-cluster-management:"+cluster.Name) {
			klog.Info("CSR approved")
			return true
		} else {
			klog.Info("CSR not approved due to illegal requester", "requester", csr.Spec.Username)
			return false
		}
	}
}

// UnionCSRApprover is a union func for multiple approvers
func UnionCSRApprover(approvers ...agent.CSRApproveFunc) agent.CSRApproveFunc {
	return func(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn, csr *certificatesv1.CertificateSigningRequest) bool {
		for _, approver := range approvers {
			if !approver(cluster, addon, csr) {
				return false
			}
		}

		return true
	}
}

// IsCSRSupported checks whether the cluster supports v1 or v1beta1 csr api.
func IsCSRSupported(nativeClient kubernetes.Interface) (bool, bool, error) {
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(nativeClient.Discovery()))
	mappings, err := mapper.RESTMappings(schema.GroupKind{
		Group: certificatesv1.GroupName,
		Kind:  "CertificateSigningRequest",
	})
	if err != nil {
		return false, false, err
	}
	v1CSRSupported := false
	for _, mapping := range mappings {
		if mapping.GroupVersionKind.Version == "v1" {
			v1CSRSupported = true
		}
	}
	v1beta1CSRSupported := false
	for _, mapping := range mappings {
		if mapping.GroupVersionKind.Version == "v1beta1" {
			v1beta1CSRSupported = true
		}
	}
	return v1CSRSupported, v1beta1CSRSupported, nil
}
