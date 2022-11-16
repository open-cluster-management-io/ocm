package e2e

import (
	"context"
	"fmt"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/registration/pkg/helpers"
	"open-cluster-management.io/registration/pkg/hub/taint"
)

var _ = ginkgo.Describe("Taints update check", func() {
	ginkgo.Context("Check the taint to update according to the condition status", func() {
		var (
			err            error
			managedCluster *clusterv1.ManagedCluster
			clusterName    string
		)
		ginkgo.BeforeEach(func() {
			clusterName = fmt.Sprintf("managedcluster-%s", rand.String(6))
			managedCluster = &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
				},
				Spec: clusterv1.ManagedClusterSpec{
					HubAcceptsClient: true,
				},
			}
			managedCluster, err = clusterClient.ClusterV1().ManagedClusters().Create(context.Background(), managedCluster, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		})

		ginkgo.AfterEach(func() {
			err := clusterClient.ClusterV1().ManagedClusters().Delete(context.Background(), clusterName, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		})

		ginkgo.It("Should update taints automatically", func() {
			managedClusters := clusterClient.ClusterV1().ManagedClusters()

			ginkgo.By("Should only be one UnreachableTaint")
			gomega.Eventually(func() error {
				if managedCluster, err = managedClusters.Get(context.TODO(), clusterName, metav1.GetOptions{}); err != nil {
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

			ginkgo.By("Change the LeaseDurationSeconds to 60")
			gomega.Eventually(func() error {
				if managedCluster, err = managedClusters.Get(context.TODO(), clusterName, metav1.GetOptions{}); err != nil {
					return err
				}
				managedCluster.Spec.LeaseDurationSeconds = 60
				if _, err = managedClusters.Update(context.TODO(), managedCluster, metav1.UpdateOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

			ginkgo.By("Add a ManagedClusterConditionAvailable condition")
			gomega.Eventually(func() error {
				if managedCluster, err = managedClusters.Get(context.TODO(), clusterName, metav1.GetOptions{}); err != nil {
					return err
				}
				meta.SetStatusCondition(&(managedCluster.Status.Conditions), metav1.Condition{
					Type:   clusterv1.ManagedClusterConditionAvailable,
					Status: metav1.ConditionTrue,
					Reason: "ForTest",
				})
				if _, err = managedClusters.UpdateStatus(context.TODO(), managedCluster, metav1.UpdateOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

			ginkgo.By("The taints len should be 0")
			gomega.Eventually(func() error {
				if managedCluster, err = managedClusters.Get(context.TODO(), clusterName, metav1.GetOptions{}); err != nil {
					return err
				}
				if len(managedCluster.Spec.Taints) != 0 {
					return fmt.Errorf("managedCluster taints len is not 0")
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

			ginkgo.By("Set the ManagedClusterConditionAvailable status to false")
			gomega.Eventually(func() error {
				if managedCluster, err = managedClusters.Get(context.TODO(), clusterName, metav1.GetOptions{}); err != nil {
					return err
				}
				meta.SetStatusCondition(&(managedCluster.Status.Conditions), metav1.Condition{
					Type:   clusterv1.ManagedClusterConditionAvailable,
					Status: metav1.ConditionFalse,
					Reason: "ForTest",
				})
				if _, err = managedClusters.UpdateStatus(context.TODO(), managedCluster, metav1.UpdateOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

			ginkgo.By("Should only be one UnavailableTaint")
			gomega.Eventually(func() error {
				if managedCluster, err = managedClusters.Get(context.TODO(), clusterName, metav1.GetOptions{}); err != nil {
					return err
				}
				if len(managedCluster.Spec.Taints) != 1 {
					return fmt.Errorf("managedCluster taints len is not 1")
				}
				if !helpers.IsTaintEqual(managedCluster.Spec.Taints[0], taint.UnavailableTaint) {
					return fmt.Errorf("the %+v is not equal to UnavailableTaint\n", managedCluster.Spec.Taints[0])
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

			ginkgo.By("Set the ManagedClusterConditionAvailable status to unknown")
			gomega.Eventually(func() error {
				if managedCluster, err = managedClusters.Get(context.TODO(), clusterName, metav1.GetOptions{}); err != nil {
					return err
				}
				meta.SetStatusCondition(&(managedCluster.Status.Conditions), metav1.Condition{
					Type:   clusterv1.ManagedClusterConditionAvailable,
					Status: metav1.ConditionUnknown,
					Reason: "ForTest",
				})
				if _, err = managedClusters.UpdateStatus(context.TODO(), managedCluster, metav1.UpdateOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

			ginkgo.By("Should only be one UnreachableTaint")
			gomega.Eventually(func() error {
				if managedCluster, err = managedClusters.Get(context.TODO(), clusterName, metav1.GetOptions{}); err != nil {
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
})
