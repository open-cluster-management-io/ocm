package integration_test

import (
	"context"
	"fmt"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"

	clusterv1 "open-cluster-management.io/api/cluster/v1"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	testinghelpers "open-cluster-management.io/registration/pkg/helpers/testing"
)

var _ = ginkgo.Describe("Cluster deleting", func() {
	var managedCluster *clusterv1.ManagedCluster

	ginkgo.BeforeEach(func() {
		managedClusterName := fmt.Sprintf("managedcluster-%s", rand.String(6))
		managedCluster = &clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: managedClusterName,
			},
			Spec: clusterv1.ManagedClusterSpec{
				HubAcceptsClient: true,
			},
		}
	})

	ginkgo.It("deleting cluster should wait until all manifestworks are deleted", func() {
		_, err := clusterClient.ClusterV1().ManagedClusters().Create(context.Background(), managedCluster, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		gomega.Eventually(func() bool {
			if _, err := kubeClient.CoreV1().Namespaces().Get(context.Background(), managedCluster.Name, metav1.GetOptions{}); err != nil {
				return false
			}
			return true
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		manifestWork := testinghelpers.NewManifestWork(managedCluster.Name, "work1", []string{}, nil)
		_, err = workClient.WorkV1().ManifestWorks(managedCluster.Name).Create(context.Background(), manifestWork, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		roleBindingName := fmt.Sprintf("open-cluster-management:managedcluster:%s:work", managedCluster.Name)
		gomega.Eventually(func() bool {
			if _, err := kubeClient.RbacV1().RoleBindings(managedCluster.Name).Get(context.Background(), roleBindingName, metav1.GetOptions{}); err != nil {
				return false
			}
			return true
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		err = clusterClient.ClusterV1().ManagedClusters().Delete(context.Background(), managedCluster.Name, metav1.DeleteOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		err = kubeClient.RbacV1().RoleBindings(managedCluster.Name).Delete(context.Background(), roleBindingName, metav1.DeleteOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		roleBinding, err := kubeClient.RbacV1().RoleBindings(managedCluster.Name).Get(context.Background(), roleBindingName, metav1.GetOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(len(roleBinding.Finalizers)).Should(gomega.Equal(1))
		gomega.Expect(roleBinding.Finalizers[0]).Should(gomega.Equal("cluster.open-cluster-management.io/manifest-work-cleanup"))
		gomega.Expect(roleBinding.DeletionTimestamp.IsZero()).Should(gomega.BeFalse())

		// Delete work
		err = workClient.WorkV1().ManifestWorks(managedCluster.Name).Delete(context.Background(), "work1", metav1.DeleteOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		gomega.Eventually(func() bool {
			_, err := workClient.WorkV1().ManifestWorks(managedCluster.Name).Get(context.Background(), "work1", metav1.GetOptions{})
			return errors.IsNotFound(err)
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		gomega.Eventually(func() bool {
			_, err := kubeClient.RbacV1().RoleBindings(managedCluster.Name).Get(context.Background(), roleBindingName, metav1.GetOptions{})
			if errors.IsNotFound(err) {
				return true
			}
			return true
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		gomega.Eventually(func() bool {
			if _, err := clusterClient.ClusterV1().ManagedClusters().Get(context.Background(), managedCluster.Name, metav1.GetOptions{}); !errors.IsNotFound(err) {
				return false
			}
			return true
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
	})
})
