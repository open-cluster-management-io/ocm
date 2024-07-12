package e2e

import (
	"context"
	"fmt"

	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
)

var _ = ginkgo.Describe("ManagedClusterSetBinding", func() {

	ginkgo.Context("ManagedClusterSetBinding", func() {
		var namespace string
		ginkgo.BeforeEach(func() {
			// create a namespace for testing
			namespace = fmt.Sprintf("ns-%s", rand.String(6))
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespace,
				},
			}
			_, err := hub.KubeClient.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// make sure the managedclustersetbinding can be created successfully
			gomega.Eventually(func() error {
				clusterSetName := fmt.Sprintf("clusterset-%s", rand.String(6))
				managedClusterSetBinding := newManagedClusterSetBinding(namespace, clusterSetName, clusterSetName)
				_, err := hub.ClusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).
					Create(context.TODO(), managedClusterSetBinding, metav1.CreateOptions{})
				if err != nil {
					return err
				}
				return hub.ClusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Delete(context.TODO(), clusterSetName, metav1.DeleteOptions{})
			}).Should(gomega.Succeed())
		})

		ginkgo.AfterEach(func() {
			err := hub.KubeClient.CoreV1().Namespaces().Delete(context.TODO(), namespace, metav1.DeleteOptions{})
			if errors.IsNotFound(err) {
				return
			}
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		})

		ginkgo.It("should bound a ManagedClusterSetBinding", func() {
			clusterSetName := fmt.Sprintf("clusterset-%s", rand.String(6))
			managedClusterSetBinding := newManagedClusterSetBinding(namespace, clusterSetName, clusterSetName)
			_, err := hub.ClusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).
				Create(context.TODO(), managedClusterSetBinding, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// make sure the managedclustersetbinding status is correct
			gomega.Eventually(func() error {
				binding, err := hub.ClusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Get(context.TODO(), clusterSetName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if !meta.IsStatusConditionFalse(binding.Status.Conditions, clusterv1beta2.ClusterSetBindingBoundType) {
					return fmt.Errorf("binding %s/%s condition should be false", namespace, clusterSetName)
				}
				return nil
			}).Should(gomega.Succeed())

			managedClusterSet := &clusterv1beta2.ManagedClusterSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterSetName,
				},
			}

			_, err = hub.ClusterClient.ClusterV1beta2().ManagedClusterSets().Create(context.TODO(), managedClusterSet, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// make sure the managedclustersetbinding status is correct
			gomega.Eventually(func() error {
				binding, err := hub.ClusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Get(context.TODO(), clusterSetName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if !meta.IsStatusConditionTrue(binding.Status.Conditions, clusterv1beta2.ClusterSetBindingBoundType) {
					return fmt.Errorf("binding %s/%s condition should be true", namespace, clusterSetName)
				}
				return nil
			}).Should(gomega.Succeed())

			err = hub.ClusterClient.ClusterV1beta2().ManagedClusterSets().Delete(context.TODO(), clusterSetName, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// make sure the managedclustersetbinding status is correct
			gomega.Eventually(func() error {
				binding, err := hub.ClusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Get(context.TODO(), clusterSetName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if !meta.IsStatusConditionFalse(binding.Status.Conditions, clusterv1beta2.ClusterSetBindingBoundType) {
					return fmt.Errorf("binding %s/%s condition should be false", namespace, clusterSetName)
				}
				return nil
			}).Should(gomega.Succeed())
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
