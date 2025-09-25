package e2e

import (
	"context"
	"fmt"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/sets"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"

	"open-cluster-management.io/ocm/pkg/registration/spoke/managedcluster"
)

var _ = ginkgo.Describe("ManagedNamespace", func() {

	ginkgo.Context("ManagedClusterSet with ManagedNamespaces", func() {
		var expectedNamespaces []string
		var originalManagedNamespaces []clusterv1.ManagedNamespaceConfig

		ginkgo.BeforeEach(func() {
			suffix := rand.String(6)
			expectedNamespaces = []string{
				fmt.Sprintf("test-ns-1-%s", suffix),
				fmt.Sprintf("test-ns-2-%s", suffix),
			}

			// Update the existing universal cluster set with managed namespaces
			gomega.Eventually(func() error {
				clusterSet, err := hub.ClusterClient.ClusterV1beta2().ManagedClusterSets().Get(
					context.TODO(), universalClusterSetName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				// Store original managed namespaces for restoration
				originalManagedNamespaces = clusterSet.Spec.ManagedNamespaces

				// Add test managed namespaces
				clusterSet.Spec.ManagedNamespaces = []clusterv1.ManagedNamespaceConfig{
					{Name: expectedNamespaces[0]},
					{Name: expectedNamespaces[1]},
				}

				_, err = hub.ClusterClient.ClusterV1beta2().ManagedClusterSets().Update(
					context.TODO(), clusterSet, metav1.UpdateOptions{})
				return err
			}, 30*time.Second, 2*time.Second).Should(gomega.Succeed())
		})

		ginkgo.AfterEach(func() {
			// Restore original managed namespaces in the universal cluster set
			gomega.Eventually(func() error {
				clusterSet, err := hub.ClusterClient.ClusterV1beta2().ManagedClusterSets().Get(
					context.TODO(), universalClusterSetName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				// Restore original managed namespaces
				clusterSet.Spec.ManagedNamespaces = originalManagedNamespaces

				_, err = hub.ClusterClient.ClusterV1beta2().ManagedClusterSets().Update(
					context.TODO(), clusterSet, metav1.UpdateOptions{})
				return err
			}, 30*time.Second, 2*time.Second).Should(gomega.Succeed())

			// Clean up any test namespaces that might have been created on the spoke cluster
			for _, nsName := range expectedNamespaces {
				err := spoke.KubeClient.CoreV1().Namespaces().Delete(
					context.TODO(), nsName, metav1.DeleteOptions{})
				if err != nil && !errors.IsNotFound(err) {
					ginkgo.GinkgoLogr.Error(err, "failed to delete test namespace", "namespace", nsName)
				}
			}
		})

		ginkgo.It("should update ManagedCluster status with managed namespaces from ManagedClusterSet", func() {
			ginkgo.By("Waiting for the hub-side managed namespace controller to update the ManagedCluster status")
			gomega.Eventually(func() bool {
				cluster, err := hub.ClusterClient.ClusterV1().ManagedClusters().Get(
					context.TODO(), universalClusterName, metav1.GetOptions{})
				if err != nil {
					return false
				}

				// Check if managed namespaces are populated in status
				if len(cluster.Status.ManagedNamespaces) != len(expectedNamespaces) {
					return false
				}

				// Verify all expected namespaces are present
				managedNSNames := make(map[string]bool)
				for _, managedNS := range cluster.Status.ManagedNamespaces {
					managedNSNames[managedNS.Name] = true
					if managedNS.ClusterSet != universalClusterSetName {
						return false
					}
				}

				for _, expectedNS := range expectedNamespaces {
					if !managedNSNames[expectedNS] {
						return false
					}
				}

				return true
			}, 60*time.Second, 2*time.Second).Should(gomega.BeTrue(),
				"ManagedCluster status should be updated with managed namespaces")
		})

		ginkgo.It("should create managed namespaces on spoke cluster with correct labels", func() {
			// Get expected hub cluster set label
			expectedLabel := managedcluster.GetHubClusterSetLabel(hubHash)

			ginkgo.By("Waiting for the spoke-side managed namespace controller to create namespaces")
			gomega.Eventually(func() bool {
				// List all namespaces with the expected label set to "true"
				namespaceList, err := spoke.KubeClient.CoreV1().Namespaces().List(
					context.TODO(), metav1.ListOptions{
						LabelSelector: fmt.Sprintf("%s=true", expectedLabel),
					})
				if err != nil {
					return false
				}

				// Create a set of found namespaces for comparison
				foundNamespaces := sets.Set[string]{}
				for _, ns := range namespaceList.Items {
					foundNamespaces.Insert(ns.Name)
				}

				// Check if all expected namespaces are found with the correct label
				return foundNamespaces.HasAll(expectedNamespaces...)
			}, 120*time.Second, 3*time.Second).Should(gomega.BeTrue(),
				"All expected namespaces should be created with correct hub-specific label")
		})

		ginkgo.It("should update managed namespace conditions when namespace creation succeeds", func() {
			ginkgo.By("Waiting for successful namespace creation conditions")
			gomega.Eventually(func() bool {
				cluster, err := hub.ClusterClient.ClusterV1().ManagedClusters().Get(
					context.TODO(), universalClusterName, metav1.GetOptions{})
				if err != nil {
					return false
				}

				// Check conditions on all managed namespaces
				for _, managedNS := range cluster.Status.ManagedNamespaces {
					condition := meta.FindStatusCondition(managedNS.Conditions, "NamespaceAvailable")
					if condition == nil || condition.Status != metav1.ConditionTrue {
						return false
					}
					if condition.Reason != "NamespaceApplied" {
						return false
					}
				}

				return len(cluster.Status.ManagedNamespaces) == len(expectedNamespaces)
			}, 120*time.Second, 3*time.Second).Should(gomega.BeTrue(),
				"All managed namespaces should have successful conditions")
		})

		ginkgo.It("should cleanup previously managed namespaces when removed from cluster set", func() {
			// Get expected hub cluster set label
			expectedLabel := managedcluster.GetHubClusterSetLabel(hubHash)

			ginkgo.By("Waiting for initial namespaces to be created")
			for _, expectedNS := range expectedNamespaces {
				nsName := expectedNS
				gomega.Eventually(func() error {
					_, err := spoke.KubeClient.CoreV1().Namespaces().Get(
						context.TODO(), nsName, metav1.GetOptions{})
					return err
				}, 120*time.Second, 3*time.Second).Should(gomega.Succeed())
			}

			ginkgo.By("Removing one namespace from the ManagedClusterSet")
			// Update the cluster set to remove the first namespace
			gomega.Eventually(func() error {
				clusterSet, err := hub.ClusterClient.ClusterV1beta2().ManagedClusterSets().Get(
					context.TODO(), universalClusterSetName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				// Remove the first namespace
				clusterSet.Spec.ManagedNamespaces = []clusterv1.ManagedNamespaceConfig{
					{Name: expectedNamespaces[1]}, // Keep only the second namespace
				}

				_, err = hub.ClusterClient.ClusterV1beta2().ManagedClusterSets().Update(
					context.TODO(), clusterSet, metav1.UpdateOptions{})
				return err
			}, 30*time.Second, 2*time.Second).Should(gomega.Succeed())

			ginkgo.By("Verifying the removed namespace label is set to 'false'")
			removedNS := expectedNamespaces[0]
			gomega.Eventually(func() bool {
				ns, err := spoke.KubeClient.CoreV1().Namespaces().Get(
					context.TODO(), removedNS, metav1.GetOptions{})
				if err != nil {
					return false
				}

				// Check if the label is now set to 'false'
				if labelValue, exists := ns.Labels[expectedLabel]; !exists || labelValue != "false" {
					return false
				}

				return true
			}, 120*time.Second, 3*time.Second).Should(gomega.BeTrue(),
				fmt.Sprintf("Removed namespace %s should have label set to 'false'", removedNS))

			ginkgo.By("Verifying the remaining namespace still has label set to 'true'")
			remainingNS := expectedNamespaces[1]
			gomega.Consistently(func() bool {
				ns, err := spoke.KubeClient.CoreV1().Namespaces().Get(
					context.TODO(), remainingNS, metav1.GetOptions{})
				if err != nil {
					return false
				}

				// Check if the label is still 'true'
				if labelValue, exists := ns.Labels[expectedLabel]; !exists || labelValue != "true" {
					return false
				}

				return true
			}, 30*time.Second, 2*time.Second).Should(gomega.BeTrue(),
				fmt.Sprintf("Remaining namespace %s should still have label set to 'true'", remainingNS))
		})
	})

	ginkgo.Context("ManagedNamespace with multiple cluster sets", func() {
		var additionalClusterSetName string
		var namespace1, namespace2 string
		var originalUniversalManagedNamespaces []clusterv1.ManagedNamespaceConfig

		ginkgo.BeforeEach(func() {
			suffix := rand.String(6)
			additionalClusterSetName = fmt.Sprintf("test-clusterset-%s", suffix)
			namespace1 = fmt.Sprintf("test-ns-1-%s", suffix)
			namespace2 = fmt.Sprintf("test-ns-2-%s", suffix)

			// Store original managed namespaces from universal cluster set
			gomega.Eventually(func() error {
				clusterSet, err := hub.ClusterClient.ClusterV1beta2().ManagedClusterSets().Get(
					context.TODO(), universalClusterSetName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				originalUniversalManagedNamespaces = clusterSet.Spec.ManagedNamespaces
				return nil
			}, 30*time.Second, 2*time.Second).Should(gomega.Succeed())

			// Add managed namespace to universal cluster set
			gomega.Eventually(func() error {
				clusterSet, err := hub.ClusterClient.ClusterV1beta2().ManagedClusterSets().Get(
					context.TODO(), universalClusterSetName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				clusterSet.Spec.ManagedNamespaces = append(originalUniversalManagedNamespaces, clusterv1.ManagedNamespaceConfig{Name: namespace1})

				_, err = hub.ClusterClient.ClusterV1beta2().ManagedClusterSets().Update(
					context.TODO(), clusterSet, metav1.UpdateOptions{})
				return err
			}, 30*time.Second, 2*time.Second).Should(gomega.Succeed())

			// Create additional ManagedClusterSet with LabelSelector
			additionalClusterSet := &clusterv1beta2.ManagedClusterSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: additionalClusterSetName,
				},
				Spec: clusterv1beta2.ManagedClusterSetSpec{
					ClusterSelector: clusterv1beta2.ManagedClusterSelector{
						SelectorType: clusterv1beta2.LabelSelector,
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"cluster.open-cluster-management.io/clusterset": universalClusterSetName,
							},
						},
					},
					ManagedNamespaces: []clusterv1.ManagedNamespaceConfig{
						{Name: namespace2},
					},
				},
			}

			_, err := hub.ClusterClient.ClusterV1beta2().ManagedClusterSets().Create(
				context.TODO(), additionalClusterSet, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		})

		ginkgo.AfterEach(func() {
			// Restore original managed namespaces in the universal cluster set
			gomega.Eventually(func() error {
				clusterSet, err := hub.ClusterClient.ClusterV1beta2().ManagedClusterSets().Get(
					context.TODO(), universalClusterSetName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				clusterSet.Spec.ManagedNamespaces = originalUniversalManagedNamespaces

				_, err = hub.ClusterClient.ClusterV1beta2().ManagedClusterSets().Update(
					context.TODO(), clusterSet, metav1.UpdateOptions{})
				return err
			}, 30*time.Second, 2*time.Second).Should(gomega.Succeed())

			// Clean up additional cluster set
			hub.ClusterClient.ClusterV1beta2().ManagedClusterSets().Delete(
				context.TODO(), additionalClusterSetName, metav1.DeleteOptions{})

			// Clean up test namespaces
			spoke.KubeClient.CoreV1().Namespaces().Delete(
				context.TODO(), namespace1, metav1.DeleteOptions{})
			spoke.KubeClient.CoreV1().Namespaces().Delete(
				context.TODO(), namespace2, metav1.DeleteOptions{})
		})

		ginkgo.It("should manage namespaces from multiple cluster sets", func() {
			ginkgo.By("Waiting for ManagedCluster status to include namespaces from both cluster sets")
			gomega.Eventually(func() bool {
				cluster, err := hub.ClusterClient.ClusterV1().ManagedClusters().Get(
					context.TODO(), universalClusterName, metav1.GetOptions{})
				if err != nil {
					return false
				}

				if len(cluster.Status.ManagedNamespaces) != 2 {
					return false
				}

				foundNamespaces := make(map[string]string) // namespace -> cluster set
				for _, managedNS := range cluster.Status.ManagedNamespaces {
					foundNamespaces[managedNS.Name] = managedNS.ClusterSet
				}

				return foundNamespaces[namespace1] == universalClusterSetName &&
					foundNamespaces[namespace2] == additionalClusterSetName

			}, 60*time.Second, 2*time.Second).Should(gomega.BeTrue(),
				"ManagedCluster should have namespaces from both cluster sets")

			ginkgo.By("Verifying both namespaces are created on the spoke cluster")
			gomega.Eventually(func() bool {
				expectedNamespaces := []string{namespace1, namespace2}
				for _, nsName := range expectedNamespaces {
					_, err := spoke.KubeClient.CoreV1().Namespaces().Get(
						context.TODO(), nsName, metav1.GetOptions{})
					if err != nil {
						return false
					}
				}
				return true
			}, 120*time.Second, 3*time.Second).Should(gomega.BeTrue(),
				"Both namespaces should be created on the spoke cluster")
		})
	})
})
