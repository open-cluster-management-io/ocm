package clientcert

import (
	"context"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"time"

	"github.com/openshift/library-go/pkg/operator/events"
	certificates "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	certificatesinformers "k8s.io/client-go/informers/certificates"
	certificatesv1informers "k8s.io/client-go/informers/certificates/v1"
	"k8s.io/client-go/kubernetes"
	csrclient "k8s.io/client-go/kubernetes/typed/certificates/v1"
	certificatesv1listers "k8s.io/client-go/listers/certificates/v1"
	"k8s.io/client-go/tools/cache"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	certutil "k8s.io/client-go/util/cert"
	"k8s.io/klog/v2"

	ocmfeature "open-cluster-management.io/api/feature"

	"open-cluster-management.io/ocm/pkg/features"
	"open-cluster-management.io/ocm/pkg/registration/helpers"
)

// HasValidHubKubeconfig checks if there exists a valid client certificate in the given secret
// Returns true if all the conditions below are met:
//  1. KubeconfigFile exists when hasKubeconfig is true
//  2. TLSKeyFile exists
//  3. TLSCertFile exists and the certificate is not expired
//  4. If subject is specified, it matches the subject in the certificate stored in TLSCertFile
func HasValidHubKubeconfig(logger klog.Logger, secret *corev1.Secret, subject *pkix.Name) bool {
	if len(secret.Data) == 0 {
		logger.V(4).Info("No data found in secret", "secret", klog.KObj(secret))
		return false
	}

	if _, ok := secret.Data[KubeconfigFile]; !ok {
		logger.V(4).Info("No specific file found in secret", "file", KubeconfigFile, "secret", klog.KObj(secret))
		return false
	}

	if _, ok := secret.Data[TLSKeyFile]; !ok {
		logger.V(4).Info("No specific key file found in secret", "keyFile", TLSKeyFile, "secret", klog.KObj(secret))
		return false
	}

	certData, ok := secret.Data[TLSCertFile]
	if !ok {
		logger.V(4).Info("No specific cert file found in secret", "certFile", TLSCertFile, "secret", klog.KObj(secret))
		return false
	}

	valid, err := IsCertificateValid(logger, certData, subject)
	if err != nil {
		logger.V(4).Error(err, "Unable to validate certificate in secret", "secret", klog.KObj(secret))
		return false
	}

	return valid
}

// IsCertificateValid return true if
// 1) All certs in client certificate are not expired.
// 2) At least one cert matches the given subject if specified
func IsCertificateValid(logger klog.Logger, certData []byte, subject *pkix.Name) (bool, error) {
	certs, err := certutil.ParseCertsPEM(certData)
	if err != nil {
		return false, errors.New("unable to parse certificate")
	}

	if len(certs) == 0 {
		return false, errors.New("no cert found in certificate")
	}

	now := time.Now()
	// make sure no cert in the certificate chain expired
	for _, cert := range certs {
		if now.After(cert.NotAfter) {
			logger.V(4).Info("Part of the certificate is expired", "expiryDate", cert.NotAfter)
			return false, nil
		}
	}

	if subject == nil {
		return true, nil
	}

	// check subject of certificates
	// if the subject is specified, make sure at least one cert in the certificate chain matches the subject
	for _, cert := range certs {
		if certMatchSubject(cert, subject) {
			return true, nil
		}
	}

	logger.V(4).Info("Certificate is not issued for subject", "commonName", subject.CommonName, "organization",
		subject.Organization, "organizationalUnit", subject.OrganizationalUnit)
	return false, nil
}

