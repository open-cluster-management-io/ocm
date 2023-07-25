package registration_test

import (
	"fmt"
	"path"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	certificates "k8s.io/api/certificates/v1"
	"k8s.io/apimachinery/pkg/api/meta"

	clusterv1 "open-cluster-management.io/api/cluster/v1"

	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/registration/spoke"
	"open-cluster-management.io/ocm/test/integration/util"
)

var _ = ginkgo.Describe("Cluster Auto Approval", func() {
	ginkgo.It("Cluster should be automatically approved", func() {
		var err error

		managedClusterName := "autoapprovaltest-spokecluster"
		//#nosec G101
		hubKubeconfigSecret := "autoapprovaltest-hub-kubeconfig-secret"
		hubKubeconfigDir := path.Join(util.TestDir, "autoapprovaltest", "hub-kubeconfig")

		bootstrapFile := path.Join(util.TestDir, "bootstrap-autoapprovaltest", "kubeconfig")
		err = authn.CreateBootstrapKubeConfigWithUser(bootstrapFile, serverCertFile, securePort, util.AutoApprovalBootstrapUser)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		agentOptions := &spoke.SpokeAgentOptions{
			BootstrapKubeconfig:      bootstrapFile,
			HubKubeconfigSecret:      hubKubeconfigSecret,
			ClusterHealthCheckPeriod: 1 * time.Minute,
		}
		commOptions := commonoptions.NewAgentOptions()
		commOptions.HubKubeconfigDir = hubKubeconfigDir
		commOptions.SpokeClusterName = managedClusterName

		// run registration agent
		cancel := runAgent("autoapprovaltest", agentOptions, commOptions, spokeCfg)
		defer cancel()

		// after bootstrap the spokecluster should be accepted and its csr should be auto approved
		gomega.Eventually(func() bool {
			cluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
			if err != nil {
				return false
			}

			return cluster.Spec.HubAcceptsClient
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		var approvedCSR *certificates.CertificateSigningRequest
		gomega.Eventually(func() bool {
			approvedCSR, err = util.FindAutoApprovedSpokeCSR(kubeClient, managedClusterName)
			return err == nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

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
