package util

import (
	"context"
	cryptorand "crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"

	"github.com/onsi/ginkgo/v2"

	"github.com/openshift/library-go/pkg/operator/events"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	clientcmdlatest "k8s.io/client-go/tools/clientcmd/api/latest"
	certutil "k8s.io/client-go/util/cert"
)

func NewIntegrationTestEventRecorder(componet string) events.Recorder {
	return &IntegrationTestEventRecorder{component: componet}
}

type IntegrationTestEventRecorder struct {
	component string
}

func (r *IntegrationTestEventRecorder) ComponentName() string {
	return r.component
}

func (r *IntegrationTestEventRecorder) WithContext(ctx context.Context) events.Recorder {
	return r
}

func (r *IntegrationTestEventRecorder) ForComponent(c string) events.Recorder {
	return &IntegrationTestEventRecorder{component: c}
}

func (r *IntegrationTestEventRecorder) WithComponentSuffix(suffix string) events.Recorder {
	return r.ForComponent(fmt.Sprintf("%s-%s", r.ComponentName(), suffix))
}

func (r *IntegrationTestEventRecorder) Event(reason, message string) {
	fmt.Fprintf(ginkgo.GinkgoWriter, "Event: [%s] %v: %v \n", r.component, reason, message)
}

func (r *IntegrationTestEventRecorder) Eventf(reason, messageFmt string, args ...interface{}) {
	r.Event(reason, fmt.Sprintf(messageFmt, args...))
}

func (r *IntegrationTestEventRecorder) Warning(reason, message string) {
	fmt.Fprintf(ginkgo.GinkgoWriter, "Warning: [%s] %v: %v \n", r.component, reason, message)
}

func (r *IntegrationTestEventRecorder) Warningf(reason, messageFmt string, args ...interface{}) {
	r.Warning(reason, fmt.Sprintf(messageFmt, args...))
}

func (r *IntegrationTestEventRecorder) Shutdown() {}

func HasCondition(conditions []metav1.Condition, expectedType, expectedReason string, expectedStatus metav1.ConditionStatus) bool {
	for _, condition := range conditions {
		if condition.Type != expectedType {
			continue
		}

		if condition.Status != expectedStatus {
			return false
		}

		if condition.Reason != expectedReason {
			return false
		}

		return true
	}

	return false
}

func NewKubeConfig(config *rest.Config) []byte {
	configData, _ := runtime.Encode(clientcmdlatest.Codec, &clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{"test-cluster": {
			Server:                config.Host,
			InsecureSkipTLSVerify: true,
		}},
		Contexts: map[string]*clientcmdapi.Context{"test-context": {
			Cluster:  "test-cluster",
			AuthInfo: "test-user",
		}},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{
			"test-user": {
				ClientCertificateData: config.CertData,
				ClientKeyData:         config.KeyData,
			},
		},
		CurrentContext: "test-context",
	})
	return configData
}

func NewCert(notAfter time.Time) []byte {
	caKey, err := rsa.GenerateKey(cryptorand.Reader, 2048)
	if err != nil {
		panic(err)
	}

	caCert, err := certutil.NewSelfSignedCACert(certutil.Config{CommonName: "open-cluster-management.io"}, caKey)
	if err != nil {
		panic(err)
	}

	key, err := rsa.GenerateKey(cryptorand.Reader, 2048)
	if err != nil {
		panic(err)
	}

	certDERBytes, err := x509.CreateCertificate(
		cryptorand.Reader,
		&x509.Certificate{
			Subject: pkix.Name{
				CommonName: "test",
			},
			SerialNumber: big.NewInt(1),
			NotBefore:    caCert.NotBefore,
			NotAfter:     notAfter,
			KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
			ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		},
		caCert,
		key.Public(),
		caKey,
	)
	if err != nil {
		panic(err)
	}

	cert, err := x509.ParseCertificate(certDERBytes)
	if err != nil {
		panic(err)
	}
	return pem.EncodeToMemory(&pem.Block{
		Type:  certutil.CertificateBlockType,
		Bytes: cert.Raw,
	})
}