func certMatchSubject(cert *x509.Certificate, subject *pkix.Name) bool {
	// check commonName
	if cert.Subject.CommonName != subject.CommonName {
		return false
	}

	// check groups (organization)
	if !sets.New(cert.Subject.Organization...).Equal(sets.New(subject.Organization...)) {
		return false
	}

	// check organizational units
	return sets.New(cert.Subject.OrganizationalUnit...).Equal(sets.New(subject.OrganizationalUnit...))
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
		return nil, nil, errors.New("no cert found in certificate")
	}

	// find out the validity period for all certs in the certificate chain
	var notBefore, notAfter *time.Time
	for index, c := range certs {
		cert := c
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
func BuildKubeconfig(clusterName, server string, caData []byte,
	proxyURL, clientCertPath, clientKeyPath string) clientcmdapi.Config {
	// Build kubeconfig.
	kubeconfig := clientcmdapi.Config{
		// Define a cluster stanza based on the bootstrap kubeconfig.
		Clusters: map[string]*clientcmdapi.Cluster{
			clusterName: {
				Server:                   server,
				InsecureSkipTLSVerify:    false,
				CertificateAuthorityData: caData,
				ProxyURL:                 proxyURL,
			}},
		// Define auth based on the obtained client cert.
		AuthInfos: map[string]*clientcmdapi.AuthInfo{"default-auth": {
			ClientCertificate: clientCertPath,
			ClientKey:         clientKeyPath,
		}},
		// Define a context that connects the auth info and cluster, and set it as the default
		Contexts: map[string]*clientcmdapi.Context{"default-context": {
			Cluster:   clusterName,
			AuthInfo:  "default-auth",
			Namespace: "configuration",
		}},
		CurrentContext: "default-context",
	}

	return kubeconfig
}

type CSRControl interface {
	create(ctx context.Context, recorder events.Recorder, objMeta metav1.ObjectMeta, csrData []byte, signerName string, expirationSeconds *int32) (string, error)
	isApproved(name string) (bool, error)
	getIssuedCertificate(name string) ([]byte, error)

	// Informer is public so we can add indexer outside
	Informer() cache.SharedIndexInformer
}

var _ CSRControl = &v1CSRControl{}

type v1CSRControl struct {
	hubCSRInformer certificatesv1informers.CertificateSigningRequestInformer
	hubCSRLister   certificatesv1listers.CertificateSigningRequestLister
	hubCSRClient   csrclient.CertificateSigningRequestInterface
}

func (v *v1CSRControl) isApproved(name string) (bool, error) {
	csr, err := v.get(name)
	if err != nil {
		return false, err
	}
	v1CSR := csr.(*certificates.CertificateSigningRequest)
	approved := false
	for _, condition := range v1CSR.Status.Conditions {
		if condition.Type == certificates.CertificateDenied {
			return false, nil
		} else if condition.Type == certificates.CertificateApproved {
			approved = true
		}
	}
	return approved, nil
}

func (v *v1CSRControl) getIssuedCertificate(name string) ([]byte, error) {
	csr, err := v.get(name)
	if err != nil {
		return nil, err
	}
	v1CSR := csr.(*certificates.CertificateSigningRequest)
	return v1CSR.Status.Certificate, nil
}

func (v *v1CSRControl) create(ctx context.Context, recorder events.Recorder, objMeta metav1.ObjectMeta, csrData []byte,
	signerName string, expirationSeconds *int32) (string, error) {
	csr := &certificates.CertificateSigningRequest{
		ObjectMeta: objMeta,
		Spec: certificates.CertificateSigningRequestSpec{
			Request: csrData,
			Usages: []certificates.KeyUsage{
				certificates.UsageDigitalSignature,
				certificates.UsageKeyEncipherment,
				certificates.UsageClientAuth,
			},
			SignerName:        signerName,
			ExpirationSeconds: expirationSeconds,
		},
	}

	req, err := v.hubCSRClient.Create(ctx, csr, metav1.CreateOptions{})
	if err != nil {
		return "", err
	}
	recorder.Eventf("CSRCreated", "A csr %q is created", req.Name)
	return req.Name, nil
}

func (v *v1CSRControl) Informer() cache.SharedIndexInformer {
	return v.hubCSRInformer.Informer()
}

func (v *v1CSRControl) get(name string) (metav1.Object, error) {
	csr, err := v.hubCSRLister.Get(name)
	switch {
	case apierrors.IsNotFound(err):
		// fallback to fetching csr from hub apiserver in case it is not cached by informer yet
		csr, err = v.hubCSRClient.Get(context.Background(), name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("unable to get csr %q. It might have already been deleted", name)
		}
	case err != nil:
		return nil, err
	}
	return csr, nil
}

func NewCSRControl(logger klog.Logger, hubCSRInformer certificatesinformers.Interface, hubKubeClient kubernetes.Interface) (CSRControl, error) {
	if features.SpokeMutableFeatureGate.Enabled(ocmfeature.V1beta1CSRAPICompatibility) {
		v1CSRSupported, v1beta1CSRSupported, err := helpers.IsCSRSupported(hubKubeClient)
		if err != nil {
			return nil, err
		}
		if !v1CSRSupported && v1beta1CSRSupported {
			csrCtrl := &v1beta1CSRControl{
				hubCSRInformer: hubCSRInformer.V1beta1().CertificateSigningRequests(),
				hubCSRLister:   hubCSRInformer.V1beta1().CertificateSigningRequests().Lister(),
				hubCSRClient:   hubKubeClient.CertificatesV1beta1().CertificateSigningRequests(),
			}
			logger.Info("Using v1beta1 CSR api to manage spoke client certificate")
			return csrCtrl, nil
		}
	}

	return &v1CSRControl{
		hubCSRInformer: hubCSRInformer.V1().CertificateSigningRequests(),
		hubCSRLister:   hubCSRInformer.V1().CertificateSigningRequests().Lister(),
		hubCSRClient:   hubKubeClient.CertificatesV1().CertificateSigningRequests(),
	}, nil
}
