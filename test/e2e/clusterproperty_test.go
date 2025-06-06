package e2e

import (
	"context"
	"fmt"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	aboutv1alpha1 "sigs.k8s.io/about-api/pkg/apis/v1alpha1"
	aboutclient "sigs.k8s.io/about-api/pkg/generated/clientset/versioned"

	ocmfeature "open-cluster-management.io/api/feature"
)

var _ = ginkgo.Describe("ClusterProperty API test", func() {
	var aboutClusterClient aboutclient.Interface
	var err error
	ginkgo.BeforeEach(func() {
		gomega.Eventually(func() error {
			return spoke.EnableRegistrationFeature(universalKlusterletName, string(ocmfeature.ClusterProperty))
		}).Should(gomega.Succeed())

		aboutClusterClient, err = aboutclient.NewForConfig(spoke.RestConfig)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ginkgo.DeferCleanup(func() {
			gomega.Eventually(func() error {
				return spoke.RemoveRegistrationFeature(universalKlusterletName, string(ocmfeature.ClusterProperty))
			}).Should(gomega.Succeed())

			err = aboutClusterClient.AboutV1alpha1().ClusterProperties().DeleteCollection(
				context.Background(), metav1.DeleteOptions{}, metav1.ListOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		})
	})

	ginkgo.Context("create/update/delete clusterproperty", func() {
		ginkgo.It("managed cluster should have clusterproperty synced", func() {
			prop1 := &aboutv1alpha1.ClusterProperty{
				ObjectMeta: metav1.ObjectMeta{
					Name: "prop1",
				},
				Spec: aboutv1alpha1.ClusterPropertySpec{
					Value: "value1",
				},
			}

			gomega.Eventually(func() error {
				_, err = aboutClusterClient.AboutV1alpha1().ClusterProperties().Create(
					context.Background(), prop1, metav1.CreateOptions{})
				return err
			}).Should(gomega.Succeed())

			ginkgo.By("create a cluster property")
			gomega.Eventually(func() error {
				managedCluster, err := hub.ClusterClient.ClusterV1().ManagedClusters().Get(
					context.Background(), universalClusterName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				for _, claim := range managedCluster.Status.ClusterClaims {
					if claim.Name == "prop1" && claim.Value == "value1" {
						return nil
					}
				}
				return fmt.Errorf(
					"managed cluster does not have prop1 synced, got %v", managedCluster.Status.ClusterClaims)
			}).Should(gomega.Succeed())

			ginkgo.By("update a cluster property")
			gomega.Eventually(func() error {
				p, err := aboutClusterClient.AboutV1alpha1().ClusterProperties().Get(
					context.Background(), "prop1", metav1.GetOptions{})
				if err != nil {
					return err
				}
				p.Spec.Value = "value2"
				_, err = aboutClusterClient.AboutV1alpha1().ClusterProperties().Update(
					context.Background(), p, metav1.UpdateOptions{})
				return err
			}).Should(gomega.Succeed())

			gomega.Eventually(func() error {
				managedCluster, err := hub.ClusterClient.ClusterV1().ManagedClusters().Get(
					context.Background(), universalClusterName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				for _, claim := range managedCluster.Status.ClusterClaims {
					if claim.Name == "prop1" && claim.Value == "value2" {
						return nil
					}
				}
				return fmt.Errorf(
					"managed cluster does not have prop1 synced, got %v", managedCluster.Status.ClusterClaims)
			}).Should(gomega.Succeed())

			ginkgo.By("delete a cluster property")
			err = aboutClusterClient.AboutV1alpha1().ClusterProperties().Delete(
				context.Background(), "prop1", metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			gomega.Eventually(func() error {
				managedCluster, err := hub.ClusterClient.ClusterV1().ManagedClusters().Get(
					context.Background(), universalClusterName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				for _, claim := range managedCluster.Status.ClusterClaims {
					if claim.Name == "prop1" && claim.Value == "value2" {
						return fmt.Errorf(
							"managed cluster should not have prop1 synced, got %v", managedCluster.Status.ClusterClaims)
					}
				}
				return nil
			}).Should(gomega.Succeed())
		})
	})

})
