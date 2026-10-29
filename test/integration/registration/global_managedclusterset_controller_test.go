package registration_test

import (
	"context"
	"fmt"
	"reflect"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	setcontroller "open-cluster-management.io/ocm/pkg/registration/hub/managedclusterset"
)

var _ = Describe("GlobalManagedClusterSet", func() {
	It("should create GlobalManagedClusterSet successfully", func() {
		By("check whether GlobalManagedClusterSet is created")
		Eventually(func() error {
			mcs, err := clusterClient.ClusterV1beta2().ManagedClusterSets().Get(context.TODO(), setcontroller.GlobalManagedClusterSetName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if mcs.ObjectMeta.Name == setcontroller.GlobalManagedClusterSetName && reflect.DeepEqual(mcs.Spec, setcontroller.GlobalManagedClusterSet.Spec) {
				return nil
			}
			return fmt.Errorf("check not pass")

		}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
	})

	It("should reconcile GlobalManagedClusterSet successfully if it changed", func() {
		By("check whether GlobalManagedClusterSet is reconciled after changed")
		mcs, err := clusterClient.ClusterV1beta2().ManagedClusterSets().Get(context.TODO(), setcontroller.GlobalManagedClusterSetName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		updateMcs := mcs.DeepCopy()
		updateMcs.Spec.ClusterSelector.LabelSelector = &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"vendor": "openshift",
			},
		}
		_, err = clusterClient.ClusterV1beta2().ManagedClusterSets().Update(context.TODO(), updateMcs, metav1.UpdateOptions{})
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() error {
			mcs, err := clusterClient.ClusterV1beta2().ManagedClusterSets().Get(context.TODO(), setcontroller.GlobalManagedClusterSetName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if mcs.ObjectMeta.Name == setcontroller.GlobalManagedClusterSetName && equality.Semantic.DeepEqual(mcs.Spec, setcontroller.GlobalManagedClusterSet.Spec) {
				return nil
			}
			return fmt.Errorf("check not pass")
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
	})

	It("should not change users labels/annotations in GlobalManagedClusterSet", func() {
		By("check whether GlobalManagedClusterSet labels/annotations reconciled after changed")
		mcs, err := clusterClient.ClusterV1beta2().ManagedClusterSets().Get(context.TODO(), setcontroller.GlobalManagedClusterSetName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

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
		_, err = clusterClient.ClusterV1beta2().ManagedClusterSets().Update(context.TODO(), updateMcs, metav1.UpdateOptions{})
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() error {
			mcs, err := clusterClient.ClusterV1beta2().ManagedClusterSets().Get(context.TODO(), setcontroller.GlobalManagedClusterSetName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if equality.Semantic.DeepEqual(mcs.Spec, setcontroller.GlobalManagedClusterSet.Spec) &&
				equality.Semantic.DeepEqual(mcs.Annotations, updateMcs.Annotations) &&
				equality.Semantic.DeepEqual(mcs.Labels, updateMcs.Labels) {
				return nil
			}
			return fmt.Errorf("check not pass")
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
	})

	It("should recreate GlobalManagedClusterSet successfully after deleted", func() {
		By("delete GlobalManagedClusterSet")
		err := clusterClient.ClusterV1beta2().ManagedClusterSets().Delete(context.TODO(), setcontroller.GlobalManagedClusterSetName, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred(), "try to delete GlobalManagedClusterSet error")

		By("check whether GlobalManagedClusterSet is recreated")
		Eventually(func() error {
			mcs, err := clusterClient.ClusterV1beta2().ManagedClusterSets().Get(context.TODO(), setcontroller.GlobalManagedClusterSetName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if mcs.ObjectMeta.Name == setcontroller.GlobalManagedClusterSetName && equality.Semantic.DeepEqual(mcs.Spec, setcontroller.GlobalManagedClusterSet.Spec) {
				return nil
			}
			return fmt.Errorf("check not pass")

		}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
	})

})
