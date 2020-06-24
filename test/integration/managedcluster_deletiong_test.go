package integration_test

import (
	"context"
	"fmt"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"

	clusterv1 "github.com/open-cluster-management/api/cluster/v1"

	testinghelpers "github.com/open-cluster-management/registration/pkg/helpers/testing"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
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

		err = clusterClient.ClusterV1().ManagedClusters().Delete(context.Background(), managedCluster.Name, metav1.DeleteOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		roleName := fmt.Sprintf("%s:managed-cluster-work", managedCluster.Name)
		err = kubeClient.RbacV1().Roles(managedCluster.Name).Delete(context.Background(), roleName, metav1.DeleteOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		role, err := kubeClient.RbacV1().Roles(managedCluster.Name).Get(context.Background(), roleName, metav1.GetOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(len(role.Finalizers)).Should(gomega.Equal(1))
		gomega.Expect(role.Finalizers[0]).Should(gomega.Equal("cluster.open-cluster-management.io/manifest-work-cleanup"))
		gomega.Expect(role.DeletionTimestamp.IsZero()).Should(gomega.BeFalse())

		// Delete work
		err = workClient.WorkV1().ManifestWorks(managedCluster.Name).Delete(context.Background(), "work1", metav1.DeleteOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		gomega.Eventually(func() bool {
			_, err := workClient.WorkV1().ManifestWorks(managedCluster.Name).Get(context.Background(), "work1", metav1.GetOptions{})
			if errors.IsNotFound(err) {
				return true
			}
			return false
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		gomega.Eventually(func() bool {
			_, err := kubeClient.RbacV1().Roles(managedCluster.Name).Get(context.Background(), roleName, metav1.GetOptions{})
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
