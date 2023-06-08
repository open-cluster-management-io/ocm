package certrotation

import (
	"bytes"
	"crypto/x509"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/crypto"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/util/cert"
)

func TestSetTargetCertKeyPairSecret(t *testing.T) {
	ca1, err := crypto.MakeSelfSignedCAConfigForDuration("signer1", time.Hour*24)
	if err != nil {
		t.Fatalf("Expected no error, but got: %v", err)
	}

	cases := []struct {
		name         string
		validity     time.Duration
		signer       *crypto.CA
		hostNames    []string
		targetSecret *corev1.Secret
		expectErr    bool
	}{
		{
			name:     "no hostname",
			validity: time.Hour * 1,
			signer: &crypto.CA{
				SerialGenerator: &crypto.RandomSerialGenerator{},
				Config:          ca1,
			},
			targetSecret: &corev1.Secret{},
			expectErr:    true,
		},
		{
			name:     "create cert/key pair",
			validity: time.Hour * 1,
			signer: &crypto.CA{
				SerialGenerator: &crypto.RandomSerialGenerator{},
				Config:          ca1,
			},
			hostNames:    []string{"service1.ns1.svc"},
			targetSecret: &corev1.Secret{},
		},
		{
			name:     "truncate validity",
			validity: time.Hour * 100,
			signer: &crypto.CA{
				SerialGenerator: &crypto.RandomSerialGenerator{},
				Config:          ca1,
			},
			hostNames:    []string{"service1.ns1.svc"},
			targetSecret: &corev1.Secret{},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tr := &TargetRotation{
				HostNames: c.hostNames,
			}
			err = tr.setTargetCertKeyPairSecret(c.targetSecret, c.validity, c.signer)

			switch {
			case err != nil:
				if !c.expectErr {
					t.Fatalf("Expect no error, but  got %v", err)
				}
			default:
				if c.expectErr {
					t.Fatalf("Expect an error")
				}

				certData := c.targetSecret.Data["tls.crt"]
				if len(certData) == 0 {
					t.Fatalf("missing tls.crt")
				}

				certificates, err := cert.ParseCertsPEM(certData)
				if err != nil {
					t.Fatalf("bad certificate")
				}
				if len(certificates) == 0 {
					t.Fatalf("missing certificate")
				}

				cert := certificates[0]
				if cert.NotAfter.After(c.signer.Config.Certs[0].NotAfter) {
					t.Fatalf("NotAfter of cert should not over NotAfter of ca")
				}
			}
		})
	}
}

type validateReasonFunc func(t *testing.T, actualReason string)

