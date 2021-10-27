package certrotation

import (
	"bytes"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/crypto"
	corev1 "k8s.io/api/core/v1"
)

func TestNeedNewSigningCertKeyPair(t *testing.T) {
	certData1, keyData1, err := newSigningCertKeyPair("signer1", time.Hour*-1)
	if err != nil {
		t.Fatalf("Expected no error, but got: %v", err)
	}

	certData2, keyData2, err := newSigningCertKeyPair("signer2", time.Hour*1)
	if err != nil {
		t.Fatalf("Expected no error, but got: %v", err)
	}

	cases := []struct {
		name           string
		secret         *corev1.Secret
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
			validateReason: expectReason(""),
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			actual := needNewSigningCertKeyPair(c.secret)
			c.validateReason(t, actual)
		})
	}
}

func newSigningCertKeyPair(signerName string, validity time.Duration) (certData, keyData []byte, err error) {
	ca, err := crypto.MakeSelfSignedCAConfigForDuration(signerName, validity)
	if err != nil {
		return nil, nil, err
	}

	certBytes := &bytes.Buffer{}
	keyBytes := &bytes.Buffer{}
	if err := ca.WriteCertConfig(certBytes, keyBytes); err != nil {
		return nil, nil, err
	}

	return certBytes.Bytes(), keyBytes.Bytes(), nil
}
