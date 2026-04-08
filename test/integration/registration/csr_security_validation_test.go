package registration_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	cryptorand "crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/rand"
	"path"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	certificatesv1 "k8s.io/api/certificates/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/registration/hub/user"
	registerfactory "open-cluster-management.io/ocm/pkg/registration/register/factory"
	"open-cluster-management.io/ocm/pkg/registration/spoke"
	"open-cluster-management.io/ocm/test/integration/util"
)

var _ = ginkgo.Describe("CSR Security Validation", func() {
	ginkgo.It("Should reject CSR with prefix-matched organization that doesn't exactly match cluster name", func() {
		var err error

		managedClusterName := "securitytest-cluster1"
		//#nosec G101
		hubKubeconfigSecret := "securitytest-hub-kubeconfig-secret"
		hubKubeconfigDir := path.Join(util.TestDir, "securitytest", "hub-kubeconfig")

		agentOptions := &spoke.SpokeAgentOptions{
			BootstrapKubeconfig:      bootstrapKubeConfigFile,
			HubKubeconfigSecret:      hubKubeconfigSecret,
			ClusterHealthCheckPeriod: 1 * time.Minute,
			RegisterDriverOption:     registerfactory.NewOptions(),
		}

		commOptions := commonoptions.NewAgentOptions()
		commOptions.HubKubeconfigDir = hubKubeconfigDir
		commOptions.SpokeClusterName = managedClusterName

		// run registration agent
		cancel := runAgent("securitytest", agentOptions, commOptions, spokeCfg)
		defer cancel()

		// after bootstrap the spokecluster and csr should be created
		gomega.Eventually(func() error {
			if _, err := util.GetManagedCluster(clusterClient, managedClusterName); err != nil {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			if _, err := util.FindUnapprovedSpokeCSR(kubeClient, managedClusterName); err != nil {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// approve the initial CSR and accept the cluster
		err = authn.ApproveSpokeClusterCSR(kubeClient, managedClusterName, 24*time.Hour)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		err = util.AcceptManagedCluster(clusterClient, managedClusterName)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// the hub kubeconfig secret should be filled after the csr is approved
		gomega.Eventually(func() error {
			if _, err := util.GetFilledHubKubeConfigSecret(kubeClient, testNamespace, hubKubeconfigSecret); err != nil {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// Now create a malicious CSR where:
		// - label has correct cluster name: "securitytest-cluster1"
		// - CN has correct cluster name: "system:open-cluster-management:securitytest-cluster1:agent1"
		// - BUT org has prefix-matched name: "system:open-cluster-management:securitytest-cluster1xyz"
		// This should NOT be auto-approved
		maliciousCSR := createMaliciousCSR(
			managedClusterName,
			user.SubjectPrefix+managedClusterName+":agent1",
			[]string{user.SubjectPrefix + managedClusterName + "xyz", user.ManagedClustersGroup},
		)

		_, err = kubeClient.CertificatesV1().CertificateSigningRequests().Create(
			context.Background(),
			maliciousCSR,
			metav1.CreateOptions{},
		)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// Wait and verify the malicious CSR is NOT auto-approved
		// The CSR should remain unapproved for at least 10 seconds
		gomega.Consistently(func() bool {
			csr, err := kubeClient.CertificatesV1().CertificateSigningRequests().Get(
				context.Background(),
				maliciousCSR.Name,
				metav1.GetOptions{},
			)
			if err != nil {
				return false
			}
			// Check if CSR is still unapproved
			return len(csr.Status.Conditions) == 0
		}, 10*time.Second, 1*time.Second).Should(gomega.BeTrue(), "Malicious CSR should NOT be auto-approved")

		// Clean up the malicious CSR
		err = kubeClient.CertificatesV1().CertificateSigningRequests().Delete(
			context.Background(),
			maliciousCSR.Name,
			metav1.DeleteOptions{},
		)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("Should reject CSR where org and CN agree on wrong cluster name", func() {
		var err error

		managedClusterName := "securitytest-cluster2"
		//#nosec G101
		hubKubeconfigSecret := "securitytest2-hub-kubeconfig-secret"
		hubKubeconfigDir := path.Join(util.TestDir, "securitytest2", "hub-kubeconfig")

		agentOptions := &spoke.SpokeAgentOptions{
			BootstrapKubeconfig:      bootstrapKubeConfigFile,
			HubKubeconfigSecret:      hubKubeconfigSecret,
			ClusterHealthCheckPeriod: 1 * time.Minute,
			RegisterDriverOption:     registerfactory.NewOptions(),
		}

		commOptions := commonoptions.NewAgentOptions()
		commOptions.HubKubeconfigDir = hubKubeconfigDir
		commOptions.SpokeClusterName = managedClusterName

		// run registration agent
		cancel := runAgent("securitytest2", agentOptions, commOptions, spokeCfg)
		defer cancel()

		// after bootstrap the spokecluster and csr should be created
		gomega.Eventually(func() error {
			if _, err := util.GetManagedCluster(clusterClient, managedClusterName); err != nil {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			if _, err := util.FindUnapprovedSpokeCSR(kubeClient, managedClusterName); err != nil {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// approve the initial CSR and accept the cluster
		err = authn.ApproveSpokeClusterCSR(kubeClient, managedClusterName, 24*time.Hour)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		err = util.AcceptManagedCluster(clusterClient, managedClusterName)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// the hub kubeconfig secret should be filled after the csr is approved
		gomega.Eventually(func() error {
			if _, err := util.GetFilledHubKubeConfigSecret(kubeClient, testNamespace, hubKubeconfigSecret); err != nil {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// Now create a malicious CSR where:
		// - label has correct cluster name: "securitytest-cluster2"
		// - BUT both CN and org have a different cluster name with prefix match: "securitytest-cluster2xyz"
		// This should NOT be auto-approved
		wrongClusterName := managedClusterName + "xyz"
		maliciousCSR := createMaliciousCSR(
			managedClusterName, // label still has correct name
			user.SubjectPrefix+wrongClusterName+":agent1",
			[]string{user.SubjectPrefix + wrongClusterName, user.ManagedClustersGroup},
		)

		_, err = kubeClient.CertificatesV1().CertificateSigningRequests().Create(
			context.Background(),
			maliciousCSR,
			metav1.CreateOptions{},
		)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// Wait and verify the malicious CSR is NOT auto-approved
		gomega.Consistently(func() bool {
			csr, err := kubeClient.CertificatesV1().CertificateSigningRequests().Get(
				context.Background(),
				maliciousCSR.Name,
				metav1.GetOptions{},
			)
			if err != nil {
				return false
			}
			// Check if CSR is still unapproved
			return len(csr.Status.Conditions) == 0
		}, 10*time.Second, 1*time.Second).Should(gomega.BeTrue(), "Malicious CSR should NOT be auto-approved")

		// Clean up the malicious CSR
		err = kubeClient.CertificatesV1().CertificateSigningRequests().Delete(
			context.Background(),
			maliciousCSR.Name,
			metav1.DeleteOptions{},
		)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})
})

// createMaliciousCSR creates a CSR with custom label, CN, and organizations for testing
func createMaliciousCSR(clusterName, commonName string, orgs []string) *certificatesv1.CertificateSigningRequest {
	insecureRand := rand.New(rand.NewSource(time.Now().UnixNano())) //nolint:gosec
	pk, err := ecdsa.GenerateKey(elliptic.P256(), insecureRand)
	if err != nil {
		panic(err)
	}

	csrb, err := x509.CreateCertificateRequest(cryptorand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName:   commonName,
			Organization: orgs,
		},
	}, pk)
	if err != nil {
		panic(err)
	}

	csrName := fmt.Sprintf("malicious-csr-%d", time.Now().UnixNano())
	return &certificatesv1.CertificateSigningRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name: csrName,
			Labels: map[string]string{
				"open-cluster-management.io/cluster-name": clusterName,
			},
		},
		Spec: certificatesv1.CertificateSigningRequestSpec{
			Username: user.SubjectPrefix + clusterName + ":agent1",
			Request: pem.EncodeToMemory(&pem.Block{
				Type:  "CERTIFICATE REQUEST",
				Bytes: csrb,
			}),
			SignerName: certificatesv1.KubeAPIServerClientSignerName,
			Usages: []certificatesv1.KeyUsage{
				certificatesv1.UsageDigitalSignature,
				certificatesv1.UsageKeyEncipherment,
				certificatesv1.UsageClientAuth,
			},
		},
	}
}
