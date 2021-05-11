package integration

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"net"
	"time"

	mathrand "math/rand"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/util/rand"

	addonapiv1alpha1 "github.com/open-cluster-management/api/addon/v1alpha1"
	clusterv1 "github.com/open-cluster-management/api/cluster/v1"
	"github.com/openshift/library-go/pkg/crypto"
	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	spokeClusterNameLabel = "open-cluster-management.io/cluster-name"
	spokeAddonNameLabel   = "open-cluster-management.io/addon-name"
)

var _ = ginkgo.Describe("Addon CSR", func() {
	var managedClusterName string
	var err error

	ginkgo.BeforeEach(func() {
		suffix := rand.String(5)
		managedClusterName = fmt.Sprintf("managedcluster-%s", suffix)

		managedCluster := &clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: managedClusterName,
			},
			Spec: clusterv1.ManagedClusterSpec{
				HubAcceptsClient: true,
			},
		}
		_, err = hubClusterClient.ClusterV1().ManagedClusters().Create(context.Background(), managedCluster, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: managedClusterName}}
		_, err = hubKubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		testAddonImpl.registrations[managedClusterName] = []addonapiv1alpha1.RegistrationConfig{
			{
				SignerName: certificatesv1.KubeAPIServerClientSignerName,
			},
		}

		addon := &addonapiv1alpha1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name: testAddonImpl.name,
			},
			Spec: addonapiv1alpha1.ManagedClusterAddOnSpec{
				InstallNamespace: "test",
			},
		}
		_, err = hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Create(context.Background(), addon, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	ginkgo.AfterEach(func() {
		err = hubKubeClient.CoreV1().Namespaces().Delete(context.Background(), managedClusterName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		err = hubClusterClient.ClusterV1().ManagedClusters().Delete(context.Background(), managedClusterName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		delete(testAddonImpl.registrations, managedClusterName)
	})

	ginkgo.It("Should approve csr successfully", func() {
		testAddonImpl.approveCSR = true
		csr := newCSR(managedClusterName, testAddonImpl.name, certificatesv1.KubeAPIServerClientSignerName)
		_, err = hubKubeClient.CertificatesV1().CertificateSigningRequests().Create(context.Background(), csr, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			actual, err := hubKubeClient.CertificatesV1().CertificateSigningRequests().Get(context.Background(), csr.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}
			approved := false
			for _, condition := range actual.Status.Conditions {
				if condition.Type == certificatesv1.CertificateDenied {
					return fmt.Errorf("csr should be approved")
				} else if condition.Type == certificatesv1.CertificateApproved {
					approved = true
				}
			}

			if !approved {
				return fmt.Errorf("csr should be approved")
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	})

	ginkgo.It("Should sign csr successfully", func() {
		testAddonImpl.approveCSR = true
		signerName := fmt.Sprintf("%s@%d", "open-cluster-management.io/test", time.Now().Unix())
		ca, _ := crypto.MakeSelfSignedCAConfigForDuration(signerName, 1*time.Hour)
		certBytes := &bytes.Buffer{}
		keyBytes := &bytes.Buffer{}
		ca.WriteCertConfig(certBytes, keyBytes)
		testAddonImpl.cert = certBytes.Bytes()

		csr := newCSR(managedClusterName, testAddonImpl.name, "open-cluster-management.io/test")
		_, err = hubKubeClient.CertificatesV1().CertificateSigningRequests().Create(context.Background(), csr, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			actual, err := hubKubeClient.CertificatesV1().CertificateSigningRequests().Get(context.Background(), csr.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}
			approved := false
			for _, condition := range actual.Status.Conditions {
				if condition.Type == certificatesv1.CertificateDenied {
					return fmt.Errorf("csr should be approved")
				} else if condition.Type == certificatesv1.CertificateApproved {
					approved = true
				}
			}

			if !approved {
				return fmt.Errorf("csr should be approved")
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			actual, err := hubKubeClient.CertificatesV1().CertificateSigningRequests().Get(context.Background(), csr.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if !apiequality.Semantic.DeepEqual(actual.Status.Certificate, testAddonImpl.cert) {
				return fmt.Errorf("Expect cert in csr to be signed")
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	})
})

func newCSR(clusterName, addonName, signerName string) *certificatesv1.CertificateSigningRequest {
	insecureRand := mathrand.New(mathrand.NewSource(0))
	pk, err := ecdsa.GenerateKey(elliptic.P256(), insecureRand)
	if err != nil {
		panic(err)
	}
	csrb, err := x509.CreateCertificateRequest(insecureRand, &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName:   "test",
			Organization: []string{"test"},
		},
		DNSNames:       []string{},
		EmailAddresses: []string{},
		IPAddresses:    []net.IP{},
	}, pk)
	if err != nil {
		panic(err)
	}
	return &certificatesv1.CertificateSigningRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("addon-%s", clusterName),
			Labels: map[string]string{
				spokeClusterNameLabel: clusterName,
				spokeAddonNameLabel:   addonName,
			},
		},
		Spec: certificatesv1.CertificateSigningRequestSpec{
			Usages: []certificatesv1.KeyUsage{
				certificatesv1.UsageAny,
			},
			SignerName: signerName,
			Request:    pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrb}),
		},
	}
}
