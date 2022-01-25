package certrotation

import (
	"context"
	"crypto/x509"
	"fmt"
	"time"

	"github.com/openshift/library-go/pkg/certs"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/openshift/library-go/pkg/crypto"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/util/cert"
)

// TargetRotation rotates a key and cert signed by a CA. It creates a new one when 80%
// of the lifetime of the old cert has passed, or the CA used to signed the old cert is
// gone from the CA bundle.
type TargetRotation struct {
	Namespace     string
	Name          string
	Validity      time.Duration
	HostNames     []string
	Lister        corev1listers.SecretLister
	Client        corev1client.SecretsGetter
	EventRecorder events.Recorder
}

func (c TargetRotation) EnsureTargetCertKeyPair(signingCertKeyPair *crypto.CA, caBundleCerts []*x509.Certificate, fns ...crypto.CertificateExtensionFunc) error {
	originalTargetCertKeyPairSecret, err := c.Lister.Secrets(c.Namespace).Get(c.Name)
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	targetCertKeyPairSecret := originalTargetCertKeyPairSecret.DeepCopy()
	if apierrors.IsNotFound(err) {
		// create an empty one
		targetCertKeyPairSecret = &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: c.Namespace, Name: c.Name}}
	}
	targetCertKeyPairSecret.Type = corev1.SecretTypeTLS

	reason := needNewTargetCertKeyPair(targetCertKeyPairSecret, caBundleCerts, c.HostNames)
	if len(reason) == 0 {
		return nil
	}

	c.EventRecorder.Eventf("TargetUpdateRequired", "%q in %q requires a new target cert/key pair: %v", c.Name, c.Namespace, reason)
	if err := c.setTargetCertKeyPairSecret(targetCertKeyPairSecret, c.Validity, signingCertKeyPair, fns...); err != nil {
		return err
	}

	if _, _, err = resourceapply.ApplySecret(context.TODO(), c.Client, c.EventRecorder, targetCertKeyPairSecret); err != nil {
		return err
	}
	return nil
}

// needNewTargetCertKeyPair returns a reason for creating a new target cert/key pair.
// Return empty if a valid cert/key pair is in place and no need to rotate it yet.
//
// We create a new target cert/key pair if
//   1) no cert/key pair exits
//   2) or the cert expired (then we are also pretty late)
//   3) or we are over the renewal percentage of the validity
//   4) or the CA bundle doesn't contain a CA cert that matches exiting secret's common name.
//   5) or the CA bundle doesn't contain the parent CA cert of the exiting secret.
//   6) or the previously signed SANs in the CA bundle doesn't match expectation
func needNewTargetCertKeyPair(secret *corev1.Secret, caBundleCerts []*x509.Certificate, hostnames []string) string {
	certData := secret.Data["tls.crt"]
	if len(certData) == 0 {
		return "missing tls.crt"
	}

	certificates, err := cert.ParseCertsPEM(certData)
	if err != nil {
		return "bad certificate"
	}
	if len(certificates) == 0 {
		return "missing certificate"
	}

	cert := certificates[0]
	if time.Now().After(cert.NotAfter) {
		return "already expired"
	}

	maxWait := cert.NotAfter.Sub(cert.NotBefore) / 5
	latestTime := cert.NotAfter.Add(-maxWait)
	if time.Now().After(latestTime) {
		return fmt.Sprintf("expired in %6.3f seconds", cert.NotAfter.Sub(time.Now()).Seconds())
	}

	// check the signer common name against all the common names in our ca bundle so we don't refresh early
	containsIssuer := false
	for _, caCert := range caBundleCerts {
		if cert.Issuer.CommonName != caCert.Subject.CommonName {
			continue
		}
		if err := cert.CheckSignatureFrom(caCert); err != nil {
			continue
		}
		containsIssuer = true
	}
	if !containsIssuer {
		return fmt.Sprintf("issuer %q not in ca bundle:\n%s", cert.Issuer.CommonName, certs.CertificateBundleToString(caBundleCerts))
	}

	expectedIPs, expectedHosts := crypto.IPAddressesDNSNames(hostnames)
	currentNames := sets.NewString(cert.DNSNames...)
	if !sets.NewString(expectedHosts...).Equal(currentNames) {
		return fmt.Sprintf("issued hostnames mismatch in ca bundle: (current) %v, (expected) %v", currentNames, expectedHosts)
	}
	currentIPs := sets.NewString()
	for _, ip := range cert.IPAddresses {
		currentIPs.Insert(ip.String())
	}
	expectedStrIPs := sets.NewString()
	for _, ip := range expectedIPs {
		expectedStrIPs.Insert(ip.String())
	}
	if !expectedStrIPs.Equal(currentIPs) {
		return fmt.Sprintf("issued ip addresses mismatch in ca bundle: (current) %v, (expected) %v", currentIPs, expectedIPs)
	}

	return ""
}

// setTargetCertKeyPairSecret creates a new cert/key pair and sets them in the secret.
func (c TargetRotation) setTargetCertKeyPairSecret(targetCertKeyPairSecret *corev1.Secret, validity time.Duration, signer *crypto.CA, fns ...crypto.CertificateExtensionFunc) error {
	if targetCertKeyPairSecret.Data == nil {
		targetCertKeyPairSecret.Data = map[string][]byte{}
	}

	// make sure that we don't specify something past our signer
	targetValidity := validity
	// TODO: When creating a certificate, crypto.MakeServerCertForDuration accetps validity as input parameter,
	// It calls time.Now() as the current time to calculate NotBefore/NotAfter of new certificate, which might
	// be little later than the returned value of time.Now() call in the line below to get remainingSignerValidity.
	// 2 more seconds is added here as a buffer to make sure NotAfter of the new certificate does not past NotAfter
	// of the signing certificate. We may need a better way to handle this.
	remainingSignerValidity := signer.Config.Certs[0].NotAfter.Sub(time.Now().Add(time.Second * 2))
	if remainingSignerValidity < validity {
		targetValidity = remainingSignerValidity
	}
	certKeyPair, err := c.NewCertificate(signer, targetValidity, fns...)
	if err != nil {
		return err
	}
	targetCertKeyPairSecret.Data["tls.crt"], targetCertKeyPairSecret.Data["tls.key"], err = certKeyPair.GetPEMBytes()
	if err != nil {
		return err
	}

	return nil
}

func (c TargetRotation) NewCertificate(signer *crypto.CA, validity time.Duration, fns ...crypto.CertificateExtensionFunc) (*crypto.TLSCertificateConfig, error) {
	if len(c.HostNames) == 0 {
		return nil, fmt.Errorf("no hostnames set")
	}
	return signer.MakeServerCertForDuration(sets.NewString(c.HostNames...), validity, fns...)
}
