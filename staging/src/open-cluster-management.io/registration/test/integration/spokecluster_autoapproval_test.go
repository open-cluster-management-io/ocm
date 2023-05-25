package integration_test

import (
	"fmt"
	"path"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	certificates "k8s.io/api/certificates/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/registration/pkg/spoke"
	"open-cluster-management.io/registration/test/integration/util"
)

var _ = ginkgo.Describe("Cluster Auto Approval", func() {
	ginkgo.It("Cluster should be automatically approved", func() {
		var err error

		managedClusterName := "autoapprovaltest-spokecluster"
		hubKubeconfigSecret := "autoapprovaltest-hub-kubeconfig-secret"
		hubKubeconfigDir := path.Join(util.TestDir, "autoapprovaltest", "hub-kubeconfig")

		bootstrapFile := path.Join(util.TestDir, "bootstrap-autoapprovaltest", "kubeconfig")
		err = authn.CreateBootstrapKubeConfigWithUser(bootstrapFile, serverCertFile, securePort, util.AutoApprovalBootstrapUser)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		agentOptions := spoke.SpokeAgentOptions{
			ClusterName:              managedClusterName,
			BootstrapKubeconfig:      bootstrapFile,
			HubKubeconfigSecret:      hubKubeconfigSecret,
			HubKubeconfigDir:         hubKubeconfigDir,
			ClusterHealthCheckPeriod: 1 * time.Minute,
		}

		// run registration agent
		cancel := util.RunAgent("autoapprovaltest", agentOptions, spokeCfg)
		defer cancel()

		gomega.Eventually(func() error {
			if _, err := util.GetManagedCluster(clusterClient, managedClusterName); err != nil {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		var approvedCSR *certificates.CertificateSigningRequest
		// after bootstrap the spokecluster csr should be auto approved
		gomega.Eventually(func() error {
			approvedCSR, err = util.FindAutoApprovedSpokeCSR(kubeClient, managedClusterName)
			if err != nil {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// simulate hub cluster to fill a certificate
		now := time.Now()
		err = authn.FillCertificateToApprovedCSR(kubeClient, approvedCSR, now.UTC(), now.Add(30*time.Second).UTC())
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// the hub kubeconfig secret should be filled after the csr is approved
		gomega.Eventually(func() error {
			if _, err := util.GetFilledHubKubeConfigSecret(kubeClient, testNamespace, hubKubeconfigSecret); err != nil {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// the spoke cluster should have joined condition finally
		gomega.Eventually(func() error {
			spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
			if err != nil {
				return err
			}
			if !meta.IsStatusConditionTrue(spokeCluster.Status.Conditions, clusterv1.ManagedClusterConditionJoined) {
				return fmt.Errorf("cluster should be joined")
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	})
})
