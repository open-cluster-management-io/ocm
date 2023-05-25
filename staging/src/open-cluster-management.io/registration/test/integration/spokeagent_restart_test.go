package integration_test

import (
	"context"
	"fmt"
	"path"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/registration/pkg/spoke"
	"open-cluster-management.io/registration/test/integration/util"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = ginkgo.Describe("Agent Restart", func() {

	ginkgo.It("restart agent", func() {
		var err error
		managedClusterName := "restart-test-cluster1"

		hubKubeconfigSecret := "restart-test-hub-kubeconfig-secret"
		hubKubeconfigDir := path.Join(util.TestDir, "restart-test", "hub-kubeconfig")

		bootstrapFile := path.Join(util.TestDir, "restart-test", "kubeconfig")

		ginkgo.By("Create bootstrap kubeconfig")
		err = authn.CreateBootstrapKubeConfigWithCertAge(bootstrapFile, serverCertFile, securePort, 20*time.Second)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("run registration agent")
		agentOptions := spoke.SpokeAgentOptions{
			ClusterName:              managedClusterName,
			BootstrapKubeconfig:      bootstrapFile,
			HubKubeconfigSecret:      hubKubeconfigSecret,
			HubKubeconfigDir:         hubKubeconfigDir,
			ClusterHealthCheckPeriod: 1 * time.Minute,
		}

		stopAgent := util.RunAgent("restart-test", agentOptions, spokeCfg)

		ginkgo.By("Check existence of csr and ManagedCluster")
		// the csr should be created
		gomega.Eventually(func() bool {
			if _, err := util.FindUnapprovedSpokeCSR(kubeClient, managedClusterName); err != nil {
				return false
			}
			return true
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		// the spoke cluster should be created
		gomega.Eventually(func() bool {
			if _, err := util.GetManagedCluster(clusterClient, managedClusterName); err != nil {
				return false
			}
			return true
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		ginkgo.By("Accept ManagedCluster and approve csr")
		err = util.AcceptManagedCluster(clusterClient, managedClusterName)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		err = authn.ApproveSpokeClusterCSR(kubeClient, managedClusterName, time.Second*20)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Check if hub kubeconfig secret is updated")
		// the hub kubeconfig secret should be filled after the csr is approved
		gomega.Eventually(func() bool {
			if _, err := util.GetFilledHubKubeConfigSecret(kubeClient, testNamespace, hubKubeconfigSecret); err != nil {
				return false
			}
			return true
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		ginkgo.By("Check if ManagedCluster joins the hub")
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

		ginkgo.By("Stop registration agent and wait for a grace period")
		stopAgent()
		time.Sleep(5 * time.Second)

		// remove the join condition. A new join condition will be added once the registration agent
		// is restarted successfully
		spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		conditions := []metav1.Condition{}
		for _, condition := range spokeCluster.Status.Conditions {
			if condition.Type == clusterv1.ManagedClusterConditionJoined {
				continue
			}
			conditions = append(conditions, condition)
		}
		spokeCluster.Status.Conditions = conditions
		_, err = clusterClient.ClusterV1().ManagedClusters().UpdateStatus(context.TODO(), spokeCluster, metav1.UpdateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Restart registration agent")
		agentOptions = spoke.SpokeAgentOptions{
			ClusterName:              managedClusterName,
			BootstrapKubeconfig:      bootstrapFile,
			HubKubeconfigSecret:      hubKubeconfigSecret,
			HubKubeconfigDir:         hubKubeconfigDir,
			ClusterHealthCheckPeriod: 1 * time.Minute,
		}
		stopAgent = util.RunAgent("restart-test", agentOptions, spokeCfg)
		defer stopAgent()

		ginkgo.By("Check if ManagedCluster joins the hub")
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

		ginkgo.By("Check the existence of the renewal csr")
		// The renewal csr is approved automaically on hub, which indicates the
		// cluster/agent names keep the same
		gomega.Eventually(func() error {
			_, err = util.FindAutoApprovedSpokeCSR(kubeClient, managedClusterName)
			if err != nil {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	})

	// This case happens when registration agent is restarted with a new cluster name by specifing
	// argument 'cluster-name' and the agent has already had a hub kubecofig with a different
	// cluster name. A bootstrap process is expected.
	ginkgo.It("restart agent with a different cluster name", func() {
		var err error
		managedClusterName := "restart-test-cluster2"

		hubKubeconfigSecret := "restart-test-hub-kubeconfig-secret"
		hubKubeconfigDir := path.Join(util.TestDir, "restart-test", "hub-kubeconfig")

		bootstrapFile := path.Join(util.TestDir, "restart-test", "kubeconfig")

		ginkgo.By("Create bootstrap kubeconfig")
		err = authn.CreateBootstrapKubeConfigWithCertAge(bootstrapFile, serverCertFile, securePort, 20*time.Second)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("run registration agent")
		agentOptions := spoke.SpokeAgentOptions{
			ClusterName:              managedClusterName,
			BootstrapKubeconfig:      bootstrapFile,
			HubKubeconfigSecret:      hubKubeconfigSecret,
			HubKubeconfigDir:         hubKubeconfigDir,
			ClusterHealthCheckPeriod: 1 * time.Minute,
		}

		stopAgent := util.RunAgent("restart-test", agentOptions, spokeCfg)

		ginkgo.By("Check existence of csr and ManagedCluster")
		// the csr should be created
		gomega.Eventually(func() bool {
			if _, err := util.FindUnapprovedSpokeCSR(kubeClient, managedClusterName); err != nil {
				return false
			}
			return true
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		// the spoke cluster should be created
		gomega.Eventually(func() bool {
			if _, err := util.GetManagedCluster(clusterClient, managedClusterName); err != nil {
				return false
			}
			return true
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		ginkgo.By("Accept ManagedCluster and approve csr")
		err = util.AcceptManagedCluster(clusterClient, managedClusterName)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		err = authn.ApproveSpokeClusterCSR(kubeClient, managedClusterName, time.Second*20)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Check if hub kubeconfig secret is updated")
		// the hub kubeconfig secret should be filled after the csr is approved
		gomega.Eventually(func() bool {
			if _, err := util.GetFilledHubKubeConfigSecret(kubeClient, testNamespace, hubKubeconfigSecret); err != nil {
				return false
			}
			return true
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		ginkgo.By("Check if ManagedCluster joins the hub")
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

		ginkgo.By("Stop registration agent and wait for a grace period")
		stopAgent()
		time.Sleep(5 * time.Second)

		ginkgo.By("Restart registration agent with a new cluster name")
		managedClusterName = "restart-test-cluster3"
		agentOptions = spoke.SpokeAgentOptions{
			ClusterName:              managedClusterName,
			BootstrapKubeconfig:      bootstrapFile,
			HubKubeconfigSecret:      hubKubeconfigSecret,
			HubKubeconfigDir:         hubKubeconfigDir,
			ClusterHealthCheckPeriod: 1 * time.Minute,
		}
		stopAgent = util.RunAgent("restart-test", agentOptions, spokeCfg)
		defer stopAgent()

		ginkgo.By("Check the existence of csr and the new ManagedCluster")
		// the csr should be created
		gomega.Eventually(func() bool {
			if _, err := util.FindUnapprovedSpokeCSR(kubeClient, managedClusterName); err != nil {
				return false
			}
			return true
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		// the spoke cluster should be created
		gomega.Eventually(func() bool {
			if _, err := util.GetManagedCluster(clusterClient, managedClusterName); err != nil {
				return false
			}
			return true
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		ginkgo.By("Accept the new ManagedCluster and approve csr")
		err = util.AcceptManagedCluster(clusterClient, managedClusterName)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		err = authn.ApproveSpokeClusterCSR(kubeClient, managedClusterName, time.Second*20)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Check if hub kubeconfig secret is updated")
		// the hub kubeconfig secret should be filled after the csr is approved
		gomega.Eventually(func() bool {
			if _, err := util.GetFilledHubKubeConfigSecret(kubeClient, testNamespace, hubKubeconfigSecret); err != nil {
				return false
			}
			return true
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		ginkgo.By("Check if the new ManagedCluster joins the hub")
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
