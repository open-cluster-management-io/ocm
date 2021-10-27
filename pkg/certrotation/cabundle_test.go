package certrotation

import (
	"crypto/x509"
	"reflect"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/crypto"
	corev1 "k8s.io/api/core/v1"
)

func TestManageCABundleConfigMap(t *testing.T) {
	caCert1, err := newCaCert("signer1", time.Hour*1)
	if err != nil {
		t.Fatalf("Expected no error, but got: %v", err)
	}

	caCert2, err := newCaCert("signer2", time.Hour*24)
	if err != nil {
		t.Fatalf("Expected no error, but got: %v", err)
	}

	cases := []struct {
		name            string
		caBundle        []byte
		existingCaCerts []*x509.Certificate
		signerCert      *x509.Certificate
		expectErr       bool
		expectCaNum     int
	}{
		{
			name:      "invalid ca bundle",
			caBundle:  []byte("invalid data"),
			expectErr: true,
		},
		{
			name:        "without existing ca",
			signerCert:  caCert1,
			expectCaNum: 1,
		},
		{
			name:            "with existing ca",
			signerCert:      caCert1,
			existingCaCerts: []*x509.Certificate{caCert2},
			expectCaNum:     2,
		},
		{
			name:            "reduce duplicated",
			signerCert:      caCert1,
			existingCaCerts: []*x509.Certificate{caCert1, caCert2},
			expectCaNum:     2,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			configmap := &corev1.ConfigMap{}
			if len(c.caBundle) > 0 {
				configmap.Data = map[string]string{
					"ca-bundle.crt": string(c.caBundle),
				}
			} else if len(c.existingCaCerts) > 0 {
				caBytes, err := crypto.EncodeCertificates(c.existingCaCerts...)
				if err != nil {
					t.Fatalf("Expected no error, but got: %v", err)
				}
				configmap.Data = map[string]string{
					"ca-bundle.crt": string(caBytes),
				}
			}
			caCerts, err := manageCABundleConfigMap(configmap, c.signerCert)
			switch {
			case err != nil:
				if !c.expectErr {
					t.Fatalf("Expect no error, but got %v", err)
				}
			default:
				if c.expectErr {
					t.Fatalf("Expect an error")
				}

				if len(caCerts) != c.expectCaNum {
					t.Fatalf("Expect %d ca certs, but got %d", c.expectCaNum, len(caCerts))
				}

				if !reflect.DeepEqual(c.signerCert, caCerts[0]) {
					t.Fatalf("Current signer cert should be put at the begining")
				}
			}
		})
	}
}

func newCaCert(signerName string, validity time.Duration) (*x509.Certificate, error) {
	ca, err := crypto.MakeSelfSignedCAConfigForDuration(signerName, validity)
	if err != nil {
		return nil, err
	}

	return ca.Certs[0], nil
}
