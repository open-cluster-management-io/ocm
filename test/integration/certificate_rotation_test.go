package integration_test

import (
	"path"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	"open-cluster-management.io/registration/pkg/spoke"
	"open-cluster-management.io/registration/test/integration/util"
)

var _ = ginkgo.Describe("Certificate Rotation", func() {
	ginkgo.It("Certificate should be automatically rotated when it is about to expire", func() {
		var err error

		managedClusterName := "rotationtest-spokecluster"
		hubKubeconfigSecret := "rotationtest-hub-kubeconfig-secret"
		hubKubeconfigDir := path.Join(util.TestDir, "rotationtest", "hub-kubeconfig")

		agentOptions := spoke.SpokeAgentOptions{
			ClusterName:              managedClusterName,
			BootstrapKubeconfig:      bootstrapKubeConfigFile,
			HubKubeconfigSecret:      hubKubeconfigSecret,
			HubKubeconfigDir:         hubKubeconfigDir,
			ClusterHealthCheckPeriod: 1 * time.Minute,
		}

		// run registration agent
		cancel := util.RunAgent("rotationtest", agentOptions, spokeCfg)
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

		// simulate hub cluster admin approve the csr with a short time certificate
		err = authn.ApproveSpokeClusterCSR(kubeClient, managedClusterName, time.Second*20)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// simulate hub cluster admin accept the spokecluster
		err = util.AcceptManagedCluster(clusterClient, managedClusterName)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// the hub kubeconfig secret should be filled after the csr is approved
		gomega.Eventually(func() error {
			if _, err := util.GetFilledHubKubeConfigSecret(kubeClient, testNamespace, hubKubeconfigSecret); err != nil {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// the agent should rotate the certificate because the certificate with a short valid time
		// the hub controller should auto approve it
		gomega.Eventually(func() error {
			if _, err := util.FindAutoApprovedSpokeCSR(kubeClient, managedClusterName); err != nil {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	})
})
