package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
)

var _ = Describe("ManagedClusterSetBinding", func() {

	Context("ManagedClusterSetBinding", func() {
		var namespace string
		BeforeEach(func() {
			// create a namespace for testing
			namespace = fmt.Sprintf("ns-%s", rand.String(6))
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespace,
				},
			}
			_, err := hub.KubeClient.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			// make sure the managedclustersetbinding can be created successfully
			Eventually(func() error {
				clusterSetName := fmt.Sprintf("clusterset-%s", rand.String(6))
				managedClusterSetBinding := newManagedClusterSetBinding(namespace, clusterSetName, clusterSetName)
				_, err := hub.ClusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).
					Create(context.TODO(), managedClusterSetBinding, metav1.CreateOptions{})
				if err != nil {
					return err
				}
				return hub.ClusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Delete(context.TODO(), clusterSetName, metav1.DeleteOptions{})
			}).Should(Succeed())
		})

		AfterEach(func() {
			err := hub.KubeClient.CoreV1().Namespaces().Delete(context.TODO(), namespace, metav1.DeleteOptions{})
			if errors.IsNotFound(err) {
				return
			}
			Expect(err).NotTo(HaveOccurred())
		})

		It("should bound a ManagedClusterSetBinding", func() {
			clusterSetName := fmt.Sprintf("clusterset-%s", rand.String(6))
			managedClusterSetBinding := newManagedClusterSetBinding(namespace, clusterSetName, clusterSetName)
			_, err := hub.ClusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).
				Create(context.TODO(), managedClusterSetBinding, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())

			// make sure the managedclustersetbinding status is correct
			Eventually(func() error {
				binding, err := hub.ClusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Get(context.TODO(), clusterSetName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if !meta.IsStatusConditionFalse(binding.Status.Conditions, clusterv1beta2.ClusterSetBindingBoundType) {
					return fmt.Errorf("binding %s/%s condition should be false", namespace, clusterSetName)
				}
				return nil
			}).Should(Succeed())

			managedClusterSet := &clusterv1beta2.ManagedClusterSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterSetName,
				},
			}

			_, err = hub.ClusterClient.ClusterV1beta2().ManagedClusterSets().Create(context.TODO(), managedClusterSet, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())

			// make sure the managedclustersetbinding status is correct
			Eventually(func() error {
				binding, err := hub.ClusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Get(context.TODO(), clusterSetName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if !meta.IsStatusConditionTrue(binding.Status.Conditions, clusterv1beta2.ClusterSetBindingBoundType) {
					return fmt.Errorf("binding %s/%s condition should be true", namespace, clusterSetName)
				}
				return nil
			}).Should(Succeed())

			err = hub.ClusterClient.ClusterV1beta2().ManagedClusterSets().Delete(context.TODO(), clusterSetName, metav1.DeleteOptions{})
			Expect(err).ToNot(HaveOccurred())

			// make sure the managedclustersetbinding status is correct
			Eventually(func() error {
				binding, err := hub.ClusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Get(context.TODO(), clusterSetName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if !meta.IsStatusConditionFalse(binding.Status.Conditions, clusterv1beta2.ClusterSetBindingBoundType) {
					return fmt.Errorf("binding %s/%s condition should be false", namespace, clusterSetName)
				}
				return nil
			}).Should(Succeed())
		})
	})
})

func newManagedClusterSetBinding(namespace, name string, clusterSet string) *clusterv1beta2.ManagedClusterSetBinding {
	return &clusterv1beta2.ManagedClusterSetBinding{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: clusterv1beta2.ManagedClusterSetBindingSpec{
			ClusterSet: clusterSet,
		},
	}
}
