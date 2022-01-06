package integration_test

import (
	"fmt"
	"path"
	"reflect"
	"time"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/registration/pkg/spoke"
	"open-cluster-management.io/registration/test/integration/util"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
)

var _ = ginkgo.Describe("Agent Recovery", func() {

	ginkgo.It("agent recovery from invalid bootstrap kubeconfig", func() {
		var err error

		managedClusterName := "bootstrap-recoverytest-spokecluster"

		hubKubeconfigSecret := "bootstrap-recoverytest-hub-kubeconfig-secret"
		hubKubeconfigDir := path.Join(util.TestDir, "bootstrap-recoverytest", "hub-kubeconfig")

		bootstrapFile := path.Join(util.TestDir, "bootstrap-recoverytest", "kubeconfig")
		// create an INVALID bootstrap kubeconfig file with an expired cert
		err = authn.CreateBootstrapKubeConfigWithCertAge(bootstrapFile, serverCertFile, securePort, -1*time.Hour)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// run registration agent with an invalid bootstrap kubeconfig
		agentOptions := spoke.SpokeAgentOptions{
			ClusterName:              managedClusterName,
			BootstrapKubeconfig:      bootstrapFile,
			HubKubeconfigSecret:      hubKubeconfigSecret,
			HubKubeconfigDir:         hubKubeconfigDir,
			ClusterHealthCheckPeriod: 1 * time.Minute,
		}

		cancel := util.RunAgent("bootstrap-recoverytest", agentOptions, spokeCfg)
		defer cancel()

		// the managedcluster should not be created
		retryToGetSpokeClusterTimes := 0
		gomega.Eventually(func() int {
			_, err = util.GetManagedCluster(clusterClient, managedClusterName)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(errors.IsNotFound(err)).Should(gomega.BeTrue())
			retryToGetSpokeClusterTimes = retryToGetSpokeClusterTimes + 1
			return retryToGetSpokeClusterTimes
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNumerically(">=", 3))

		// the csr should not be created
		retryToGetSpokeCSRTimes := 0
		gomega.Eventually(func() int {
			_, err := util.FindUnapprovedSpokeCSR(kubeClient, managedClusterName)
			gomega.Expect(err).To(gomega.HaveOccurred())
			retryToGetSpokeCSRTimes = retryToGetSpokeCSRTimes + 1
			return retryToGetSpokeCSRTimes
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNumerically(">=", 3))

		// recover the invalid bootstrap kubeconfig file
		err = authn.CreateBootstrapKubeConfigWithCertAge(bootstrapFile, serverCertFile, securePort, 24*time.Hour)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// the csr should be created after the bootstrap kubeconfig was recovered
		gomega.Eventually(func() error {
			if _, err := util.FindUnapprovedSpokeCSR(kubeClient, managedClusterName); err != nil {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// the spoke cluster should be created after the bootstrap kubeconfig was recovered
		gomega.Eventually(func() bool {
			if _, err := util.GetManagedCluster(clusterClient, managedClusterName); err != nil {
				return false
			}
			return true
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		// simulate hub cluster admin accept the spoke cluster and approve the csr
		err = util.AcceptManagedCluster(clusterClient, managedClusterName)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		err = authn.ApproveSpokeClusterCSR(kubeClient, managedClusterName, time.Hour*24)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// the hub kubeconfig secret should be filled after the csr is approved
		gomega.Eventually(func() bool {
			if _, err := util.GetFilledHubKubeConfigSecret(kubeClient, testNamespace, hubKubeconfigSecret); err != nil {
				return false
			}
			return true
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

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

	ginkgo.It("agent recovery from invalid hub kubeconfig", func() {
		var err error

		spokeClusterName := "hubkubeconfig-recoverytest-spokecluster"

		hubKubeconfigSecret := "hubkubeconfig-recoverytest-hub-kubeconfig-secret"
		hubKubeconfigDir := path.Join(util.TestDir, "hubkubeconfig-recoverytest", "hub-kubeconfig")

		// run registration agent
		agentOptions := spoke.SpokeAgentOptions{
			ClusterName:              spokeClusterName,
			BootstrapKubeconfig:      bootstrapKubeConfigFile,
			HubKubeconfigSecret:      hubKubeconfigSecret,
			HubKubeconfigDir:         hubKubeconfigDir,
			ClusterHealthCheckPeriod: 1 * time.Minute,
		}

		cancel := util.RunAgent("hubkubeconfig-recoverytest", agentOptions, spokeCfg)
		defer cancel()

		// after bootstrap the spokecluster and csr should be created
		gomega.Eventually(func() bool {
			if _, err := util.GetManagedCluster(clusterClient, spokeClusterName); err != nil {
				return false
			}
			return true
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		var firstCSRName string
		gomega.Eventually(func() bool {
			csr, err := util.FindUnapprovedSpokeCSR(kubeClient, spokeClusterName)
			if err != nil {
				return false
			}
			firstCSRName = csr.Name
			return true
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		// simulate hub cluster admin accept the spoke cluster
		err = util.AcceptManagedCluster(clusterClient, spokeClusterName)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// simulate hub cluster admin approve the csr with an INVALID hub config
		err = authn.ApproveSpokeClusterCSRWithExpiredCert(kubeClient, spokeClusterName)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		var firstHubKubeConfigSecret *corev1.Secret
		// the hub kubeconfig secret should be filled after the csr is approved
		gomega.Eventually(func() error {
			firstHubKubeConfigSecret, err = util.GetFilledHubKubeConfigSecret(kubeClient, testNamespace, hubKubeconfigSecret)
			return err
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// agent should bootstrap again due to the invalid hub config
		var secondCSRName string
		gomega.Eventually(func() bool {
			csr, err := util.FindUnapprovedSpokeCSR(kubeClient, spokeClusterName)
			if err != nil {
				return false
			}
			secondCSRName = csr.Name
			return true
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		// a new csr should be recreated
		gomega.Expect(firstCSRName).ShouldNot(gomega.BeEquivalentTo(secondCSRName))

		// approve the new csr with a valid hub config
		err = authn.ApproveSpokeClusterCSR(kubeClient, spokeClusterName, time.Hour*24)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// wait the hub kubeconfig secret is updated with the valid hub config
		gomega.Eventually(func() bool {
			secondHubKubeConfigSecret, err := util.GetFilledHubKubeConfigSecret(kubeClient, testNamespace, hubKubeconfigSecret)
			if err != nil {
				return false
			}

			// the hub kubeconfig secret should be updated
			if reflect.DeepEqual(firstHubKubeConfigSecret.Data, secondHubKubeConfigSecret.Data) {
				return false
			}

			return true
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		// the spoke cluster should have joined condition finally
		gomega.Eventually(func() error {
			spokeCluster, err := util.GetManagedCluster(clusterClient, spokeClusterName)
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
