package registration_test

import (
	"context"
	"fmt"
	"path"
	"reflect"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusterv1 "open-cluster-management.io/api/cluster/v1"

	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	registerfactory "open-cluster-management.io/ocm/pkg/registration/register/factory"
	"open-cluster-management.io/ocm/pkg/registration/spoke"
	"open-cluster-management.io/ocm/test/integration/util"
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
		agentOptions := &spoke.SpokeAgentOptions{
			BootstrapKubeconfig:      bootstrapFile,
			HubKubeconfigSecret:      hubKubeconfigSecret,
			ClusterHealthCheckPeriod: 1 * time.Minute,
			RegisterDriverOption:     registerfactory.NewOptions(),
		}
		commOptions := commonoptions.NewAgentOptions()
		commOptions.HubKubeconfigDir = hubKubeconfigDir
		commOptions.SpokeClusterName = managedClusterName

		stopAgent := runAgent("restart-test", agentOptions, commOptions, spokeCfg)

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
		gomega.Eventually(func() error {
			spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
			if err != nil {
				return err
			}
			var conditions []metav1.Condition
			for _, condition := range spokeCluster.Status.Conditions {
				if condition.Type == clusterv1.ManagedClusterConditionJoined {
					continue
				}
				conditions = append(conditions, condition)
			}
			spokeCluster.Status.Conditions = conditions
			_, err = clusterClient.ClusterV1().ManagedClusters().UpdateStatus(context.TODO(), spokeCluster, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

		ginkgo.By("Restart registration agent")
		agentOptions = &spoke.SpokeAgentOptions{
			BootstrapKubeconfig:      bootstrapFile,
			HubKubeconfigSecret:      hubKubeconfigSecret,
			ClusterHealthCheckPeriod: 1 * time.Minute,
			RegisterDriverOption:     registerfactory.NewOptions(),
		}
		commOptions = commonoptions.NewAgentOptions()
		commOptions.HubKubeconfigDir = hubKubeconfigDir
		commOptions.SpokeClusterName = managedClusterName
		stopAgent = runAgent("restart-test", agentOptions, commOptions, spokeCfg)
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

	// This case happens when registration agent is restarted with a new cluster name by specifying
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
		agentOptions := &spoke.SpokeAgentOptions{
			BootstrapKubeconfig:      bootstrapFile,
			HubKubeconfigSecret:      hubKubeconfigSecret,
			ClusterHealthCheckPeriod: 1 * time.Minute,
			RegisterDriverOption:     registerfactory.NewOptions(),
		}
		commOptions := commonoptions.NewAgentOptions()
		commOptions.HubKubeconfigDir = hubKubeconfigDir
		commOptions.SpokeClusterName = managedClusterName
		stopAgent := runAgent("restart-test", agentOptions, commOptions, spokeCfg)

		ginkgo.By("Check existence of csr and ManagedCluster")
		// the csr should be created
		gomega.Eventually(func() error {
			_, err := util.FindUnapprovedSpokeCSR(kubeClient, managedClusterName)
			return err
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// the spoke cluster should be created
		gomega.Eventually(func() error {
			_, err := util.GetManagedCluster(clusterClient, managedClusterName)
			return err
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Accept ManagedCluster and approve csr")
		err = util.AcceptManagedCluster(clusterClient, managedClusterName)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		err = authn.ApproveSpokeClusterCSR(kubeClient, managedClusterName, time.Second*20)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Check if hub kubeconfig secret is updated")
		// the hub kubeconfig secret should be filled after the csr is approved
		gomega.Eventually(func() error {
			_, err := util.GetFilledHubKubeConfigSecret(kubeClient, testNamespace, hubKubeconfigSecret)
			return err
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

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
		agentOptions = &spoke.SpokeAgentOptions{
			BootstrapKubeconfig:      bootstrapFile,
			HubKubeconfigSecret:      hubKubeconfigSecret,
			ClusterHealthCheckPeriod: 1 * time.Minute,
			RegisterDriverOption:     registerfactory.NewOptions(),
		}
		commOptions = commonoptions.NewAgentOptions()
		commOptions.HubKubeconfigDir = hubKubeconfigDir
		commOptions.SpokeClusterName = managedClusterName
		stopAgent = runAgent("restart-test", agentOptions, commOptions, spokeCfg)
		defer stopAgent()

		ginkgo.By("Check the existence of csr and the new ManagedCluster")
		// the csr should be created
		gomega.Eventually(func() error {
			_, err := util.FindUnapprovedSpokeCSR(kubeClient, managedClusterName)
			return err
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// the spoke cluster should be created
		gomega.Eventually(func() error {
			_, err := util.GetManagedCluster(clusterClient, managedClusterName)
			return err
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Accept the new ManagedCluster and approve csr")
		err = util.AcceptManagedCluster(clusterClient, managedClusterName)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		err = authn.ApproveSpokeClusterCSR(kubeClient, managedClusterName, time.Second*20)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Check if hub kubeconfig secret is updated")
		// the hub kubeconfig secret should be filled after the csr is approved
		gomega.Eventually(func() error {
			_, err := util.GetFilledHubKubeConfigSecret(kubeClient, testNamespace, hubKubeconfigSecret)
			return err
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

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

	ginkgo.It("agent rebootstrap when kubeconfig current context cluster name changes", func() {
		var err error
		spokeClusterName := "contextclusternamechanges-spokecluster"

		//#nosec G101
		hubKubeconfigSecret := "contextclusternamechanges-hub-kubeconfig-secret"
		hubKubeconfigDir := path.Join(util.TestDir, "contextclusternamechanges", "hub-kubeconfig")

		bootstrapFile := path.Join(util.TestDir, "contextclusternamechanges-rebootstrap-test", "kubeconfig")
		ginkgo.By("Create bootstrap kubeconfig")
		err = authn.CreateBootstrapKubeConfigWithCertAge(bootstrapFile, serverCertFile, securePort, 20*time.Second,
			"hub")
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// run registration agent
		agentOptions := &spoke.SpokeAgentOptions{
			BootstrapKubeconfig:      bootstrapFile,
			HubKubeconfigSecret:      hubKubeconfigSecret,
			ClusterHealthCheckPeriod: 1 * time.Minute,
			RegisterDriverOption:     registerfactory.NewOptions(),
		}
		commOptions := commonoptions.NewAgentOptions()
		commOptions.HubKubeconfigDir = hubKubeconfigDir
		commOptions.SpokeClusterName = spokeClusterName

		stopAgent := runAgent("contextclusternamechanges-rebootstraptest", agentOptions, commOptions, spokeCfg)
		defer stopAgent()

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

		// approve the csr with a valid hub config
		err = authn.ApproveSpokeClusterCSR(kubeClient, spokeClusterName, time.Hour*24)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		var firstHubKubeConfigSecret *corev1.Secret
		// the hub kubeconfig secret should be filled after the csr is approved
		gomega.Eventually(func() error {
			firstHubKubeConfigSecret, err = util.GetFilledHubKubeConfigSecret(kubeClient, testNamespace, hubKubeconfigSecret)
			return err
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Stop registration agent and wait for a grace period")
		stopAgent()
		time.Sleep(5 * time.Second)

		ginkgo.By("Regenerate a new bootstrap kubeconfig with a new cluster name")
		err = authn.CreateBootstrapKubeConfigWithCertAge(bootstrapFile, serverCertFile, securePort, 20*time.Second,
			"new-hub")
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Restart registration agent")
		stopAgent = runAgent("contextclusternamechanges-rebootstraptest", agentOptions, commOptions, spokeCfg)
		defer stopAgent()

		// agent should bootstrap again due to the cluster name in the kubeconfig current context changes
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

		// the spoke cluster should have joined condition
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
