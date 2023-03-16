package integration

import (
	"context"
	"fmt"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
)

var _ = ginkgo.Describe("Agent deploy", func() {
	suffix := rand.String(5)
	var cma *addonapiv1alpha1.ClusterManagementAddOn
	var placementNamespace string
	var clusterNames []string

	ginkgo.BeforeEach(func() {
		// Create clustermanagement addon
		cma = &addonapiv1alpha1.ClusterManagementAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("test-%s", suffix),
			},
			Spec: addonapiv1alpha1.ClusterManagementAddOnSpec{
				InstallStrategy: addonapiv1alpha1.InstallStrategy{
					Type: addonapiv1alpha1.AddonInstallStrategyManual,
				},
			},
		}
		_, err := hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().Create(context.Background(), cma, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		placementNamespace = fmt.Sprintf("ns-%s", suffix)
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: placementNamespace}}
		_, err = hubKubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		for i := 0; i < 4; i++ {
			managedClusterName := fmt.Sprintf("managedcluster-%s-%d", suffix, i)
			clusterNames = append(clusterNames, managedClusterName)
			managedCluster := &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: managedClusterName,
				},
				Spec: clusterv1.ManagedClusterSpec{
					HubAcceptsClient: true,
				},
			}
			_, err = hubClusterClient.ClusterV1().ManagedClusters().Create(context.Background(), managedCluster, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: managedClusterName}}
			_, err = hubKubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		}
	})

	ginkgo.AfterEach(func() {
		err := hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().Delete(context.Background(), cma.Name, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		err = hubKubeClient.CoreV1().Namespaces().Delete(context.Background(), placementNamespace, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		for _, managedClusterName := range clusterNames {
			err = hubKubeClient.CoreV1().Namespaces().Delete(context.Background(), managedClusterName, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			err = hubClusterClient.ClusterV1().ManagedClusters().Delete(context.Background(), managedClusterName, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		}
	})

	ginkgo.Context("Addon install strategy", func() {
		ginkgo.It("Should create/delete mca correctly by placement", func() {
			placement := &clusterv1beta1.Placement{ObjectMeta: metav1.ObjectMeta{Name: "test-placement", Namespace: placementNamespace}}
			_, err := hubClusterClient.ClusterV1beta1().Placements(placementNamespace).Create(context.Background(), placement, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			decision := &clusterv1beta1.PlacementDecision{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-placement",
					Namespace: placementNamespace,
					Labels:    map[string]string{clusterv1beta1.PlacementLabel: "test-placement"},
				},
			}
			decision, err = hubClusterClient.ClusterV1beta1().PlacementDecisions(placementNamespace).Create(context.Background(), decision, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			decision.Status.Decisions = []clusterv1beta1.ClusterDecision{
				{ClusterName: clusterNames[0]},
				{ClusterName: clusterNames[1]},
			}
			_, err = hubClusterClient.ClusterV1beta1().PlacementDecisions(placementNamespace).UpdateStatus(context.Background(), decision, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			clusterManagementAddon, err := hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().Get(context.Background(), cma.Name, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			clusterManagementAddon.Spec.InstallStrategy = addonapiv1alpha1.InstallStrategy{
				Type: addonapiv1alpha1.AddonInstallStrategyManualPlacements,
				Placements: []addonapiv1alpha1.PlacementStrategy{
					{
						PlacementRef: addonapiv1alpha1.PlacementRef{Name: "test-placement", Namespace: placementNamespace},
					},
				},
			}

			_, err = hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().Update(context.Background(), clusterManagementAddon, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			gomega.Eventually(func() error {
				_, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(clusterNames[0]).Get(context.Background(), cma.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}
				_, err = hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(clusterNames[1]).Get(context.Background(), cma.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Update the decision
			decision, err = hubClusterClient.ClusterV1beta1().PlacementDecisions(placementNamespace).Get(context.Background(), "test-placement", metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			decision.Status.Decisions = []clusterv1beta1.ClusterDecision{
				{ClusterName: clusterNames[1]},
				{ClusterName: clusterNames[2]},
			}
			_, err = hubClusterClient.ClusterV1beta1().PlacementDecisions(placementNamespace).UpdateStatus(context.Background(), decision, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			gomega.Eventually(func() error {
				_, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(clusterNames[1]).Get(context.Background(), cma.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}
				_, err = hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(clusterNames[2]).Get(context.Background(), cma.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}

				_, err = hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(clusterNames[0]).Get(context.Background(), cma.Name, metav1.GetOptions{})
				if !errors.IsNotFound(err) {
					return fmt.Errorf("addon in cluster %s should be removed", clusterNames[0])
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// delete an addon and ensure it is recreated.
			err = hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(clusterNames[1]).Delete(context.Background(), cma.Name, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Eventually(func() error {
				_, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(clusterNames[1]).Get(context.Background(), cma.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
		})
	})
})
