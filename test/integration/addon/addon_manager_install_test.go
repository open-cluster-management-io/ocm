package integration

import (
	"context"
	"fmt"

	ginkgo "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
)

var _ = ginkgo.Describe("Addon install", func() {
	var cma *addonapiv1alpha1.ClusterManagementAddOn
	var placementNamespace string
	var clusterNames []string

	ginkgo.BeforeEach(func() {
		clusterNames = []string{}
		suffix := rand.String(5)

		// Create clustermanagement addon
		cma = newClusterManagementAddon(fmt.Sprintf("test-%s", suffix))
		_, err := hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().Create(context.Background(), cma, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		assertClusterManagementAddOnAnnotations(cma.Name)

		placementNamespace = fmt.Sprintf("ns-%s-%s", suffix, rand.String(5))

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
					Labels: map[string]string{
						clusterv1beta1.PlacementLabel:          "test-placement",
						clusterv1beta1.DecisionGroupIndexLabel: "0",
					},
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
				Type: addonapiv1alpha1.AddonInstallStrategyPlacements,
				Placements: []addonapiv1alpha1.PlacementStrategy{
					{
						PlacementRef: addonapiv1alpha1.PlacementRef{Name: "test-placement", Namespace: placementNamespace},
						RolloutStrategy: clusterv1alpha1.RolloutStrategy{
							Type: clusterv1alpha1.All,
						},
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

		ginkgo.It("Should not recreate addon during sequential PlacementDecision updates", func() {
			// Setup: Create placement and two PlacementDecisions (simulating max 100 clusters per decision)
			placement := &clusterv1beta1.Placement{ObjectMeta: metav1.ObjectMeta{Name: "test-placement", Namespace: placementNamespace}}
			_, err := hubClusterClient.ClusterV1beta1().Placements(placementNamespace).Create(context.Background(), placement, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			decision1 := &clusterv1beta1.PlacementDecision{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-placement-1",
					Namespace: placementNamespace,
					Labels: map[string]string{
						clusterv1beta1.PlacementLabel:          "test-placement",
						clusterv1beta1.DecisionGroupIndexLabel: "0",
					},
				},
			}
			decision1, err = hubClusterClient.ClusterV1beta1().PlacementDecisions(placementNamespace).Create(context.Background(), decision1, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			decision2 := &clusterv1beta1.PlacementDecision{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-placement-2",
					Namespace: placementNamespace,
					Labels: map[string]string{
						clusterv1beta1.PlacementLabel:          "test-placement",
						clusterv1beta1.DecisionGroupIndexLabel: "1",
					},
				},
			}
			decision2, err = hubClusterClient.ClusterV1beta1().PlacementDecisions(placementNamespace).Create(context.Background(), decision2, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Initial state: decision1 has cluster1, decision2 has cluster2 and cluster3
			decision1.Status.Decisions = []clusterv1beta1.ClusterDecision{
				{ClusterName: clusterNames[1]},
			}
			_, err = hubClusterClient.ClusterV1beta1().PlacementDecisions(placementNamespace).UpdateStatus(context.Background(), decision1, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			decision2.Status.Decisions = []clusterv1beta1.ClusterDecision{
				{ClusterName: clusterNames[2]},
				{ClusterName: clusterNames[3]},
			}
			_, err = hubClusterClient.ClusterV1beta1().PlacementDecisions(placementNamespace).UpdateStatus(context.Background(), decision2, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			clusterManagementAddon, err := hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().Get(context.Background(), cma.Name, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			clusterManagementAddon.Spec.InstallStrategy = addonapiv1alpha1.InstallStrategy{
				Type: addonapiv1alpha1.AddonInstallStrategyPlacements,
				Placements: []addonapiv1alpha1.PlacementStrategy{
					{
						PlacementRef: addonapiv1alpha1.PlacementRef{Name: "test-placement", Namespace: placementNamespace},
						RolloutStrategy: clusterv1alpha1.RolloutStrategy{
							Type: clusterv1alpha1.All,
						},
					},
				},
			}

			_, err = hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().Update(context.Background(), clusterManagementAddon, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Wait for initial addons to be created on cluster1, cluster2, cluster3
			gomega.Eventually(func() error {
				_, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(clusterNames[1]).Get(context.Background(), cma.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}
				_, err = hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(clusterNames[2]).Get(context.Background(), cma.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}
				_, err = hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(clusterNames[3]).Get(context.Background(), cma.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Capture cluster1's addon UID before the update - this is what we'll verify doesn't change
			addon1, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(clusterNames[1]).Get(context.Background(), cma.Name, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			originalUID := string(addon1.UID)

			// Sequential update: Move cluster1 from decision1 to decision2
			// Step 1: Update decision1 to remove cluster1 and add cluster0
			// Step 2: Update decision2 to include cluster1 (along with cluster2, cluster3)
			decision1, err = hubClusterClient.ClusterV1beta1().PlacementDecisions(placementNamespace).Get(context.Background(), "test-placement-1", metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			decision1.Status.Decisions = []clusterv1beta1.ClusterDecision{
				{ClusterName: clusterNames[0]},
			}
			_, err = hubClusterClient.ClusterV1beta1().PlacementDecisions(placementNamespace).UpdateStatus(context.Background(), decision1, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			decision2, err = hubClusterClient.ClusterV1beta1().PlacementDecisions(placementNamespace).Get(context.Background(), "test-placement-2", metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			decision2.Status.Decisions = []clusterv1beta1.ClusterDecision{
				{ClusterName: clusterNames[1]},
				{ClusterName: clusterNames[2]},
				{ClusterName: clusterNames[3]},
			}
			_, err = hubClusterClient.ClusterV1beta1().PlacementDecisions(placementNamespace).UpdateStatus(context.Background(), decision2, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Verify cluster0's addon is created (from decision1)
			gomega.Eventually(func() error {
				_, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(clusterNames[0]).Get(context.Background(), cma.Name, metav1.GetOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Critical verification: cluster1's addon should NOT be recreated
			// The UID should remain unchanged, proving the addon was not deleted and recreated
			gomega.Consistently(func() error {
				addon, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(clusterNames[1]).Get(context.Background(), cma.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}
				if string(addon.UID) != originalUID {
					return fmt.Errorf("addon UID changed for cluster %s, expected %s, got %s", clusterNames[1], originalUID, addon.UID)
				}
				return nil
			}, "5s", "500ms").ShouldNot(gomega.HaveOccurred())
		})
	})
})
