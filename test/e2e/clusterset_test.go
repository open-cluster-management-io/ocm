package e2e

import (
	"context"
	"fmt"
	"reflect"

	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
)

const (
	clustersetCrdName        = "managedclustersets.cluster.open-cluster-management.io"
	clustersetBindingCrdName = "managedclustersetbindings.cluster.open-cluster-management.io"
)

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
		gomega.Eventually(func() error {
			_, err := t.ClusterClient.ClusterV1beta2().ManagedClusterSets().Create(context.Background(), managedClusterSet, metav1.CreateOptions{})
			return err
		}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(gomega.Succeed())

		ginkgo.By("Get v1beta2 ManagedClusterSet using v1beta2 client")
		gomega.Eventually(func() error {
			_, err := t.ClusterClient.ClusterV1beta2().ManagedClusterSets().Get(context.Background(), managedClusterSetName, metav1.GetOptions{})
			return err
		}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(gomega.Succeed())

		ginkgo.By("Update v1beta2 ManagedClusterSet using v1beta2 client")
		gomega.Eventually(func() error {
			managedClusterSet, err := t.ClusterClient.ClusterV1beta2().ManagedClusterSets().Get(context.Background(), managedClusterSetName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			updateManagedClusterSet := managedClusterSet.DeepCopy()
			updateManagedClusterSet.Spec.ClusterSelector.LabelSelector.MatchLabels = nil
			_, err = t.ClusterClient.ClusterV1beta2().ManagedClusterSets().Update(context.Background(), updateManagedClusterSet, metav1.UpdateOptions{})
			return err
		}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(gomega.Succeed())

		ginkgo.By("Delete v1beta2 ManagedClusterSet using v1beta2 client")
		err := t.ClusterClient.ClusterV1beta2().ManagedClusterSets().Delete(context.Background(), managedClusterSetName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
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
		gomega.Eventually(func() error {
			_, err := t.ClusterClient.ClusterV1beta2().ManagedClusterSets().Create(context.Background(), managedClusterSet, metav1.CreateOptions{})
			return err
		}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(gomega.Succeed())

		ginkgo.By("Get v1beta2 ManagedClusterSet using v1beta1 client")
		gomega.Eventually(func() bool {
			v1beta1ManagedClusterSet, err := t.ClusterClient.ClusterV1beta1().ManagedClusterSets().Get(context.Background(), managedClusterSetName, metav1.GetOptions{})
			if err != nil {
				return false
			}
			if !reflect.DeepEqual(string(v1beta1ManagedClusterSet.Spec.ClusterSelector.SelectorType), string(managedClusterSet.Spec.ClusterSelector.SelectorType)) {
				return false
			}
			if !reflect.DeepEqual(v1beta1ManagedClusterSet.Spec.ClusterSelector.LabelSelector.MatchLabels, managedClusterSet.Spec.ClusterSelector.LabelSelector.MatchLabels) {
				return false
			}
			return true
		}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(gomega.BeTrue())

		ginkgo.By("Update v1beta2 ManagedClusterSet using v1beta1 client")
		gomega.Eventually(func() error {
			updateManagedClusterSet, err := t.ClusterClient.ClusterV1beta1().ManagedClusterSets().Get(context.Background(), managedClusterSetName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			updateManagedClusterSet.Spec.ClusterSelector.LabelSelector.MatchLabels = nil
			_, err = t.ClusterClient.ClusterV1beta1().ManagedClusterSets().Update(context.Background(), updateManagedClusterSet, metav1.UpdateOptions{})
			return err
		}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(gomega.Succeed())

		ginkgo.By("Delete v1beta2 ManagedClusterSet using v1beta1 client")
		err := t.ClusterClient.ClusterV1beta1().ManagedClusterSets().Delete(context.Background(), managedClusterSetName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})
	ginkgo.It("Check if the v1beta1 storageversion is removed from clusterset crd", func() {
		gomega.Eventually(func() error {
			clustersetCrd, err := t.HubAPIExtensionClient.ApiextensionsV1().CustomResourceDefinitions().Get(context.Background(), clustersetCrdName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if len(clustersetCrd.Status.StoredVersions) != 1 {
				return fmt.Errorf("clustersetCrd.Status.StoredVersions should be v1beta2, but got:%v", clustersetCrd.Status.StoredVersions)
			}
			if clustersetCrd.Status.StoredVersions[0] != "v1beta2" {
				return fmt.Errorf("clustersetCrd.Status.StoredVersions should be v1beta2, but got:%v", clustersetCrd.Status.StoredVersions)
			}
			return nil
		}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(gomega.Succeed())
	})
	ginkgo.It("Check if the v1beta1 storageversion is removed from clustersetbinding crd", func() {
		gomega.Eventually(func() error {
			clustersetBindingCrd, err := t.HubAPIExtensionClient.ApiextensionsV1().CustomResourceDefinitions().Get(context.Background(), clustersetBindingCrdName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if len(clustersetBindingCrd.Status.StoredVersions) != 1 {
				return fmt.Errorf("clustersetBindingCrd.Status.StoredVersions should be v1beta2, but got:%v", clustersetBindingCrd.Status.StoredVersions)
			}
			if clustersetBindingCrd.Status.StoredVersions[0] != "v1beta2" {
				return fmt.Errorf("clustersetBindingCrd.Status.StoredVersions should be v1beta2, but got:%v", clustersetBindingCrd.Status.StoredVersions)
			}
			return nil
		}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(gomega.Succeed())

	})
})
