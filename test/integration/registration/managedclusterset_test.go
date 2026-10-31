package registration_test

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"

	"open-cluster-management.io/ocm/pkg/registration/hub/managedclusterset"
)

var _ = Describe("ManagedClusterSet", func() {
	It("should create empty cluster set and keep it synced successfully", func(ctx SpecContext) {
		for i := 0; i < 10; i++ {
			suffix := rand.String(6)
			By("Create a ManagedClusterSet")
			managedClusterSetName := fmt.Sprintf("cs1-%s", suffix)
			managedClusterSet := &clusterv1beta2.ManagedClusterSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: managedClusterSetName,
				},
			}

			_, err := clusterClient.ClusterV1beta2().ManagedClusterSets().Create(ctx, managedClusterSet, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			By("Check if ManagedClusterSet is reconciled")
			Eventually(func() bool {
				managedClusterSet, err = clusterClient.ClusterV1beta2().ManagedClusterSets().Get(ctx, managedClusterSetName, metav1.GetOptions{})
				if err != nil {
					return false
				}

				for _, condition := range managedClusterSet.Status.Conditions {
					if condition.Type != clusterv1beta2.ManagedClusterSetConditionEmpty {
						continue
					}
					if condition.Status != metav1.ConditionTrue {
						return false
					}
					if condition.Reason != managedclusterset.ReasonNoClusterMatchced {
						return false
					}
					return true
				}
				return false
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			By("Create a ManagedCluster")
			managedClusterName := fmt.Sprintf("managedcluster-%s", suffix)
			managedCluster := &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: managedClusterName,
					Labels: map[string]string{
						clusterv1beta2.ClusterSetLabel: managedClusterSetName,
					},
				},
				Spec: clusterv1.ManagedClusterSpec{
					HubAcceptsClient: true,
				},
			}

			_, err = clusterClient.ClusterV1().ManagedClusters().Create(ctx, managedCluster, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			By("Check if ManagedClusterSet is reconciled again")
			Eventually(func() bool {
				managedClusterSet, err = clusterClient.ClusterV1beta2().ManagedClusterSets().Get(ctx, managedClusterSetName, metav1.GetOptions{})
				if err != nil {
					return false
				}
				for _, condition := range managedClusterSet.Status.Conditions {
					if condition.Type != clusterv1beta2.ManagedClusterSetConditionEmpty {
						continue
					}
					if condition.Status != metav1.ConditionFalse {
						return false
					}
					if condition.Reason != managedclusterset.ReasonClusterSelected {
						return false
					}
					return true
				}
				return false
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			By("Move the cluster to another ManagedClusterSet and check if the original one is empty again")
			// create another clusterset
			managedClusterSet = &clusterv1beta2.ManagedClusterSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("cs2-%s", suffix),
				},
			}
			_, err = clusterClient.ClusterV1beta2().ManagedClusterSets().Create(ctx, managedClusterSet, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() error {
				// move the cluster to the new clusterset
				managedCluster, err = clusterClient.ClusterV1().ManagedClusters().Get(ctx, managedCluster.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}
				managedCluster.Labels = map[string]string{
					clusterv1beta2.ClusterSetLabel: managedClusterSet.Name,
				}
				_, err := clusterClient.ClusterV1().ManagedClusters().Update(ctx, managedCluster, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

			// check if the new clusterset synced
			Eventually(func() bool {
				managedClusterSet, err = clusterClient.ClusterV1beta2().ManagedClusterSets().Get(ctx, managedClusterSet.Name, metav1.GetOptions{})
				if err != nil {
					return false
				}
				for _, condition := range managedClusterSet.Status.Conditions {
					if condition.Type != clusterv1beta2.ManagedClusterSetConditionEmpty {
						continue
					}
					if condition.Status != metav1.ConditionFalse {
						return false
					}
					if condition.Reason != managedclusterset.ReasonClusterSelected {
						return false
					}
					return true
				}
				return false
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			// check if the original clusterset synced
			Eventually(func() error {
				managedClusterSet, err = clusterClient.ClusterV1beta2().ManagedClusterSets().Get(ctx, managedClusterSetName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				cond := meta.FindStatusCondition(managedClusterSet.Status.Conditions, clusterv1beta2.ManagedClusterSetConditionEmpty)
				if cond == nil {
					return fmt.Errorf("clusterset empty condition is not found")
				}

				if cond.Status != metav1.ConditionTrue {
					return fmt.Errorf("clusterset should be empty")
				}

				if cond.Reason != managedclusterset.ReasonNoClusterMatchced {
					return fmt.Errorf("clusterset condition reason not correct, got %q", cond.Reason)
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
		}
	})
	It("should create legacy cluster set and keep it synced successfully", func(ctx SpecContext) {
		suffix := rand.String(6)
		By("Create a ManagedClusterSet")
		managedClusterSetName := fmt.Sprintf("cs1-%s", suffix)
		managedClusterSet := &clusterv1beta2.ManagedClusterSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: managedClusterSetName,
			},
			Spec: clusterv1beta2.ManagedClusterSetSpec{
				ClusterSelector: clusterv1beta2.ManagedClusterSelector{
					SelectorType: clusterv1beta2.ExclusiveClusterSetLabel,
				},
			},
		}

		_, err := clusterClient.ClusterV1beta2().ManagedClusterSets().Create(ctx, managedClusterSet, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Check if ManagedClusterSet is reconciled")
		Eventually(func() bool {
			managedClusterSet, err = clusterClient.ClusterV1beta2().ManagedClusterSets().Get(ctx, managedClusterSetName, metav1.GetOptions{})
			if err != nil {
				return false
			}

			for _, condition := range managedClusterSet.Status.Conditions {
				if condition.Type != clusterv1beta2.ManagedClusterSetConditionEmpty {
					continue
				}
				if condition.Status != metav1.ConditionTrue {
					return false
				}
				if condition.Reason != managedclusterset.ReasonNoClusterMatchced {
					return false
				}
				return true
			}
			return false
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

		By("Create a ManagedCluster")
		managedClusterName := fmt.Sprintf("managedcluster-%s", suffix)
		managedCluster := &clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: managedClusterName,
				Labels: map[string]string{
					clusterv1beta2.ClusterSetLabel: managedClusterSetName,
				},
			},
			Spec: clusterv1.ManagedClusterSpec{
				HubAcceptsClient: true,
			},
		}

		_, err = clusterClient.ClusterV1().ManagedClusters().Create(ctx, managedCluster, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Check if ManagedClusterSet is reconciled again")
		Eventually(func() bool {
			managedClusterSet, err = clusterClient.ClusterV1beta2().ManagedClusterSets().Get(ctx, managedClusterSetName, metav1.GetOptions{})
			if err != nil {
				return false
			}
			for _, condition := range managedClusterSet.Status.Conditions {
				if condition.Type != clusterv1beta2.ManagedClusterSetConditionEmpty {
					continue
				}
				if condition.Status != metav1.ConditionFalse {
					return false
				}
				if condition.Reason != managedclusterset.ReasonClusterSelected {
					return false
				}
				return true
			}
			return false
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

		By("Move the cluster to another ManagedClusterSet and check if the original one is empty again")
		// create another clusterset
		managedClusterSet = &clusterv1beta2.ManagedClusterSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("cs2-%s", suffix),
			},
		}
		_, err = clusterClient.ClusterV1beta2().ManagedClusterSets().Create(ctx, managedClusterSet, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() error {
			// move the cluster to the new clusterset
			managedCluster, err = clusterClient.ClusterV1().ManagedClusters().Get(ctx, managedCluster.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}
			managedCluster.Labels = map[string]string{
				clusterv1beta2.ClusterSetLabel: managedClusterSet.Name,
			}
			_, err := clusterClient.ClusterV1().ManagedClusters().Update(ctx, managedCluster, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

		// check if the new clusterset synced
		Eventually(func() bool {
			managedClusterSet, err = clusterClient.ClusterV1beta2().ManagedClusterSets().Get(ctx, managedClusterSet.Name, metav1.GetOptions{})
			if err != nil {
				return false
			}
			for _, condition := range managedClusterSet.Status.Conditions {
				if condition.Type != clusterv1beta2.ManagedClusterSetConditionEmpty {
					continue
				}
				if condition.Status != metav1.ConditionFalse {
					return false
				}
				if condition.Reason != managedclusterset.ReasonClusterSelected {
					return false
				}
				return true
			}
			return false
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

		// check if the original clusterset synced
		Eventually(func() error {
			managedClusterSet, err = clusterClient.ClusterV1beta2().ManagedClusterSets().Get(ctx, managedClusterSetName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			cond := meta.FindStatusCondition(managedClusterSet.Status.Conditions, clusterv1beta2.ManagedClusterSetConditionEmpty)
			if cond == nil {
				return fmt.Errorf("clusterset empty condition is not found")
			}

			if cond.Status != metav1.ConditionTrue {
				return fmt.Errorf("clusterset should be empty")
			}

			if cond.Reason != managedclusterset.ReasonNoClusterMatchced {
				return fmt.Errorf("clusterset condition reason not correct, got %q", cond.Reason)
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
	})
	It("should create labelSelector cluster set and keep it synced successfully", func(ctx SpecContext) {
		suffix := rand.String(6)
		By("Create a ManagedClusterSet")
		managedClusterSetName := fmt.Sprintf("cs1-%s", suffix)
		managedClusterSet := &clusterv1beta2.ManagedClusterSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: managedClusterSetName,
			},
			Spec: clusterv1beta2.ManagedClusterSetSpec{
				ClusterSelector: clusterv1beta2.ManagedClusterSelector{
					SelectorType: clusterv1beta2.LabelSelector,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"vendor": "openShift",
						},
					},
				},
			},
		}

		_, err := clusterClient.ClusterV1beta2().ManagedClusterSets().Create(ctx, managedClusterSet, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Check if ManagedClusterSet is reconciled")
		Eventually(func() bool {
			managedClusterSet, err = clusterClient.ClusterV1beta2().ManagedClusterSets().Get(ctx, managedClusterSetName, metav1.GetOptions{})
			if err != nil {
				return false
			}

			for _, condition := range managedClusterSet.Status.Conditions {
				if condition.Type != clusterv1beta2.ManagedClusterSetConditionEmpty {
					continue
				}
				if condition.Status != metav1.ConditionTrue {
					return false
				}
				if condition.Reason != managedclusterset.ReasonNoClusterMatchced {
					return false
				}
				return true
			}
			return false
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

		By("Create a ManagedCluster")
		managedClusterName := fmt.Sprintf("managedcluster-%s", suffix)
		managedCluster := &clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: managedClusterName,
				Labels: map[string]string{
					"vendor": "openShift",
				},
			},
			Spec: clusterv1.ManagedClusterSpec{
				HubAcceptsClient: true,
			},
		}

		_, err = clusterClient.ClusterV1().ManagedClusters().Create(ctx, managedCluster, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Check if ManagedClusterSet is reconciled again")
		Eventually(func() bool {
			managedClusterSet, err = clusterClient.ClusterV1beta2().ManagedClusterSets().Get(ctx, managedClusterSetName, metav1.GetOptions{})
			if err != nil {
				return false
			}
			for _, condition := range managedClusterSet.Status.Conditions {
				if condition.Type != clusterv1beta2.ManagedClusterSetConditionEmpty {
					continue
				}
				if condition.Status != metav1.ConditionFalse {
					return false
				}
				if condition.Reason != managedclusterset.ReasonClusterSelected {
					return false
				}
				return true
			}
			return false
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

		By("Create a new ManagedClusterSet")
		newManagedClusterSetName := fmt.Sprintf("cs2-%s", suffix)
		newManagedClusterSet := &clusterv1beta2.ManagedClusterSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: newManagedClusterSetName,
			},
			Spec: clusterv1beta2.ManagedClusterSetSpec{
				ClusterSelector: clusterv1beta2.ManagedClusterSelector{
					SelectorType: clusterv1beta2.LabelSelector,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"region": "apac",
						},
					},
				},
			},
		}

		_, err = clusterClient.ClusterV1beta2().ManagedClusterSets().Create(ctx, newManagedClusterSet, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() error {
			// move the cluster to the new clusterset
			managedCluster, err = clusterClient.ClusterV1().ManagedClusters().Get(ctx, managedCluster.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}
			managedCluster.Labels = map[string]string{
				"region": "apac",
			}
			_, err = clusterClient.ClusterV1().ManagedClusters().Update(ctx, managedCluster, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

		// check if the new clusterset synced
		Eventually(func() bool {
			newManagedClusterSet, err = clusterClient.ClusterV1beta2().ManagedClusterSets().Get(ctx, newManagedClusterSet.Name, metav1.GetOptions{})
			if err != nil {
				return false
			}
			for _, condition := range managedClusterSet.Status.Conditions {
				if condition.Type != clusterv1beta2.ManagedClusterSetConditionEmpty {
					continue
				}
				if condition.Status != metav1.ConditionFalse {
					return false
				}
				if condition.Reason != managedclusterset.ReasonClusterSelected {
					return false
				}
				return true
			}
			return false
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

		// check if the original clusterset synced
		Eventually(func() error {
			managedClusterSet, err = clusterClient.ClusterV1beta2().ManagedClusterSets().Get(ctx, managedClusterSetName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			cond := meta.FindStatusCondition(managedClusterSet.Status.Conditions, clusterv1beta2.ManagedClusterSetConditionEmpty)
			if cond == nil {
				return fmt.Errorf("clusterset empty condition is not found")
			}

			if cond.Status != metav1.ConditionTrue {
				return fmt.Errorf("clusterset should be empty")
			}

			if cond.Reason != managedclusterset.ReasonNoClusterMatchced {
				return fmt.Errorf("clusterset condition reason not correct, got %q", cond.Reason)
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

	})
	It("should create global cluster set and keep it synced successfully", func(ctx SpecContext) {
		suffix := rand.String(6)
		By("Create a ManagedCluster")
		managedClusterName := fmt.Sprintf("managedcluster-%s", suffix)
		managedCluster := &clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: managedClusterName,
				Labels: map[string]string{
					"vendor": "openShift",
				},
			},
			Spec: clusterv1.ManagedClusterSpec{
				HubAcceptsClient: true,
			},
		}

		_, err := clusterClient.ClusterV1().ManagedClusters().Create(ctx, managedCluster, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Create a ManagedClusterSet")
		managedClusterSetName := fmt.Sprintf("cs1-%s", suffix)
		managedClusterSet := &clusterv1beta2.ManagedClusterSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: managedClusterSetName,
			},
			Spec: clusterv1beta2.ManagedClusterSetSpec{
				ClusterSelector: clusterv1beta2.ManagedClusterSelector{
					SelectorType:  clusterv1beta2.LabelSelector,
					LabelSelector: &metav1.LabelSelector{},
				},
			},
		}

		_, err = clusterClient.ClusterV1beta2().ManagedClusterSets().Create(ctx, managedClusterSet, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Check if ManagedClusterSet is reconciled")
		Eventually(func() bool {
			managedClusterSet, err = clusterClient.ClusterV1beta2().ManagedClusterSets().Get(ctx, managedClusterSetName, metav1.GetOptions{})
			if err != nil {
				return false
			}
			for _, condition := range managedClusterSet.Status.Conditions {
				if condition.Type != clusterv1beta2.ManagedClusterSetConditionEmpty {
					continue
				}
				if condition.Status != metav1.ConditionFalse {
					return false
				}
				if condition.Reason != managedclusterset.ReasonClusterSelected {
					return false
				}
				return true
			}
			return false
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
	})

})
