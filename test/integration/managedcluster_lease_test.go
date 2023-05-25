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

	"k8s.io/apimachinery/pkg/api/errors"
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
		agentOptions := spoke.SpokeAgentOptions{
			ClusterName:              managedClusterName,
			BootstrapKubeconfig:      bootstrapKubeConfigFile,
			HubKubeconfigSecret:      hubKubeconfigSecret,
			HubKubeconfigDir:         hubKubeconfigDir,
			ClusterHealthCheckPeriod: 1 * time.Minute,
		}
		cancel := util.RunAgent("cluster-leasetest", agentOptions, spokeCfg)
		defer cancel()

		bootstrapManagedCluster(managedClusterName, hubKubeconfigSecret, util.TestLeaseDurationSeconds)
		// after two grace period, make sure the managed cluster is available
		gracePeriod := 2 * 5 * util.TestLeaseDurationSeconds
		assertAvailableCondition(managedClusterName, metav1.ConditionTrue, gracePeriod)
	})

	ginkgo.It("managed cluster available condition should be recovered after its lease update is recovered", func() {
		// run registration agent
		agentOptions := spoke.SpokeAgentOptions{
			ClusterName:              managedClusterName,
			BootstrapKubeconfig:      bootstrapKubeConfigFile,
			HubKubeconfigSecret:      hubKubeconfigSecret,
			HubKubeconfigDir:         hubKubeconfigDir,
			ClusterHealthCheckPeriod: 1 * time.Minute,
		}
		stop := util.RunAgent("cluster-availabletest", agentOptions, spokeCfg)

		bootstrapManagedCluster(managedClusterName, hubKubeconfigSecret, util.TestLeaseDurationSeconds)
		assertAvailableCondition(managedClusterName, metav1.ConditionTrue, 0)

		// stop the current managed cluster
		stop()

		// after one grace period, make sure the managed available condition is cluster unknown
		gracePeriod := 5 * util.TestLeaseDurationSeconds
		assertAvailableCondition(managedClusterName, metav1.ConditionUnknown, gracePeriod)

		agentOptions = spoke.SpokeAgentOptions{
			ClusterName:              managedClusterName,
			BootstrapKubeconfig:      bootstrapKubeConfigFile,
			HubKubeconfigSecret:      hubKubeconfigSecret,
			HubKubeconfigDir:         hubKubeconfigDir,
			ClusterHealthCheckPeriod: 1 * time.Minute,
		}
		stop = util.RunAgent("cluster-availabletest", agentOptions, spokeCfg)
		defer stop()

		// after one grace period, make sure the managed cluster available condition is recovered
		gracePeriod = 5*util.TestLeaseDurationSeconds + 1
		assertAvailableCondition(managedClusterName, metav1.ConditionTrue, gracePeriod)
	})

	ginkgo.It("managed cluster available condition should be recovered after the cluster is restored", func() {
		// run registration agent
		agentOptions := spoke.SpokeAgentOptions{
			ClusterName:              managedClusterName,
			BootstrapKubeconfig:      bootstrapKubeConfigFile,
			HubKubeconfigSecret:      hubKubeconfigSecret,
			HubKubeconfigDir:         hubKubeconfigDir,
			ClusterHealthCheckPeriod: 1 * time.Minute,
		}
		cancel := util.RunAgent("cluster-leasetest", agentOptions, spokeCfg)
		defer cancel()

		bootstrapManagedCluster(managedClusterName, hubKubeconfigSecret, util.TestLeaseDurationSeconds)
		assertAvailableCondition(managedClusterName, metav1.ConditionTrue, 0)

		// remove the cluster
		gomega.Eventually(func() error {
			if err := clusterClient.ClusterV1().ManagedClusters().Delete(context.TODO(), managedClusterName, metav1.DeleteOptions{}); err != nil {
				return err
			}
			managedCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
			if errors.IsNotFound(err) {
				return nil
			}
			managedCluster.Finalizers = []string{}
			_, err = clusterClient.ClusterV1().ManagedClusters().Update(context.TODO(), managedCluster, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// restore the cluster
		gomega.Eventually(func() error {
			_, err := clusterClient.ClusterV1().ManagedClusters().Create(
				context.TODO(),
				&clusterv1.ManagedCluster{
					ObjectMeta: metav1.ObjectMeta{Name: managedClusterName},
					Spec: clusterv1.ManagedClusterSpec{
						HubAcceptsClient:     true,
						LeaseDurationSeconds: util.TestLeaseDurationSeconds,
					},
				},
				metav1.CreateOptions{},
			)
			return err
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// after two grace period, make sure the managed cluster is available
		gracePeriod := 2 * 5 * util.TestLeaseDurationSeconds
		assertAvailableCondition(managedClusterName, metav1.ConditionTrue, gracePeriod)
	})

	ginkgo.It("should use a short lease duration", func() {
		// run registration agent
		agentOptions := spoke.SpokeAgentOptions{
			ClusterName:              managedClusterName,
			BootstrapKubeconfig:      bootstrapKubeConfigFile,
			HubKubeconfigSecret:      hubKubeconfigSecret,
			HubKubeconfigDir:         hubKubeconfigDir,
			ClusterHealthCheckPeriod: 1 * time.Minute,
		}
		stop := util.RunAgent("cluster-leasetest", agentOptions, spokeCfg)

		bootstrapManagedCluster(managedClusterName, hubKubeconfigSecret, 60)
		assertAvailableCondition(managedClusterName, metav1.ConditionTrue, 0)

		// update the lease duration with a short duration (1s)
		err := updateManagedClusterLeaseDuration(managedClusterName, util.TestLeaseDurationSeconds)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// stop the agent
		stop()

		// after two short grace period, make sure the managed cluster is unknown
		gracePeriod := 2 * 5 * util.TestLeaseDurationSeconds
		assertAvailableCondition(managedClusterName, metav1.ConditionUnknown, gracePeriod)
	})
})

func bootstrapManagedCluster(managedClusterName, hubKubeconfigSecret string, leaseDuration int32) {
	// simulate hub cluster admin to accept the managed cluster and approve the csr
	gomega.Eventually(func() error {
		return util.AcceptManagedClusterWithLeaseDuration(clusterClient, managedClusterName, leaseDuration)
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

	gomega.Eventually(func() error {
		return authn.ApproveSpokeClusterCSR(kubeClient, managedClusterName, time.Hour*24)
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

	// simulate k8s to mount the hub kubeconfig secret after the bootstrap is finished
	gomega.Eventually(func() error {
		_, err := util.GetFilledHubKubeConfigSecret(kubeClient, testNamespace, hubKubeconfigSecret)
		return err
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
}

func assertAvailableCondition(managedClusterName string, status metav1.ConditionStatus, d int) {
	<-time.After(time.Duration(d) * time.Second)
	gomega.Eventually(func() error {
		managedCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
		if err != nil {
			return err
		}
		availableCond := meta.FindStatusCondition(managedCluster.Status.Conditions, clusterv1.ManagedClusterConditionAvailable)
		if availableCond == nil {
			return fmt.Errorf("available condition is not found")
		}
		if availableCond.Status != status {
			return fmt.Errorf("expected avaibale condition is %s, but %v", status, availableCond)
		}
		return nil
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
}

func updateManagedClusterLeaseDuration(clusterName string, leaseDuration int32) error {
	cluster, err := clusterClient.ClusterV1().ManagedClusters().Get(context.TODO(), clusterName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	cluster.Spec.LeaseDurationSeconds = leaseDuration

	_, err = clusterClient.ClusterV1().ManagedClusters().Update(context.TODO(), cluster, metav1.UpdateOptions{})
	return err
}