func TestNeedNewTargetCertKeyPair(t *testing.T) {
	correctHostname := "service1.ns1.svc"
	certData1, keyData1, caData1, _, err := newServingCertKeyPair(correctHostname, "signer1", time.Hour*-1)
	if err != nil {
		t.Fatalf("Expected no error, but got: %v", err)
	}

	certData2, keyData2, caData2, signer2, err := newServingCertKeyPair(correctHostname, "signer2", time.Hour*1)
	if err != nil {
		t.Fatalf("Expected no error, but got: %v", err)
	}

	_, _, caData3, _, err := newServingCertKeyPair(correctHostname, "signer2", time.Hour*1)
	if err != nil {
		t.Fatalf("Expected no error, but got: %v", err)
	}
	wrongHostname := "service2.ns1.svc"
	certData4, keyData4, _, err := newServingCertKeyPairWithSigner(signer2, wrongHostname, "signer2", time.Hour*1)
	if err != nil {
		t.Fatalf("Expected no error, but got: %v", err)
	}

	caCert1, err := cert.ParseCertsPEM(caData1)
	if err != nil {
		t.Fatalf("Expected no error, but got: %v", err)
	}
	caCert2, err := cert.ParseCertsPEM(caData2)
	if err != nil {
		t.Fatalf("Expected no error, but got: %v", err)
	}
	caCert3, err := cert.ParseCertsPEM(caData3)
	if err != nil {
		t.Fatalf("Expected no error, but got: %v", err)
	}

	cases := []struct {
		name           string
		secret         *corev1.Secret
		caBundle       []*x509.Certificate
		validateReason validateReasonFunc
	}{
		{
			name:           "missing tls.crt",
			secret:         &corev1.Secret{},
			validateReason: expectReason("missing tls.crt"),
		},
		{
			name: "bad certificate",
			secret: &corev1.Secret{
				Data: map[string][]byte{
					"tls.crt": []byte("invalid data"),
				},
			},
			validateReason: expectReason("bad certificate"),
		},
		{
			name: "expired",
			secret: &corev1.Secret{
				Data: map[string][]byte{
					"tls.crt": certData1,
					"tls.key": keyData1,
				},
			},
			validateReason: expectReason("already expired"),
		},
		{
			name: "no new cert needed",
			secret: &corev1.Secret{
				Data: map[string][]byte{
					"tls.crt": certData2,
					"tls.key": keyData2,
				},
			},
			caBundle:       caCert2,
			validateReason: expectReason(""),
		},
		{
			name: "no new cert needed - mixed 1",
			secret: &corev1.Secret{
				Data: map[string][]byte{
					"tls.crt": certData2,
					"tls.key": keyData2,
				},
			},
			caBundle:       append(caCert1, caCert2...),
			validateReason: expectReason(""),
		},
		{
			name: "no new cert needed - mixed 2",
			secret: &corev1.Secret{
				Data: map[string][]byte{
					"tls.crt": certData2,
					"tls.key": keyData2,
				},
			},
			caBundle:       append(caCert2, caCert3...),
			validateReason: expectReason(""),
		},
		{
			name: "no issuer found - empty",
			secret: &corev1.Secret{
				Data: map[string][]byte{
					"tls.crt": certData2,
					"tls.key": keyData2,
				},
			},
			validateReason: startsWith(fmt.Sprintf("issuer %q not in ca bundle:\n", "signer2")),
		},
		{
			name: "no issuer found - cn mismatch",
			secret: &corev1.Secret{
				Data: map[string][]byte{
					"tls.crt": certData2,
					"tls.key": keyData2,
				},
			},
			caBundle:       caCert1,
			validateReason: startsWith(fmt.Sprintf("issuer %q not in ca bundle:\n", "signer2")),
		},
		{
			name: "no issuer found - parent mismatch",
			secret: &corev1.Secret{
				Data: map[string][]byte{
					"tls.crt": certData2,
					"tls.key": keyData2,
				},
			},
			caBundle:       caCert3,
			validateReason: startsWith(fmt.Sprintf("issuer %q not in ca bundle:\n", "signer2")),
		},
		{
			name: "issued SAN mismatch",
			secret: &corev1.Secret{
				Data: map[string][]byte{
					"tls.crt": certData4,
					"tls.key": keyData4,
				},
			},
			caBundle:       caCert2,
			validateReason: startsWith("issued hostnames mismatch in ca bundle:"),
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			actual := needNewTargetCertKeyPair(c.secret, c.caBundle, []string{correctHostname})
			c.validateReason(t, actual)
		})
	}
}

func newServingCertKeyPair(hostName, signerName string, validity time.Duration) (certData, keyData, caData []byte, signer *crypto.CA, err error) {
	ca, err := crypto.MakeSelfSignedCAConfigForDuration(signerName, validity)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	signer = &crypto.CA{
		SerialGenerator: &crypto.RandomSerialGenerator{},
		Config:          ca,
	}
	certData, keyData, caData, err = newServingCertKeyPairWithSigner(signer, hostName, signerName, validity)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return certData, keyData, caData, signer, nil
}
func newServingCertKeyPairWithSigner(signer *crypto.CA, hostName, signerName string, validity time.Duration) (certData, keyData, caData []byte, err error) {
	caCertBytes := &bytes.Buffer{}
	caKeyBytes := &bytes.Buffer{}
	if err := signer.Config.WriteCertConfig(caCertBytes, caKeyBytes); err != nil {
		return nil, nil, nil, err
	}

	certKeyPair, err := signer.MakeServerCertForDuration(sets.NewString(hostName), validity)
	if err != nil {
		return nil, nil, nil, err
	}
	certData, keyData, err = certKeyPair.GetPEMBytes()
	if err != nil {
		return nil, nil, nil, err
	}

	return certData, keyData, caCertBytes.Bytes(), nil
}

func expectReason(expectedReason string) validateReasonFunc {
	return func(t *testing.T, actualReason string) {
		if actualReason != expectedReason {
			t.Fatalf("Expect %q, but got %q", expectedReason, actualReason)
		}
	}
}

func startsWith(suffix string) validateReasonFunc {
	return func(t *testing.T, actualReason string) {
		if !strings.HasPrefix(actualReason, suffix) {
			t.Fatalf("Expect reason start with %q, but got %q", suffix, actualReason)
		}
	}
}
