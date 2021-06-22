package integration_test

import (
	"context"
	"fmt"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
)

const (
	clusterSetLabel = "cluster.open-cluster-management.io/clusterset"
)

var _ = ginkgo.Describe("ManagedClusterSet", func() {
	ginkgo.It("should create cluster set and keep it synced successfully ", func() {
		ginkgo.By("Create a ManagedClusterSet")
		managedClusterSetName := "cs1"
		managedClusterSet := &clusterv1alpha1.ManagedClusterSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: managedClusterSetName,
			},
		}

		_, err := clusterClient.ClusterV1alpha1().ManagedClusterSets().Create(context.Background(), managedClusterSet, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Check if ManagedClusterSet is reconciled")
		gomega.Eventually(func() bool {
			managedClusterSet, err = clusterClient.ClusterV1alpha1().ManagedClusterSets().Get(context.Background(), managedClusterSetName, metav1.GetOptions{})
			if err != nil {
				return false
			}
			for _, condition := range managedClusterSet.Status.Conditions {
				if condition.Type != clusterv1alpha1.ManagedClusterSetConditionEmpty {
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
		managedClusterName := fmt.Sprintf("managedcluster-%s", rand.String(6))
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
			managedClusterSet, err = clusterClient.ClusterV1alpha1().ManagedClusterSets().Get(context.Background(), managedClusterSetName, metav1.GetOptions{})
			if err != nil {
				return false
			}
			for _, condition := range managedClusterSet.Status.Conditions {
				if condition.Type != clusterv1alpha1.ManagedClusterSetConditionEmpty {
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
	})
})
