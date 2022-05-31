package e2e

import (
	"context"
	"fmt"
	"time"

	ginkgo "github.com/onsi/ginkgo"
	gomega "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	apiregistrationv1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
)

var _ = ginkgo.Describe("ManagedClusterSetBinding", func() {
	ginkgo.BeforeEach(func() {
		// make sure the api service v1.admission.cluster.open-cluster-management.io is available
		gomega.Eventually(func() bool {
			apiService, err := hubAPIServiceClient.APIServices().Get(context.TODO(), apiserviceName, metav1.GetOptions{})
			if err != nil {
				return false
			}
			if len(apiService.Status.Conditions) == 0 {
				return false
			}
			return apiService.Status.Conditions[0].Type == apiregistrationv1.Available &&
				apiService.Status.Conditions[0].Status == apiregistrationv1.ConditionTrue
		}, 60*time.Second, 1*time.Second).Should(gomega.BeTrue())
	})

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
			_, err := hubClient.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// make sure the managedclustersetbinding can be created successfully
			gomega.Eventually(func() error {
				clusterSetName := fmt.Sprintf("clusterset-%s", rand.String(6))
				managedClusterSetBinding := newManagedClusterSetBinding(namespace, clusterSetName, clusterSetName)
				_, err := clusterClient.ClusterV1beta1().ManagedClusterSetBindings(namespace).Create(context.TODO(), managedClusterSetBinding, metav1.CreateOptions{})
				if err != nil {
					return err
				}
				return clusterClient.ClusterV1beta1().ManagedClusterSetBindings(namespace).Delete(context.TODO(), clusterSetName, metav1.DeleteOptions{})
			}, 60*time.Second, 1*time.Second).Should(gomega.Succeed())
		})

		ginkgo.AfterEach(func() {
			err := hubClient.CoreV1().Namespaces().Delete(context.TODO(), namespace, metav1.DeleteOptions{})
			if errors.IsNotFound(err) {
				return
			}
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		})

		ginkgo.It("should bound a ManagedClusterSetBinding", func() {
			clusterSetName := fmt.Sprintf("clusterset-%s", rand.String(6))
			managedClusterSetBinding := newManagedClusterSetBinding(namespace, clusterSetName, clusterSetName)
			_, err := clusterClient.ClusterV1beta1().ManagedClusterSetBindings(namespace).Create(context.TODO(), managedClusterSetBinding, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// make sure the managedclustersetbinding status is correct
			gomega.Eventually(func() error {
				binding, err := clusterClient.ClusterV1beta1().ManagedClusterSetBindings(namespace).Get(context.TODO(), clusterSetName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if !meta.IsStatusConditionFalse(binding.Status.Conditions, clusterv1beta1.ClusterSetBindingBoundType) {
					return fmt.Errorf("binding %s/%s condition should be false", namespace, clusterSetName)
				}
				return nil
			}, 60*time.Second, 1*time.Second).Should(gomega.Succeed())

			managedClusterSet := &clusterv1beta1.ManagedClusterSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterSetName,
				},
			}

			_, err = clusterClient.ClusterV1beta1().ManagedClusterSets().Create(context.TODO(), managedClusterSet, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// make sure the managedclustersetbinding status is correct
			gomega.Eventually(func() error {
				binding, err := clusterClient.ClusterV1beta1().ManagedClusterSetBindings(namespace).Get(context.TODO(), clusterSetName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if !meta.IsStatusConditionTrue(binding.Status.Conditions, clusterv1beta1.ClusterSetBindingBoundType) {
					return fmt.Errorf("binding %s/%s condition should be true", namespace, clusterSetName)
				}
				return nil
			}, 60*time.Second, 1*time.Second).Should(gomega.Succeed())

			err = clusterClient.ClusterV1beta1().ManagedClusterSets().Delete(context.TODO(), clusterSetName, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// make sure the managedclustersetbinding status is correct
			gomega.Eventually(func() error {
				binding, err := clusterClient.ClusterV1beta1().ManagedClusterSetBindings(namespace).Get(context.TODO(), clusterSetName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if !meta.IsStatusConditionFalse(binding.Status.Conditions, clusterv1beta1.ClusterSetBindingBoundType) {
					return fmt.Errorf("binding %s/%s condition should be false", namespace, clusterSetName)
				}
				return nil
			}, 60*time.Second, 1*time.Second).Should(gomega.Succeed())
		})
	})
})
