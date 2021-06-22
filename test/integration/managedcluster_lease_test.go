package integration_test

import (
	"context"
	"fmt"
	"path"
	"time"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"

	"github.com/openshift/library-go/pkg/controller/controllercmd"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/registration/pkg/spoke"
	"open-cluster-management.io/registration/test/integration/util"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
)

var _ = ginkgo.Describe("Cluster Lease Update", func() {
	var managedClusterName string
	var hubKubeconfigSecret string
	var hubKubeconfigDir string

	ginkgo.BeforeEach(func() {
		managedClusterName = fmt.Sprintf("managedcluster-%s", rand.String(6))
		hubKubeconfigSecret = fmt.Sprintf("%s-secret", managedClusterName)
		hubKubeconfigDir = path.Join(util.TestDir, "leasetest", fmt.Sprintf("%s-config", managedClusterName))
	})

	ginkgo.It("managed cluster lease should be updated constantly", func() {
		// run registration agent
		go func() {
			agentOptions := spoke.SpokeAgentOptions{
				ClusterName:              managedClusterName,
				BootstrapKubeconfig:      bootstrapKubeConfigFile,
				HubKubeconfigSecret:      hubKubeconfigSecret,
				HubKubeconfigDir:         hubKubeconfigDir,
				ClusterHealthCheckPeriod: 1 * time.Minute,
			}
			err := agentOptions.RunSpokeAgent(context.Background(), &controllercmd.ControllerContext{
				KubeConfig:    spokeCfg,
				EventRecorder: util.NewIntegrationTestEventRecorder("cluster-leasetest"),
			})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		}()

		// simulate hub cluster admin to accept the managedcluster and approve the csr
		gomega.Eventually(func() bool {
			if err := util.AcceptManagedCluster(clusterClient, managedClusterName); err != nil {
				return false
			}
			return true
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		gomega.Eventually(func() bool {
			if err := util.ApproveSpokeClusterCSR(kubeClient, managedClusterName, time.Hour*24); err != nil {
				return false
			}
			return true
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		// simulate k8s to mount the hub kubeconfig secret after the bootstrap is finished
		gomega.Eventually(func() bool {
			if _, err := util.GetFilledHubKubeConfigSecret(kubeClient, testNamespace, hubKubeconfigSecret); err != nil {
				return false
			}
			return true
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		// after two grace period, make sure the managed cluster is available
		select {
		case <-time.After(time.Duration(2*5*util.TestLeaseDurationSeconds) * time.Second):
			managedCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			availableCond := meta.FindStatusCondition(managedCluster.Status.Conditions, clusterv1.ManagedClusterConditionAvailable)
			gomega.Expect(availableCond).ShouldNot(gomega.BeNil())
			gomega.Expect(availableCond.Status).Should(gomega.Equal(metav1.ConditionTrue))
		}
	})

	ginkgo.It("managed cluster available condition should be recovered after its lease update is recovered", func() {
		ctx, stop := context.WithCancel(context.Background())
		// run registration agent
		go func() {
			agentOptions := spoke.SpokeAgentOptions{
				ClusterName:              managedClusterName,
				BootstrapKubeconfig:      bootstrapKubeConfigFile,
				HubKubeconfigSecret:      hubKubeconfigSecret,
				HubKubeconfigDir:         hubKubeconfigDir,
				ClusterHealthCheckPeriod: 1 * time.Minute,
			}
			err := agentOptions.RunSpokeAgent(ctx, &controllercmd.ControllerContext{
				KubeConfig:    spokeCfg,
				EventRecorder: util.NewIntegrationTestEventRecorder("cluster-availabletest"),
			})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		}()

		// simulate hub cluster admin to accept the managed cluster and approve the csr
		gomega.Eventually(func() bool {
			if err := util.AcceptManagedCluster(clusterClient, managedClusterName); err != nil {
				return false
			}
			return true
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		gomega.Eventually(func() bool {
			if err := util.ApproveSpokeClusterCSR(kubeClient, managedClusterName, time.Hour*24); err != nil {
				return false
			}
			return true
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		// simulate k8s to mount the hub kubeconfig secret after the bootstrap is finished
		gomega.Eventually(func() bool {
			if _, err := util.GetFilledHubKubeConfigSecret(kubeClient, testNamespace, hubKubeconfigSecret); err != nil {
				return false
			}
			return true
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		// make sure the managed cluster is available
		gomega.Eventually(func() bool {
			managedCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
			if err != nil {
				return false
			}
			availableCond := meta.FindStatusCondition(managedCluster.Status.Conditions, clusterv1.ManagedClusterConditionAvailable)
			if availableCond == nil {
				return false
			}
			return availableCond.Status == metav1.ConditionTrue
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		// stop the current managed cluster
		stop()

		// after one grace period, make sure the managed available condition is cluster unknown
		select {
		case <-time.After(time.Duration(5*util.TestLeaseDurationSeconds+10) * time.Second):
			managedCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			availableCond := meta.FindStatusCondition(managedCluster.Status.Conditions, clusterv1.ManagedClusterConditionAvailable)
			gomega.Expect(availableCond).ShouldNot(gomega.BeNil())
			gomega.Expect(availableCond.Status).Should(gomega.Equal(metav1.ConditionUnknown))
		}

		// restart the registration agent
		go func() {
			agentOptions := spoke.SpokeAgentOptions{
				ClusterName:              managedClusterName,
				BootstrapKubeconfig:      bootstrapKubeConfigFile,
				HubKubeconfigSecret:      hubKubeconfigSecret,
				HubKubeconfigDir:         hubKubeconfigDir,
				ClusterHealthCheckPeriod: 1 * time.Minute,
			}
			err := agentOptions.RunSpokeAgent(context.Background(), &controllercmd.ControllerContext{
				KubeConfig:    spokeCfg,
				EventRecorder: util.NewIntegrationTestEventRecorder("cluster-leasetest"),
			})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		}()

		// after one grace period, make sure the managed cluster available condition is recovered
		select {
		case <-time.After(time.Duration(5*util.TestLeaseDurationSeconds+1) * time.Second):
			managedCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			availableCond := meta.FindStatusCondition(managedCluster.Status.Conditions, clusterv1.ManagedClusterConditionAvailable)
			gomega.Expect(availableCond).ShouldNot(gomega.BeNil())
			gomega.Expect(availableCond.Status).Should(gomega.Equal(metav1.ConditionTrue))
		}
	})
})
