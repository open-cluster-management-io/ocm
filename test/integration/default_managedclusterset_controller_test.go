package integration_test

import (
	"context"
	"fmt"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/equality"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
)

var _ = ginkgo.Describe("DefaultManagedClusterSet", func() {
	var (
		defaultClusterSetName        = "default"
		defaultManagedClusterSetSpec = clusterv1beta1.ManagedClusterSetSpec{
			ClusterSelector: clusterv1beta1.ManagedClusterSelector{
				SelectorType: clusterv1beta1.LegacyClusterSetLabel,
			},
		}
	)

	ginkgo.It("should create DefaultManagedClusterSet successfully", func() {

		ginkgo.By("check whether DefaultManagedClusterSet is created")
		gomega.Eventually(func() error {
			mcs, err := clusterClient.ClusterV1beta1().ManagedClusterSets().Get(context.TODO(), defaultClusterSetName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if mcs.ObjectMeta.Name == defaultClusterSetName && equality.Semantic.DeepEqual(mcs.Spec, defaultManagedClusterSetSpec) {
				return nil
			}
			return fmt.Errorf("check not pass!")

		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	})

	ginkgo.It("should recreate DefaultManagedClusterSet successfully after deleted", func() {

		ginkgo.By("delete DefaultManagedClusterSet")
		err := clusterClient.ClusterV1beta1().ManagedClusterSets().Delete(context.TODO(), defaultClusterSetName, metav1.DeleteOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred(), "try to delete DefaultManagedClusterSet error")

		ginkgo.By("check whether DefaultManagedClusterSet is recreated")
		gomega.Eventually(func() error {
			mcs, err := clusterClient.ClusterV1beta1().ManagedClusterSets().Get(context.TODO(), defaultClusterSetName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if mcs.ObjectMeta.Name == defaultClusterSetName && equality.Semantic.DeepEqual(mcs.Spec, defaultManagedClusterSetSpec) {
				return nil
			}
			return fmt.Errorf("check not pass!")

		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	})

})
