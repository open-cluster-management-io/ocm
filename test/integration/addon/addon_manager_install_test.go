package integration

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	addonapiv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
)

var _ = Describe("Addon install Beta", func() {
	var suffix string
	var cma *addonapiv1beta1.ClusterManagementAddOn
	var placementNamespace string
	var clusterNames []string

	BeforeEach(func() {
		suffix = rand.String(5)
		clusterNames = nil

		// Create clustermanagement addon
		cma = newClusterManagementAddonBeta(fmt.Sprintf("test-%s", suffix))
		_, err := hubAddonClient.AddonV1beta1().ClusterManagementAddOns().Create(context.Background(), cma, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		assertClusterManagementAddOnAnnotationsBeta(cma.Name)

		placementNamespace = fmt.Sprintf("ns-%s", suffix)
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: placementNamespace}}
		_, err = hubKubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

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
			Expect(err).ToNot(HaveOccurred())

			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: managedClusterName}}
			_, err = hubKubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())
		}
	})

	AfterEach(func() {
		err := hubAddonClient.AddonV1beta1().ClusterManagementAddOns().Delete(context.Background(), cma.Name, metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())
		err = hubKubeClient.CoreV1().Namespaces().Delete(context.Background(), placementNamespace, metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())
		for _, managedClusterName := range clusterNames {
			err = hubKubeClient.CoreV1().Namespaces().Delete(context.Background(), managedClusterName, metav1.DeleteOptions{})
			Expect(err).ToNot(HaveOccurred())
			err = hubClusterClient.ClusterV1().ManagedClusters().Delete(context.Background(), managedClusterName, metav1.DeleteOptions{})
			Expect(err).ToNot(HaveOccurred())
		}
	})

	Context("Addon install strategy", func() {
		It("Should create/delete mca correctly by placement", func() {
			placement := &clusterv1beta1.Placement{ObjectMeta: metav1.ObjectMeta{Name: "test-placement", Namespace: placementNamespace}}
			_, err := hubClusterClient.ClusterV1beta1().Placements(placementNamespace).Create(context.Background(), placement, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())

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
			Expect(err).ToNot(HaveOccurred())

			decision.Status.Decisions = []clusterv1beta1.ClusterDecision{
				{ClusterName: clusterNames[0]},
				{ClusterName: clusterNames[1]},
			}
			_, err = hubClusterClient.ClusterV1beta1().PlacementDecisions(placementNamespace).UpdateStatus(context.Background(), decision, metav1.UpdateOptions{})
			Expect(err).ToNot(HaveOccurred())

			clusterManagementAddon, err := hubAddonClient.AddonV1beta1().ClusterManagementAddOns().Get(context.Background(), cma.Name, metav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())

			clusterManagementAddon.Spec.InstallStrategy = addonapiv1beta1.InstallStrategy{
				Type: addonapiv1beta1.AddonInstallStrategyPlacements,
				Placements: []addonapiv1beta1.PlacementStrategy{
					{
						PlacementRef: addonapiv1beta1.PlacementRef{Name: "test-placement", Namespace: placementNamespace},
						RolloutStrategy: clusterv1alpha1.RolloutStrategy{
							Type: clusterv1alpha1.All,
						},
					},
				},
			}

			_, err = hubAddonClient.AddonV1beta1().ClusterManagementAddOns().Update(context.Background(), clusterManagementAddon, metav1.UpdateOptions{})
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() error {
				_, err := hubAddonClient.AddonV1beta1().ManagedClusterAddOns(clusterNames[0]).Get(context.Background(), cma.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}
				_, err = hubAddonClient.AddonV1beta1().ManagedClusterAddOns(clusterNames[1]).Get(context.Background(), cma.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

			// Update the decision
			decision, err = hubClusterClient.ClusterV1beta1().PlacementDecisions(placementNamespace).Get(context.Background(), "test-placement", metav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())

			decision.Status.Decisions = []clusterv1beta1.ClusterDecision{
				{ClusterName: clusterNames[1]},
				{ClusterName: clusterNames[2]},
			}
			_, err = hubClusterClient.ClusterV1beta1().PlacementDecisions(placementNamespace).UpdateStatus(context.Background(), decision, metav1.UpdateOptions{})
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() error {
				_, err := hubAddonClient.AddonV1beta1().ManagedClusterAddOns(clusterNames[1]).Get(context.Background(), cma.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}
				_, err = hubAddonClient.AddonV1beta1().ManagedClusterAddOns(clusterNames[2]).Get(context.Background(), cma.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}

				_, err = hubAddonClient.AddonV1beta1().ManagedClusterAddOns(clusterNames[0]).Get(context.Background(), cma.Name, metav1.GetOptions{})
				if !errors.IsNotFound(err) {
					return fmt.Errorf("addon in cluster %s should be removed", clusterNames[0])
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

			// delete an addon and ensure it is recreated.
			err = hubAddonClient.AddonV1beta1().ManagedClusterAddOns(clusterNames[1]).Delete(context.Background(), cma.Name, metav1.DeleteOptions{})
			Expect(err).ToNot(HaveOccurred())
			Eventually(func() error {
				_, err := hubAddonClient.AddonV1beta1().ManagedClusterAddOns(clusterNames[1]).Get(context.Background(), cma.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
		})

		It("Should sync hosting cluster name annotation from managed cluster to addon", func() {
			// Add hosting-cluster-name annotation to one of the managed clusters
			cluster, err := hubClusterClient.ClusterV1().ManagedClusters().Get(context.Background(), clusterNames[0], metav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())
			cluster.Annotations = map[string]string{
				addonapiv1beta1.HostingClusterNameAnnotationKey: "hosting-cluster",
			}
			_, err = hubClusterClient.ClusterV1().ManagedClusters().Update(context.Background(), cluster, metav1.UpdateOptions{})
			Expect(err).ToNot(HaveOccurred())

			placement := &clusterv1beta1.Placement{ObjectMeta: metav1.ObjectMeta{Name: "test-placement-hosted", Namespace: placementNamespace}}
			_, err = hubClusterClient.ClusterV1beta1().Placements(placementNamespace).Create(context.Background(), placement, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())

			decision := &clusterv1beta1.PlacementDecision{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-placement-hosted",
					Namespace: placementNamespace,
					Labels: map[string]string{
						clusterv1beta1.PlacementLabel:          "test-placement-hosted",
						clusterv1beta1.DecisionGroupIndexLabel: "0",
					},
				},
			}
			decision, err = hubClusterClient.ClusterV1beta1().PlacementDecisions(placementNamespace).Create(context.Background(), decision, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())

			decision.Status.Decisions = []clusterv1beta1.ClusterDecision{
				{ClusterName: clusterNames[0]},
				{ClusterName: clusterNames[1]},
			}
			_, err = hubClusterClient.ClusterV1beta1().PlacementDecisions(placementNamespace).UpdateStatus(context.Background(), decision, metav1.UpdateOptions{})
			Expect(err).ToNot(HaveOccurred())

			clusterManagementAddon, err := hubAddonClient.AddonV1beta1().ClusterManagementAddOns().Get(context.Background(), cma.Name, metav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())

			clusterManagementAddon.Spec.InstallStrategy = addonapiv1beta1.InstallStrategy{
				Type: addonapiv1beta1.AddonInstallStrategyPlacements,
				Placements: []addonapiv1beta1.PlacementStrategy{
					{
						PlacementRef: addonapiv1beta1.PlacementRef{Name: "test-placement-hosted", Namespace: placementNamespace},
						RolloutStrategy: clusterv1alpha1.RolloutStrategy{
							Type: clusterv1alpha1.All,
						},
					},
				},
			}
			_, err = hubAddonClient.AddonV1beta1().ClusterManagementAddOns().Update(context.Background(), clusterManagementAddon, metav1.UpdateOptions{})
			Expect(err).ToNot(HaveOccurred())

			// Verify the addon on the hosted cluster has the hosting-cluster-name annotation
			Eventually(func() error {
				addon, err := hubAddonClient.AddonV1beta1().ManagedClusterAddOns(clusterNames[0]).Get(context.Background(), cma.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}
				hostingCluster, ok := addon.Annotations[addonapiv1beta1.HostingClusterNameAnnotationKey]
				if !ok {
					return fmt.Errorf("expected hosting cluster name annotation on addon in cluster %s", clusterNames[0])
				}
				if hostingCluster != "hosting-cluster" {
					return fmt.Errorf("expected hosting cluster name 'hosting-cluster', got '%s'", hostingCluster)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

			// Verify the addon on the non-hosted cluster does NOT have the hosting-cluster-name annotation
			Eventually(func() error {
				addon, err := hubAddonClient.AddonV1beta1().ManagedClusterAddOns(clusterNames[1]).Get(context.Background(), cma.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}
				if _, ok := addon.Annotations[addonapiv1beta1.HostingClusterNameAnnotationKey]; ok {
					return fmt.Errorf("expected no hosting cluster name annotation on addon in cluster %s", clusterNames[1])
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
		})
	})
})
