package registration_test

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	setcontroller "open-cluster-management.io/ocm/pkg/registration/hub/managedclusterset"
)

var _ = Describe("DefaultManagedClusterSet", func() {

	It("should create DefaultManagedClusterSet successfully", func(ctx SpecContext) {

		By("check whether DefaultManagedClusterSet is created")
		Eventually(func() error {
			mcs, err := clusterClient.ClusterV1beta2().ManagedClusterSets().Get(ctx, setcontroller.DefaultManagedClusterSetName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if mcs.ObjectMeta.Name == setcontroller.DefaultManagedClusterSetName && equality.Semantic.DeepEqual(mcs.Spec, setcontroller.DefaultManagedClusterSet.Spec) {
				return nil
			}
			return fmt.Errorf("check not pass!")

		}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
	})

	It("should recreate DefaultManagedClusterSet successfully after deleted", func(ctx SpecContext) {

		By("delete DefaultManagedClusterSet")
		err := clusterClient.ClusterV1beta2().ManagedClusterSets().Delete(ctx, setcontroller.DefaultManagedClusterSetName, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred(), "try to delete DefaultManagedClusterSet error")

		By("check whether DefaultManagedClusterSet is recreated")
		Eventually(func() error {
			mcs, err := clusterClient.ClusterV1beta2().ManagedClusterSets().Get(ctx, setcontroller.DefaultManagedClusterSetName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if mcs.ObjectMeta.Name == setcontroller.DefaultManagedClusterSetName && equality.Semantic.DeepEqual(mcs.Spec, setcontroller.DefaultManagedClusterSet.Spec) {
				return nil
			}
			return fmt.Errorf("check not pass!")

		}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
	})
})
