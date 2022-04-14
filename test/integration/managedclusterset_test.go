package integration_test

import (
	"context"
	"fmt"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
)

const (
	clusterSetLabel = "cluster.open-cluster-management.io/clusterset"
)

var _ = ginkgo.Describe("ManagedClusterSet", func() {
	ginkgo.It("should create cluster set and keep it synced successfully ", func() {
		for i := 0; i < 10; i++ {
			suffix := rand.String(6)
			ginkgo.By("Create a ManagedClusterSet")
			managedClusterSetName := fmt.Sprintf("cs1-%s", suffix)
			managedClusterSet := &clusterv1beta1.ManagedClusterSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: managedClusterSetName,
				},
			}

			_, err := clusterClient.ClusterV1beta1().ManagedClusterSets().Create(context.Background(), managedClusterSet, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			ginkgo.By("Check if ManagedClusterSet is reconciled")
			gomega.Eventually(func() bool {
				managedClusterSet, err = clusterClient.ClusterV1beta1().ManagedClusterSets().Get(context.Background(), managedClusterSetName, metav1.GetOptions{})
				if err != nil {
					return false
				}

				for _, condition := range managedClusterSet.Status.Conditions {
					if condition.Type != clusterv1beta1.ManagedClusterSetConditionEmpty {
						continue
					}
					if condition.Status != metav1.ConditionTrue {
						return false
					}
					if condition.Reason != "NoClusterMatched" {
						return false
					}
					return true
				}
				return false
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			ginkgo.By("Create a ManagedCluster")
			managedClusterName := fmt.Sprintf("managedcluster-%s", suffix)
			managedCluster := &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: managedClusterName,
					Labels: map[string]string{
						clusterSetLabel: managedClusterSetName,
					},
				},
				Spec: clusterv1.ManagedClusterSpec{
					HubAcceptsClient: true,
				},
			}

			_, err = clusterClient.ClusterV1().ManagedClusters().Create(context.Background(), managedCluster, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			ginkgo.By("Check if ManagedClusterSet is reconciled again")
			gomega.Eventually(func() bool {
				managedClusterSet, err = clusterClient.ClusterV1beta1().ManagedClusterSets().Get(context.Background(), managedClusterSetName, metav1.GetOptions{})
				if err != nil {
					return false
				}
				for _, condition := range managedClusterSet.Status.Conditions {
					if condition.Type != clusterv1beta1.ManagedClusterSetConditionEmpty {
						continue
					}
					if condition.Status != metav1.ConditionFalse {
						return false
					}
					if condition.Reason != "ClustersSelected" {
						return false
					}
					return true
				}
				return false
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			ginkgo.By("Move the cluster to another ManagedClusterSet and check if the original one is empty again")
			// create another clusterset
			managedClusterSet = &clusterv1beta1.ManagedClusterSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("cs2-%s", suffix),
				},
			}
			_, err = clusterClient.ClusterV1beta1().ManagedClusterSets().Create(context.Background(), managedClusterSet, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			gomega.Eventually(func() error {
				// move the cluster to the new clusterset
				managedCluster, err = clusterClient.ClusterV1().ManagedClusters().Get(context.Background(), managedCluster.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}
				managedCluster.Labels = map[string]string{
					clusterSetLabel: managedClusterSet.Name,
				}
				_, err := clusterClient.ClusterV1().ManagedClusters().Update(context.Background(), managedCluster, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// check if the new clusterset synced
			gomega.Eventually(func() bool {
				managedClusterSet, err = clusterClient.ClusterV1beta1().ManagedClusterSets().Get(context.Background(), managedClusterSet.Name, metav1.GetOptions{})
				if err != nil {
					return false
				}
				for _, condition := range managedClusterSet.Status.Conditions {
					if condition.Type != clusterv1beta1.ManagedClusterSetConditionEmpty {
						continue
					}
					if condition.Status != metav1.ConditionFalse {
						return false
					}
					if condition.Reason != "ClustersSelected" {
						return false
					}
					return true
				}
				return false
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			// check if the original clusterset synced
			gomega.Eventually(func() error {
				managedClusterSet, err = clusterClient.ClusterV1beta1().ManagedClusterSets().Get(context.Background(), managedClusterSetName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				cond := meta.FindStatusCondition(managedClusterSet.Status.Conditions, clusterv1beta1.ManagedClusterSetConditionEmpty)
				if cond == nil {
					return fmt.Errorf("clusterset empty condition is not found")
				}

				if cond.Status != metav1.ConditionTrue {
					return fmt.Errorf("clusterset should be empty")
				}

				if cond.Reason != "NoClusterMatched" {
					return fmt.Errorf("clusterset condition reason not correct, got %q", cond.Reason)
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
		}
	})
})
