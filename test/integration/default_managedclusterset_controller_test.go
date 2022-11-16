package integration_test

import (
	"context"
	"fmt"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/equality"
	setcontroller "open-cluster-management.io/registration/pkg/hub/managedclusterset"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = ginkgo.Describe("DefaultManagedClusterSet", func() {

	ginkgo.It("should create DefaultManagedClusterSet successfully", func() {

		ginkgo.By("check whether DefaultManagedClusterSet is created")
		gomega.Eventually(func() error {
			mcs, err := clusterClient.ClusterV1beta1().ManagedClusterSets().Get(context.TODO(), setcontroller.DefaultManagedClusterSetName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if mcs.ObjectMeta.Name == setcontroller.DefaultManagedClusterSetName && equality.Semantic.DeepEqual(mcs.Spec, setcontroller.DefaultManagedClusterSet.Spec) {
				return nil
			}
			return fmt.Errorf("check not pass!")

		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	})

	ginkgo.It("should recreate DefaultManagedClusterSet successfully after deleted", func() {

		ginkgo.By("delete DefaultManagedClusterSet")
		err := clusterClient.ClusterV1beta1().ManagedClusterSets().Delete(context.TODO(), setcontroller.DefaultManagedClusterSetName, metav1.DeleteOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred(), "try to delete DefaultManagedClusterSet error")

		ginkgo.By("check whether DefaultManagedClusterSet is recreated")
		gomega.Eventually(func() error {
			mcs, err := clusterClient.ClusterV1beta1().ManagedClusterSets().Get(context.TODO(), setcontroller.DefaultManagedClusterSetName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if mcs.ObjectMeta.Name == setcontroller.DefaultManagedClusterSetName && equality.Semantic.DeepEqual(mcs.Spec, setcontroller.DefaultManagedClusterSet.Spec) {
				return nil
			}
			return fmt.Errorf("check not pass!")

		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	})
})
