package e2e

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/sets"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"

	"open-cluster-management.io/ocm/pkg/registration/spoke/managedcluster"
)

var _ = Describe("ManagedNamespace", func() {

	Context("ManagedClusterSet with ManagedNamespaces", func() {
		var expectedNamespaces []string
		var originalManagedNamespaces []clusterv1.ManagedNamespaceConfig

		BeforeEach(func(ctx SpecContext) {
			suffix := rand.String(6)
			expectedNamespaces = []string{
				fmt.Sprintf("test-ns-1-%s", suffix),
				fmt.Sprintf("test-ns-2-%s", suffix),
			}

			// Update the existing universal cluster set with managed namespaces
			Eventually(func() error {
				clusterSet, err := hub.ClusterClient.ClusterV1beta2().ManagedClusterSets().Get(
					ctx, universalClusterSetName, metav1.GetOptions{})
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
					ctx, clusterSet, metav1.UpdateOptions{})
				return err
			}, 30*time.Second, 2*time.Second).Should(Succeed())
		})

		AfterEach(func(ctx SpecContext) {
			// Restore original managed namespaces in the universal cluster set
			Eventually(func() error {
				clusterSet, err := hub.ClusterClient.ClusterV1beta2().ManagedClusterSets().Get(
					ctx, universalClusterSetName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				// Restore original managed namespaces
				clusterSet.Spec.ManagedNamespaces = originalManagedNamespaces

				_, err = hub.ClusterClient.ClusterV1beta2().ManagedClusterSets().Update(
					ctx, clusterSet, metav1.UpdateOptions{})
				return err
			}, 30*time.Second, 2*time.Second).Should(Succeed())

			// Clean up any test namespaces that might have been created on the spoke cluster
			for _, nsName := range expectedNamespaces {
				err := spoke.KubeClient.CoreV1().Namespaces().Delete(
					ctx, nsName, metav1.DeleteOptions{})
				if err != nil && !errors.IsNotFound(err) {
					GinkgoLogr.Error(err, "failed to delete test namespace", "namespace", nsName)
				}
			}
		})

		It("should update ManagedCluster status with managed namespaces from ManagedClusterSet", func(ctx SpecContext) {
			By("Waiting for the hub-side managed namespace controller to update the ManagedCluster status")
			Eventually(func() bool {
				cluster, err := hub.ClusterClient.ClusterV1().ManagedClusters().Get(
					ctx, universalClusterName, metav1.GetOptions{})
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
			}, 60*time.Second, 2*time.Second).Should(BeTrue(),
				"ManagedCluster status should be updated with managed namespaces")
		})

		It("should create managed namespaces on spoke cluster with correct labels", func(ctx SpecContext) {
			// Get expected hub cluster set label
			expectedLabel := managedcluster.GetHubClusterSetLabel(hubHash)

			By("Waiting for the spoke-side managed namespace controller to create namespaces")
			Eventually(func() bool {
				// List all namespaces with the expected label set to "true"
				namespaceList, err := spoke.KubeClient.CoreV1().Namespaces().List(
					ctx, metav1.ListOptions{
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
			}, 120*time.Second, 3*time.Second).Should(BeTrue(),
				"All expected namespaces should be created with correct hub-specific label")
		})

		It("should update managed namespace conditions when namespace creation succeeds", func(ctx SpecContext) {
			By("Waiting for successful namespace creation conditions")
			Eventually(func() bool {
				cluster, err := hub.ClusterClient.ClusterV1().ManagedClusters().Get(
					ctx, universalClusterName, metav1.GetOptions{})
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
			}, 120*time.Second, 3*time.Second).Should(BeTrue(),
				"All managed namespaces should have successful conditions")
		})

		It("should cleanup previously managed namespaces when removed from cluster set", func(ctx SpecContext) {
			// Get expected hub cluster set label
			expectedLabel := managedcluster.GetHubClusterSetLabel(hubHash)

			By("Waiting for initial namespaces to be created")
			for _, expectedNS := range expectedNamespaces {
				nsName := expectedNS
				Eventually(func() error {
					_, err := spoke.KubeClient.CoreV1().Namespaces().Get(
						ctx, nsName, metav1.GetOptions{})
					return err
				}, 120*time.Second, 3*time.Second).Should(Succeed())
			}

			By("Removing one namespace from the ManagedClusterSet")
			// Update the cluster set to remove the first namespace
			Eventually(func() error {
				clusterSet, err := hub.ClusterClient.ClusterV1beta2().ManagedClusterSets().Get(
					ctx, universalClusterSetName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				// Remove the first namespace
				clusterSet.Spec.ManagedNamespaces = []clusterv1.ManagedNamespaceConfig{
					{Name: expectedNamespaces[1]}, // Keep only the second namespace
				}

				_, err = hub.ClusterClient.ClusterV1beta2().ManagedClusterSets().Update(
					ctx, clusterSet, metav1.UpdateOptions{})
				return err
			}, 30*time.Second, 2*time.Second).Should(Succeed())

			By("Verifying the removed namespace label is set to 'false'")
			removedNS := expectedNamespaces[0]
			Eventually(func() bool {
				ns, err := spoke.KubeClient.CoreV1().Namespaces().Get(
					ctx, removedNS, metav1.GetOptions{})
				if err != nil {
					return false
				}

				// Check if the label is now set to 'false'
				if labelValue, exists := ns.Labels[expectedLabel]; !exists || labelValue != "false" {
					return false
				}

				return true
			}, 120*time.Second, 3*time.Second).Should(BeTrue(),
				fmt.Sprintf("Removed namespace %s should have label set to 'false'", removedNS))

			By("Verifying the remaining namespace still has label set to 'true'")
			remainingNS := expectedNamespaces[1]
			Consistently(func() bool {
				ns, err := spoke.KubeClient.CoreV1().Namespaces().Get(
					ctx, remainingNS, metav1.GetOptions{})
				if err != nil {
					return false
				}

				// Check if the label is still 'true'
				if labelValue, exists := ns.Labels[expectedLabel]; !exists || labelValue != "true" {
					return false
				}

				return true
			}, 30*time.Second, 2*time.Second).Should(BeTrue(),
				fmt.Sprintf("Remaining namespace %s should still have label set to 'true'", remainingNS))
		})
	})

	Context("ManagedNamespace with multiple cluster sets", func() {
		var additionalClusterSetName string
		var namespace1, namespace2 string
		var originalUniversalManagedNamespaces []clusterv1.ManagedNamespaceConfig

		BeforeEach(func(ctx SpecContext) {
			suffix := rand.String(6)
			additionalClusterSetName = fmt.Sprintf("test-clusterset-%s", suffix)
			namespace1 = fmt.Sprintf("test-ns-1-%s", suffix)
			namespace2 = fmt.Sprintf("test-ns-2-%s", suffix)

			// Store original managed namespaces from universal cluster set
			Eventually(func() error {
				clusterSet, err := hub.ClusterClient.ClusterV1beta2().ManagedClusterSets().Get(
					ctx, universalClusterSetName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				originalUniversalManagedNamespaces = clusterSet.Spec.ManagedNamespaces
				return nil
			}, 30*time.Second, 2*time.Second).Should(Succeed())

			// Add managed namespace to universal cluster set
			Eventually(func() error {
				clusterSet, err := hub.ClusterClient.ClusterV1beta2().ManagedClusterSets().Get(
					ctx, universalClusterSetName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				clusterSet.Spec.ManagedNamespaces = append(originalUniversalManagedNamespaces, clusterv1.ManagedNamespaceConfig{Name: namespace1})

				_, err = hub.ClusterClient.ClusterV1beta2().ManagedClusterSets().Update(
					ctx, clusterSet, metav1.UpdateOptions{})
				return err
			}, 30*time.Second, 2*time.Second).Should(Succeed())

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
				ctx, additionalClusterSet, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func(ctx SpecContext) {
			// Restore original managed namespaces in the universal cluster set
			Eventually(func() error {
				clusterSet, err := hub.ClusterClient.ClusterV1beta2().ManagedClusterSets().Get(
					ctx, universalClusterSetName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				clusterSet.Spec.ManagedNamespaces = originalUniversalManagedNamespaces

				_, err = hub.ClusterClient.ClusterV1beta2().ManagedClusterSets().Update(
					ctx, clusterSet, metav1.UpdateOptions{})
				return err
			}, 30*time.Second, 2*time.Second).Should(Succeed())

			// Clean up additional cluster set
			err := hub.ClusterClient.ClusterV1beta2().ManagedClusterSets().Delete(
				ctx, additionalClusterSetName, metav1.DeleteOptions{})
			if !errors.IsNotFound(err) {
				Expect(err).ToNot(HaveOccurred())
			}

			// Clean up test namespaces
			Expect(spoke.KubeClient.CoreV1().Namespaces().Delete(
				ctx, namespace1, metav1.DeleteOptions{})).ToNot(HaveOccurred())
			Expect(spoke.KubeClient.CoreV1().Namespaces().Delete(
				ctx, namespace2, metav1.DeleteOptions{})).ToNot(HaveOccurred())
		})

		It("should manage namespaces from multiple cluster sets", func(ctx SpecContext) {
			By("Waiting for ManagedCluster status to include namespaces from both cluster sets")
			Eventually(func() bool {
				cluster, err := hub.ClusterClient.ClusterV1().ManagedClusters().Get(
					ctx, universalClusterName, metav1.GetOptions{})
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

			}, 60*time.Second, 2*time.Second).Should(BeTrue(),
				"ManagedCluster should have namespaces from both cluster sets")

			By("Verifying both namespaces are created on the spoke cluster")
			Eventually(func() bool {
				expectedNamespaces := []string{namespace1, namespace2}
				for _, nsName := range expectedNamespaces {
					_, err := spoke.KubeClient.CoreV1().Namespaces().Get(
						ctx, nsName, metav1.GetOptions{})
					if err != nil {
						return false
					}
				}
				return true
			}, 120*time.Second, 3*time.Second).Should(BeTrue(),
				"Both namespaces should be created on the spoke cluster")
		})
	})
})
