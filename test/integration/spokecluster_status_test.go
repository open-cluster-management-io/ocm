package integration_test

import (
	"fmt"
	"path"
	"time"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/registration/pkg/spoke"
	"open-cluster-management.io/registration/test/integration/util"
)

var _ = ginkgo.Describe("Collecting Node Resource", func() {
	ginkgo.It("managed cluster node resource should be collected successfully", func() {
		var err error

		// create one node
		capacity := util.NewResourceList(32, 64)
		allocatable := util.NewResourceList(16, 32)
		err = util.CreateNode(kubeClient, "node1", capacity, allocatable)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		managedClusterName := "resorucetest-managedcluster"
		hubKubeconfigSecret := "resorucetest-hub-kubeconfig-secret"
		hubKubeconfigDir := path.Join(util.TestDir, "resorucetest", "hub-kubeconfig")

		// run registration agent
		agentOptions := spoke.SpokeAgentOptions{
			ClusterName:              managedClusterName,
			BootstrapKubeconfig:      bootstrapKubeConfigFile,
			HubKubeconfigSecret:      hubKubeconfigSecret,
			HubKubeconfigDir:         hubKubeconfigDir,
			ClusterHealthCheckPeriod: 1 * time.Minute,
		}

		cancel := util.RunAgent("resorucetest", agentOptions, spokeCfg)
		defer cancel()

		// the spoke cluster and csr should be created after bootstrap
		gomega.Eventually(func() bool {
			if _, err := util.GetManagedCluster(clusterClient, managedClusterName); err != nil {
				return false
			}
			return true
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		gomega.Eventually(func() bool {
			if _, err := util.FindUnapprovedSpokeCSR(kubeClient, managedClusterName); err != nil {
				return false
			}
			return true
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		// the spoke cluster should has finalizer that is added by hub controller
		gomega.Eventually(func() bool {
			spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
			if err != nil {
				return false
			}
			if len(spokeCluster.Finalizers) != 1 {
				return false
			}

			if spokeCluster.Finalizers[0] != "cluster.open-cluster-management.io/api-resource-cleanup" {
				return false
			}

			return true
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		// simulate hub cluster admin to accept the managedcluster and approve the csr
		err = util.AcceptManagedCluster(clusterClient, managedClusterName)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		err = authn.ApproveSpokeClusterCSR(kubeClient, managedClusterName, time.Hour*24)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// the managed cluster should have accepted condition after it is accepted
		gomega.Eventually(func() bool {
			spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
			if err != nil {
				return false
			}
			accpeted := meta.FindStatusCondition(spokeCluster.Status.Conditions, clusterv1.ManagedClusterConditionHubAccepted)
			return accpeted != nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		// the hub kubeconfig secret should be filled after the csr is approved
		gomega.Eventually(func() bool {
			if _, err := util.GetFilledHubKubeConfigSecret(kubeClient, testNamespace, hubKubeconfigSecret); err != nil {
				return false
			}
			return true
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		// the resource of spoke cluster should be updated finally
		gomega.Eventually(func() bool {
			spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
			if err != nil {
				return false
			}
			if !util.CmpResourceQuantity("cpu", capacity, spokeCluster.Status.Capacity) {
				fmt.Printf("expected cpu capacity %#v but got: %#v\n", capacity["cpu"], spokeCluster.Status.Capacity["cpu"])
				return false
			}
			if !util.CmpResourceQuantity("memory", capacity, spokeCluster.Status.Capacity) {
				return false
			}
			if !util.CmpResourceQuantity("cpu", allocatable, spokeCluster.Status.Allocatable) {
				return false
			}
			if !util.CmpResourceQuantity("memory", allocatable, spokeCluster.Status.Allocatable) {
				return false
			}
			return true
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		// cordon the node
		err = util.CordonNode(kubeClient, "node1")
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// the resource of spoke cluster should be updated finally
		gomega.Eventually(func() bool {
			spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
			if err != nil {
				return false
			}
			if !util.CmpResourceQuantity("cpu", capacity, spokeCluster.Status.Capacity) {
				fmt.Printf("expected cpu capacity %#v but got: %#v\n", capacity["cpu"], spokeCluster.Status.Capacity["cpu"])
				return false
			}
			if !util.CmpResourceQuantity("memory", capacity, spokeCluster.Status.Capacity) {
				return false
			}

			// after cordoned the node, there should be no allocatable resource
			if len(spokeCluster.Status.Allocatable) != 0 {
				return false
			}

			return true
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
	})
})
