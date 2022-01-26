package integration_test

import (
	"context"
	"fmt"
	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	v1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/registration/pkg/helpers"
	"open-cluster-management.io/registration/pkg/hub/taint"
	"open-cluster-management.io/registration/pkg/spoke"
	"open-cluster-management.io/registration/test/integration/util"
	"path"
	"time"
)

var _ = ginkgo.Describe("ManagedCluster Taints Update", func() {
	var managedClusterName string
	var hubKubeconfigSecret string
	var hubKubeconfigDir string

	ginkgo.BeforeEach(func() {
		managedClusterName = fmt.Sprintf("managedcluster-%s", rand.String(6))
		hubKubeconfigSecret = fmt.Sprintf("%s-secret", managedClusterName)
		hubKubeconfigDir = path.Join(util.TestDir, "leasetest", fmt.Sprintf("%s-config", managedClusterName))
	})

	ginkgo.It("ManagedCluster taint should be updated automatically", func() {
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
				EventRecorder: util.NewIntegrationTestEventRecorder("cluster-tainttest"),
			})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		}()

		gomega.Eventually(func() error {
			spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
			if err != nil {
				return err
			}
			spokeCluster.Spec.HubAcceptsClient = true
			_, err = clusterClient.ClusterV1().ManagedClusters().Update(context.TODO(), spokeCluster, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

		gomega.Eventually(func() error {
			managedCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
			if err != nil {
				return err
			}
			if len(managedCluster.Spec.Taints) != 1 {
				return fmt.Errorf("managedCluster taints len is not 1")
			}
			if !helpers.IsTaintEqual(managedCluster.Spec.Taints[0], taint.UnreachableTaint) {
				return fmt.Errorf("the %+v is not equal to UnreachableTaint", managedCluster.Spec.Taints[0])
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

		gomega.Eventually(func() error {
			if err := authn.ApproveSpokeClusterCSR(kubeClient, managedClusterName, time.Hour*24); err != nil {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

		// The managed cluster is available, so taint is expected to be empty
		gomega.Eventually(func() bool {
			managedCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
			if err != nil {
				return false
			}
			if len(managedCluster.Spec.Taints) != 0 {
				return false
			}
			return true
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		stop()

		gomega.Eventually(func() error {
			managedCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
			if err != nil {
				return err
			}
			condition := metav1.Condition{
				Type:   v1.ManagedClusterConditionAvailable,
				Status: metav1.ConditionFalse,
				Reason: "ForTest",
			}
			meta.SetStatusCondition(&(managedCluster.Status.Conditions), condition)
			_, err = clusterClient.ClusterV1().ManagedClusters().UpdateStatus(context.Background(), managedCluster, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

		gomega.Eventually(func() error {
			managedCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
			if err != nil {
				return err
			}
			if len(managedCluster.Spec.Taints) != 1 {
				return fmt.Errorf("managedCluster taints len is not 1")
			}
			if !helpers.IsTaintEqual(managedCluster.Spec.Taints[0], taint.UnavailableTaint) {
				return fmt.Errorf("the %+v is not equal to UnavailableTaint", managedCluster.Spec.Taints[0])
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

		gomega.Eventually(func() error {
			spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
			if err != nil {
				return err
			}
			spokeCluster.Spec.LeaseDurationSeconds = 1
			_, err = clusterClient.ClusterV1().ManagedClusters().Update(context.TODO(), spokeCluster, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

		gomega.Eventually(func() error {
			managedCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
			if err != nil {
				return err
			}
			if len(managedCluster.Spec.Taints) != 1 {
				return fmt.Errorf("managedCluster taints len is not 1")
			}
			if !helpers.IsTaintEqual(managedCluster.Spec.Taints[0], taint.UnreachableTaint) {
				return fmt.Errorf("the %+v is not equal to UnreachableTaint", managedCluster.Spec.Taints[0])
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
	})
})
