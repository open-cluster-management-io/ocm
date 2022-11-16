package e2e

import (
	"context"
	"fmt"
	"reflect"

	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
)

var _ = ginkgo.Describe("Create v1beta1 managedclusterset", func() {
	ginkgo.It("Create a v1beta1 labelselector based ManagedClusterSet and get/update/delete with v1beta1 client", func() {
		ginkgo.By("Create a v1beta1 ManagedClusterSet")
		suffix := rand.String(6)
		managedClusterSetName := fmt.Sprintf("cs1-%s", suffix)
		managedClusterSet := &clusterv1beta1.ManagedClusterSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: managedClusterSetName,
			},
			Spec: clusterv1beta1.ManagedClusterSetSpec{
				ClusterSelector: clusterv1beta1.ManagedClusterSelector{
					SelectorType: clusterv1beta1.LabelSelector,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"vendor": "openShift",
						},
					},
				},
			},
		}
		_, err := clusterClient.ClusterV1beta1().ManagedClusterSets().Create(context.Background(), managedClusterSet, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Update v1beta1 ManagedClusterSet using v1beta1 client")
		gomega.Eventually(func() error {
			updateManagedClusterSet, err := clusterClient.ClusterV1beta1().ManagedClusterSets().Get(context.Background(), managedClusterSetName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			updateManagedClusterSet.Spec.ClusterSelector.LabelSelector.MatchLabels = nil
			_, err = clusterClient.ClusterV1beta1().ManagedClusterSets().Update(context.Background(), updateManagedClusterSet, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Delete v1beta1 ManagedClusterSet using v1beta1 client")
		gomega.Eventually(func() error {
			return clusterClient.ClusterV1beta1().ManagedClusterSets().Delete(context.Background(), managedClusterSetName, metav1.DeleteOptions{})
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	})

	ginkgo.It("Create a v1beta1 labelselector based ManagedClusterSet and get/update/delete with v1beta2 client", func() {
		ginkgo.By("Create a v1beta1 ManagedClusterSet")
		suffix := rand.String(6)
		managedClusterSetName := fmt.Sprintf("cs1-%s", suffix)
		managedClusterSet := &clusterv1beta1.ManagedClusterSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: managedClusterSetName,
			},
			Spec: clusterv1beta1.ManagedClusterSetSpec{
				ClusterSelector: clusterv1beta1.ManagedClusterSelector{
					SelectorType: clusterv1beta1.LabelSelector,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"vendor": "openShift",
						},
					},
				},
			},
		}
		_, err := clusterClient.ClusterV1beta1().ManagedClusterSets().Create(context.Background(), managedClusterSet, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Get v1beta1 ManagedClusterSet using v1beta2 client")
		gomega.Eventually(func() error {
			v1beta2ManagedClusterSet, err := clusterClient.ClusterV1beta2().ManagedClusterSets().Get(context.Background(), managedClusterSetName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if string(v1beta2ManagedClusterSet.Spec.ClusterSelector.SelectorType) != string(managedClusterSet.Spec.ClusterSelector.SelectorType) {
				return fmt.Errorf("unexpected v1beta2 cluster set %v", v1beta2ManagedClusterSet)
			}
			if !reflect.DeepEqual(v1beta2ManagedClusterSet.Spec.ClusterSelector.LabelSelector.MatchLabels, managedClusterSet.Spec.ClusterSelector.LabelSelector.MatchLabels) {
				return fmt.Errorf("unexpected v1beta2 cluster set %v", v1beta2ManagedClusterSet)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Update v1beta1 ManagedClusterSet using v1beta2 client")
		gomega.Eventually(func() error {
			updateManagedClusterSet, err := clusterClient.ClusterV1beta2().ManagedClusterSets().Get(context.Background(), managedClusterSetName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			updateManagedClusterSet.Spec.ClusterSelector.LabelSelector.MatchLabels = nil
			_, err = clusterClient.ClusterV1beta2().ManagedClusterSets().Update(context.Background(), updateManagedClusterSet, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Delete v1beta1 ManagedClusterSet using v1beta2 client")
		gomega.Eventually(func() error {
			return clusterClient.ClusterV1beta2().ManagedClusterSets().Delete(context.Background(), managedClusterSetName, metav1.DeleteOptions{})
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	})

	ginkgo.It("Create a v1beta1 legacy ManagedClusterSet and get/update/delete with v1beta2 client", func() {
		ginkgo.By("Create a v1beta1 legacy ManagedClusterSet")
		suffix := rand.String(6)
		managedClusterSetName := fmt.Sprintf("cs1-%s", suffix)
		managedClusterSet := &clusterv1beta1.ManagedClusterSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: managedClusterSetName,
			},
			Spec: clusterv1beta1.ManagedClusterSetSpec{
				ClusterSelector: clusterv1beta1.ManagedClusterSelector{
					SelectorType: clusterv1beta1.LegacyClusterSetLabel,
				},
			},
		}
		_, err := clusterClient.ClusterV1beta1().ManagedClusterSets().Create(context.Background(), managedClusterSet, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Get v1beta1 ManagedClusterSet using v1beta2 client")
		gomega.Eventually(func() error {
			v1beta2ManagedClusterSet, err := clusterClient.ClusterV1beta2().ManagedClusterSets().Get(context.Background(), managedClusterSetName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if v1beta2ManagedClusterSet.Spec.ClusterSelector.SelectorType != clusterv1beta2.ExclusiveClusterSetLabel {
				return fmt.Errorf("unexpected v1beta2 cluster set %v", v1beta2ManagedClusterSet)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Delete v1beta1 ManagedClusterSet using v1beta2 client")
		gomega.Eventually(func() error {
			return clusterClient.ClusterV1beta2().ManagedClusterSets().Delete(context.Background(), managedClusterSetName, metav1.DeleteOptions{})
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	})
})

var _ = ginkgo.Describe("Create v1beta2 managedclusterset", func() {
	ginkgo.It("Create a v1beta2 labelselector based ManagedClusterSet and get/update/delete with v1beta2 client", func() {
		ginkgo.By("Create a v1beta2 ManagedClusterSet")
		suffix := rand.String(6)
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
		_, err := clusterClient.ClusterV1beta2().ManagedClusterSets().Create(context.Background(), managedClusterSet, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Update v1beta2 ManagedClusterSet using v1beta2 client")
		gomega.Eventually(func() error {
			updateManagedClusterSet, err := clusterClient.ClusterV1beta2().ManagedClusterSets().Get(context.Background(), managedClusterSetName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			updateManagedClusterSet.Spec.ClusterSelector.LabelSelector.MatchLabels = nil
			_, err = clusterClient.ClusterV1beta2().ManagedClusterSets().Update(context.Background(), updateManagedClusterSet, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Delete v1beta2 ManagedClusterSet using v1beta2 client")
		gomega.Eventually(func() error {
			return clusterClient.ClusterV1beta2().ManagedClusterSets().Delete(context.Background(), managedClusterSetName, metav1.DeleteOptions{})
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	})

	ginkgo.It("Create a v1beta2 labelselector based ManagedClusterSet and get/update/delete with v1beta1 client", func() {
		ginkgo.By("Create a v1beta2 ManagedClusterSet")
		suffix := rand.String(6)
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
		_, err := clusterClient.ClusterV1beta2().ManagedClusterSets().Create(context.Background(), managedClusterSet, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Get v1beta2 ManagedClusterSet using v1beta1 client")
		gomega.Eventually(func() error {
			v1beta1ManagedClusterSet, err := clusterClient.ClusterV1beta1().ManagedClusterSets().Get(context.Background(), managedClusterSetName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if string(v1beta1ManagedClusterSet.Spec.ClusterSelector.SelectorType) != string(managedClusterSet.Spec.ClusterSelector.SelectorType) {
				return fmt.Errorf("unexpected v1beta1 cluster set %v", v1beta1ManagedClusterSet)
			}
			if !reflect.DeepEqual(v1beta1ManagedClusterSet.Spec.ClusterSelector.LabelSelector.MatchLabels, managedClusterSet.Spec.ClusterSelector.LabelSelector.MatchLabels) {
				return fmt.Errorf("unexpected v1beta1 cluster set %v", v1beta1ManagedClusterSet)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Update v1beta2 ManagedClusterSet using v1beta1 client")
		gomega.Eventually(func() error {
			updateManagedClusterSet, err := clusterClient.ClusterV1beta1().ManagedClusterSets().Get(context.Background(), managedClusterSetName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			updateManagedClusterSet.Spec.ClusterSelector.LabelSelector.MatchLabels = nil
			_, err = clusterClient.ClusterV1beta1().ManagedClusterSets().Update(context.Background(), updateManagedClusterSet, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Delete v1beta2 ManagedClusterSet using v1beta1 client")
		gomega.Eventually(func() error {
			return clusterClient.ClusterV1beta1().ManagedClusterSets().Delete(context.Background(), managedClusterSetName, metav1.DeleteOptions{})
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	})

	ginkgo.It("Create a v1beta2 legacy ManagedClusterSet and get/update/delete with v1beta1 client", func() {
		ginkgo.By("Create a v1beta2 legacy ManagedClusterSet")
		suffix := rand.String(6)
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
		_, err := clusterClient.ClusterV1beta2().ManagedClusterSets().Create(context.Background(), managedClusterSet, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Get v1beta2 ManagedClusterSet using v1beta2 client")
		gomega.Eventually(func() error {
			v1beta1ManagedClusterSet, err := clusterClient.ClusterV1beta1().ManagedClusterSets().Get(context.Background(), managedClusterSetName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if v1beta1ManagedClusterSet.Spec.ClusterSelector.SelectorType != clusterv1beta1.LegacyClusterSetLabel {
				return fmt.Errorf("unexpected v1beta1 cluster set %v", v1beta1ManagedClusterSet)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Delete v1beta2 ManagedClusterSet using v1beta1 client")
		gomega.Eventually(func() error {
			return clusterClient.ClusterV1beta1().ManagedClusterSets().Delete(context.Background(), managedClusterSetName, metav1.DeleteOptions{})
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	})
})
