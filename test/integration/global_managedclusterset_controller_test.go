package integration_test

import (
	"context"
	"fmt"
	"reflect"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/equality"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	setcontroller "open-cluster-management.io/registration/pkg/hub/managedclusterset"
)

var _ = ginkgo.Describe("GlobalManagedClusterSet", func() {
	ginkgo.It("should create GlobalManagedClusterSet successfully", func() {
		ginkgo.By("check whether GlobalManagedClusterSet is created")
		gomega.Eventually(func() error {
			mcs, err := clusterClient.ClusterV1beta1().ManagedClusterSets().Get(context.TODO(), setcontroller.GlobalManagedClusterSetName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if mcs.ObjectMeta.Name == setcontroller.GlobalManagedClusterSetName && reflect.DeepEqual(mcs.Spec, setcontroller.GlobalManagedClusterSet.Spec) {
				return nil
			}
			return fmt.Errorf("check not pass!")

		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	})

	ginkgo.It("should reconcile GlobalManagedClusterSet successfully if it changed", func() {
		ginkgo.By("check whether GlobalManagedClusterSet is reconciled after changed")
		mcs, err := clusterClient.ClusterV1beta1().ManagedClusterSets().Get(context.TODO(), setcontroller.GlobalManagedClusterSetName, metav1.GetOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		updateMcs := mcs.DeepCopy()
		updateMcs.Spec.ClusterSelector.LabelSelector = &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"vendor": "openshift",
			},
		}
		_, err = clusterClient.ClusterV1beta1().ManagedClusterSets().Update(context.TODO(), updateMcs, metav1.UpdateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			mcs, err := clusterClient.ClusterV1beta1().ManagedClusterSets().Get(context.TODO(), setcontroller.GlobalManagedClusterSetName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if mcs.ObjectMeta.Name == setcontroller.GlobalManagedClusterSetName && equality.Semantic.DeepEqual(mcs.Spec, setcontroller.GlobalManagedClusterSet.Spec) {
				return nil
			}
			return fmt.Errorf("check not pass!")
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	})

	ginkgo.It("should not change users labels/annotations in GlobalManagedClusterSet", func() {
		ginkgo.By("check whether GlobalManagedClusterSet labels/annotations reconciled after changed")
		mcs, err := clusterClient.ClusterV1beta1().ManagedClusterSets().Get(context.TODO(), setcontroller.GlobalManagedClusterSetName, metav1.GetOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		updateMcs := mcs.DeepCopy()
		updateMcs.Annotations = map[string]string{
			"annotation-test": "a1",
		}
		updateMcs.Labels = map[string]string{
			"label-test": "l1",
		}
		updateMcs.Spec.ClusterSelector.LabelSelector = &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"vendor": "openshift",
			},
		}
		_, err = clusterClient.ClusterV1beta1().ManagedClusterSets().Update(context.TODO(), updateMcs, metav1.UpdateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			mcs, err := clusterClient.ClusterV1beta1().ManagedClusterSets().Get(context.TODO(), setcontroller.GlobalManagedClusterSetName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if equality.Semantic.DeepEqual(mcs.Spec, setcontroller.GlobalManagedClusterSet.Spec) && equality.Semantic.DeepEqual(mcs.Annotations, updateMcs.Annotations) && equality.Semantic.DeepEqual(mcs.Labels, updateMcs.Labels) {
				return nil
			}
			return fmt.Errorf("check not pass!")
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	})

	ginkgo.It("should recreate GlobalManagedClusterSet successfully after deleted", func() {
		ginkgo.By("delete GlobalManagedClusterSet")
		err := clusterClient.ClusterV1beta1().ManagedClusterSets().Delete(context.TODO(), setcontroller.GlobalManagedClusterSetName, metav1.DeleteOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred(), "try to delete GlobalManagedClusterSet error")

		ginkgo.By("check whether GlobalManagedClusterSet is recreated")
		gomega.Eventually(func() error {
			mcs, err := clusterClient.ClusterV1beta1().ManagedClusterSets().Get(context.TODO(), setcontroller.GlobalManagedClusterSetName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if mcs.ObjectMeta.Name == setcontroller.GlobalManagedClusterSetName && equality.Semantic.DeepEqual(mcs.Spec, setcontroller.GlobalManagedClusterSet.Spec) {
				return nil
			}
			return fmt.Errorf("check not pass!")

		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	})

})
