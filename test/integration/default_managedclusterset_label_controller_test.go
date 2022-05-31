package integration_test

import (
	"context"
	"fmt"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
)

const (
	defaultManagedClusterSetValue = "default"
)

var _ = ginkgo.Describe("DefaultManagedClusterSetLabel", func() {
	ginkgo.It("should ensure managed cluster has a cluster set label", func() {
		ginkgo.By("Create a ManagedCluster with no label")
		mcl1, err := newManagedCluster()
		gomega.Expect(err).NotTo(gomega.HaveOccurred(), "create managed cluster failed")

		ginkgo.By("Check whether DefaultManagedClusterSetLabel is set")
		gomega.Eventually(func() bool {
			cluster1, err := clusterClient.ClusterV1().ManagedClusters().Get(context.TODO(), mcl1.Name, metav1.GetOptions{})
			if err != nil {
				return false
			}
			if hasLabel(cluster1, clusterv1beta1.ClusterSetLabel, defaultManagedClusterSetValue) {
				return true
			}
			return false
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

	})

	ginkgo.It("should ensure managed cluster label is not \"\"", func() {
		ginkgo.By("Create a ManagedCluster with empty-valued label")
		mcl2, err := newManagedClusterWithLabel(clusterv1beta1.ClusterSetLabel, "")
		gomega.Expect(err).NotTo(gomega.HaveOccurred(), "create managed cluster failed")

		ginkgo.By("Check whether DefaultManagedClusterSetLabel value is empty")
		gomega.Eventually(func() bool {
			cluster2, err := clusterClient.ClusterV1().ManagedClusters().Get(context.TODO(), mcl2.Name, metav1.GetOptions{})
			if err != nil {
				return false
			}
			if hasLabel(cluster2, clusterv1beta1.ClusterSetLabel, defaultManagedClusterSetValue) {
				return true
			}
			return false
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

	})

	ginkgo.It("should ensure managed cluster label is default", func() {
		ginkgo.By("Create a ManagedCluster with default label")
		mcl2, err := newManagedClusterWithLabel(clusterv1beta1.ClusterSetLabel, defaultManagedClusterSetValue)
		gomega.Expect(err).NotTo(gomega.HaveOccurred(), "create managed cluster failed")

		ginkgo.By("Check whether DefaultManagedClusterSetLabel value is default")
		gomega.Eventually(func() bool {
			cluster2, err := clusterClient.ClusterV1().ManagedClusters().Get(context.TODO(), mcl2.Name, metav1.GetOptions{})
			if err != nil {
				return false
			}
			if hasLabel(cluster2, clusterv1beta1.ClusterSetLabel, defaultManagedClusterSetValue) {
				return true
			}
			return false
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

	})
})

func newManagedCluster() (*clusterv1.ManagedCluster, error) {
	managedClusterName := fmt.Sprintf("managedcluster-%s", rand.String(6))
	managedCluster := &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: managedClusterName,
		},
		Spec: clusterv1.ManagedClusterSpec{
			HubAcceptsClient: true,
		},
	}

	return clusterClient.ClusterV1().ManagedClusters().Create(context.Background(), managedCluster, metav1.CreateOptions{})
}

func newManagedClusterWithLabel(key, value string) (*clusterv1.ManagedCluster, error) {
	managedClusterName := fmt.Sprintf("managedcluster-%s", rand.String(6))
	managedCluster := &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: managedClusterName,
			Labels: map[string]string{
				key: value,
			},
		},
		Spec: clusterv1.ManagedClusterSpec{
			HubAcceptsClient: true,
		},
	}

	return clusterClient.ClusterV1().ManagedClusters().Create(context.Background(), managedCluster, metav1.CreateOptions{})
}

func hasLabel(mcl *clusterv1.ManagedCluster, key, value string) bool {
	v, ok := mcl.Labels[key]
	return ok && v == value
}
