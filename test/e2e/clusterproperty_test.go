package e2e

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	aboutv1alpha1 "sigs.k8s.io/about-api/pkg/apis/v1alpha1"
	aboutclient "sigs.k8s.io/about-api/pkg/generated/clientset/versioned"

	ocmfeature "open-cluster-management.io/api/feature"

	"open-cluster-management.io/ocm/test/framework"
)

var _ = Describe("ClusterProperty API test", func() {
	var aboutClusterClient aboutclient.Interface
	var err error
	BeforeEach(func() {
		framework.EnableRegistrationFeature(hub, spoke, universalKlusterletName, string(ocmfeature.ClusterProperty))

		aboutClusterClient, err = aboutclient.NewForConfig(spoke.RestConfig)
		Expect(err).ToNot(HaveOccurred())

		DeferCleanup(func(ctx SpecContext) {
			framework.RemoveRegistrationFeature(hub, spoke, universalKlusterletName, string(ocmfeature.ClusterProperty))

			err = aboutClusterClient.AboutV1alpha1().ClusterProperties().DeleteCollection(
				ctx, metav1.DeleteOptions{}, metav1.ListOptions{})
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Context("create/update/delete clusterproperty", func() {
		It("managed cluster should have clusterproperty synced", func(ctx SpecContext) {
			prop1 := &aboutv1alpha1.ClusterProperty{
				ObjectMeta: metav1.ObjectMeta{
					Name: "prop1",
				},
				Spec: aboutv1alpha1.ClusterPropertySpec{
					Value: "value1",
				},
			}

			Eventually(func() error {
				_, err = aboutClusterClient.AboutV1alpha1().ClusterProperties().Create(
					ctx, prop1, metav1.CreateOptions{})
				return err
			}).Should(Succeed())

			By("create a cluster property")
			Eventually(func() error {
				managedCluster, err := hub.ClusterClient.ClusterV1().ManagedClusters().Get(
					ctx, universalClusterName, metav1.GetOptions{})
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
			}).Should(Succeed())

			By("update a cluster property")
			Eventually(func() error {
				p, err := aboutClusterClient.AboutV1alpha1().ClusterProperties().Get(
					ctx, "prop1", metav1.GetOptions{})
				if err != nil {
					return err
				}
				p.Spec.Value = "value2"
				_, err = aboutClusterClient.AboutV1alpha1().ClusterProperties().Update(
					ctx, p, metav1.UpdateOptions{})
				return err
			}).Should(Succeed())

			Eventually(func() error {
				managedCluster, err := hub.ClusterClient.ClusterV1().ManagedClusters().Get(
					ctx, universalClusterName, metav1.GetOptions{})
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
			}).Should(Succeed())

			By("delete a cluster property")
			err = aboutClusterClient.AboutV1alpha1().ClusterProperties().Delete(
				ctx, "prop1", metav1.DeleteOptions{})
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() error {
				managedCluster, err := hub.ClusterClient.ClusterV1().ManagedClusters().Get(
					ctx, universalClusterName, metav1.GetOptions{})
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
			}).Should(Succeed())
		})
	})

})
