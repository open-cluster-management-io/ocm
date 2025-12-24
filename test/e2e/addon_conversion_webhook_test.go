package e2e

import (
	"context"
	"fmt"
	"reflect"

	ginkgo "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1beta1 "open-cluster-management.io/api/addon/v1beta1"
)

var _ = ginkgo.Describe("Create v1alpha1 ManagedClusterAddOn", ginkgo.Label("addon-conversion"), func() {
	ginkgo.It("Create a v1alpha1 ManagedClusterAddOn and get/update/delete with v1alpha1 client", func() {
		clusterName := universalClusterName
		suffix := rand.String(6)
		addonName := fmt.Sprintf("addon-%s", suffix)

		ginkgo.By("Create a v1alpha1 ManagedClusterAddOn")
		addon := &addonv1alpha1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: clusterName,
				Name:      addonName,
			},
			Spec: addonv1alpha1.ManagedClusterAddOnSpec{
				InstallNamespace: "test-install-ns",
			},
		}

		_, err := hub.AddonClient.AddonV1alpha1().ManagedClusterAddOns(clusterName).Create(
			context.Background(), addon, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Get v1alpha1 ManagedClusterAddOn using v1alpha1 client")
		gomega.Eventually(func() bool {
			addon, err = hub.GetManagedClusterAddOnV1Alpha1(clusterName, addonName)
			if err != nil {
				return false
			}
			return true
		}).Should(gomega.BeTrue())

		ginkgo.By("Update v1alpha1 ManagedClusterAddOn status using v1alpha1 client")
		gomega.Eventually(func() error {
			addon, err = hub.GetManagedClusterAddOnV1Alpha1(clusterName, addonName)
			if err != nil {
				return err
			}
			// Only update if not already set
			if len(addon.Status.ConfigReferences) == 0 {
				addon.Status.ConfigReferences = []addonv1alpha1.ConfigReference{
					{
						ConfigGroupResource: addonv1alpha1.ConfigGroupResource{
							Group:    "addon.open-cluster-management.io",
							Resource: "addondeploymentconfigs",
						},
						ConfigReferent: addonv1alpha1.ConfigReferent{
							Namespace: "test-ns",
							Name:      "test-config",
						},
						DesiredConfig: &addonv1alpha1.ConfigSpecHash{
							ConfigReferent: addonv1alpha1.ConfigReferent{
								Namespace: "test-ns",
								Name:      "test-config",
							},
						},
					},
				}
			}
			_, err = hub.AddonClient.AddonV1alpha1().ManagedClusterAddOns(clusterName).UpdateStatus(
				context.Background(), addon, metav1.UpdateOptions{})
			return err
		}).Should(gomega.Succeed())

		ginkgo.By("Delete v1alpha1 ManagedClusterAddOn using v1alpha1 client")
		gomega.Eventually(func() bool {
			err = hub.AddonClient.AddonV1alpha1().ManagedClusterAddOns(clusterName).Delete(
				context.Background(), addonName, metav1.DeleteOptions{})
			if err != nil {
				return false
			}
			return true
		}).Should(gomega.BeTrue())
	})

	ginkgo.It("Create a v1alpha1 ManagedClusterAddOn and get/update/delete with v1beta1 client", func() {
		clusterName := universalClusterName
		suffix := rand.String(6)
		addonName := fmt.Sprintf("addon-%s", suffix)

		ginkgo.By("Create a v1alpha1 ManagedClusterAddOn")
		addon := &addonv1alpha1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: clusterName,
				Name:      addonName,
			},
			Spec: addonv1alpha1.ManagedClusterAddOnSpec{
				InstallNamespace: "test-install-ns",
			},
		}

		_, err := hub.AddonClient.AddonV1alpha1().ManagedClusterAddOns(clusterName).Create(
			context.Background(), addon, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Update v1alpha1 ManagedClusterAddOn status using v1alpha1 client")
		gomega.Eventually(func() error {
			addon, err = hub.GetManagedClusterAddOnV1Alpha1(clusterName, addonName)
			if err != nil {
				return err
			}
			// Only update if not already set
			if len(addon.Status.ConfigReferences) == 0 {
				addon.Status.ConfigReferences = []addonv1alpha1.ConfigReference{
					{
						ConfigGroupResource: addonv1alpha1.ConfigGroupResource{
							Group:    "addon.open-cluster-management.io",
							Resource: "addondeploymentconfigs",
						},
						ConfigReferent: addonv1alpha1.ConfigReferent{
							Namespace: "test-ns",
							Name:      "test-config",
						},
						DesiredConfig: &addonv1alpha1.ConfigSpecHash{
							ConfigReferent: addonv1alpha1.ConfigReferent{
								Namespace: "test-ns",
								Name:      "test-config",
							},
						},
					},
				}
			}
			_, err = hub.AddonClient.AddonV1alpha1().ManagedClusterAddOns(clusterName).UpdateStatus(
				context.Background(), addon, metav1.UpdateOptions{})
			return err
		}).Should(gomega.Succeed())

		ginkgo.By("Get v1alpha1 ManagedClusterAddOn using v1beta1 client and verify conversion")
		gomega.Eventually(func() bool {
			v1beta1Addon, err := hub.GetManagedClusterAddOnV1Beta1(clusterName, addonName)
			if err != nil {
				return false
			}
			// Verify InstallNamespace annotation is preserved
			if v1beta1Addon.Annotations["addon.open-cluster-management.io/v1alpha1-install-namespace"] != "test-install-ns" {
				return false
			}
			// Verify status.ConfigReferences conversion
			if len(v1beta1Addon.Status.ConfigReferences) != 1 {
				return false
			}
			if v1beta1Addon.Status.ConfigReferences[0].DesiredConfig == nil {
				return false
			}
			if v1beta1Addon.Status.ConfigReferences[0].DesiredConfig.Name != "test-config" {
				return false
			}
			return true
		}).Should(gomega.BeTrue())

		ginkgo.By("Update v1alpha1 ManagedClusterAddOn status using v1beta1 client")
		gomega.Eventually(func() error {
			v1beta1Addon, err := hub.GetManagedClusterAddOnV1Beta1(clusterName, addonName)
			if err != nil {
				return err
			}
			// Only append if not already present (should have 2 configs after append)
			if len(v1beta1Addon.Status.ConfigReferences) < 2 {
				v1beta1Addon.Status.ConfigReferences = append(v1beta1Addon.Status.ConfigReferences,
					addonv1beta1.ConfigReference{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{
							Group:    "addon.open-cluster-management.io",
							Resource: "addondeploymentconfigs",
						},
						DesiredConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{
								Namespace: "beta-ns",
								Name:      "beta-config",
							},
						},
					})
			}
			_, err = hub.AddonClient.AddonV1beta1().ManagedClusterAddOns(clusterName).UpdateStatus(
				context.Background(), v1beta1Addon, metav1.UpdateOptions{})
			return err
		}).Should(gomega.Succeed())

		ginkgo.By("Verify status update via v1alpha1 client")
		gomega.Eventually(func() bool {
			addon, err = hub.GetManagedClusterAddOnV1Alpha1(clusterName, addonName)
			if err != nil {
				return false
			}
			return len(addon.Status.ConfigReferences) == 2
		}).Should(gomega.BeTrue())

		ginkgo.By("Delete v1alpha1 ManagedClusterAddOn using v1beta1 client")
		gomega.Eventually(func() bool {
			err = hub.AddonClient.AddonV1beta1().ManagedClusterAddOns(clusterName).Delete(
				context.Background(), addonName, metav1.DeleteOptions{})
			if err != nil {
				return false
			}
			return true
		}).Should(gomega.BeTrue())
	})
})

var _ = ginkgo.Describe("Create v1beta1 ManagedClusterAddOn", ginkgo.Label("addon-conversion"), func() {
	ginkgo.It("Create a v1beta1 ManagedClusterAddOn and get/update/delete with v1beta1 client", func() {
		clusterName := universalClusterName
		suffix := rand.String(6)
		addonName := fmt.Sprintf("addon-%s", suffix)

		ginkgo.By("Create a v1beta1 ManagedClusterAddOn")
		addon := &addonv1beta1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: clusterName,
				Name:      addonName,
			},
			Spec: addonv1beta1.ManagedClusterAddOnSpec{},
		}

		_, err := hub.AddonClient.AddonV1beta1().ManagedClusterAddOns(clusterName).Create(
			context.Background(), addon, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Get v1beta1 ManagedClusterAddOn using v1beta1 client")
		gomega.Eventually(func() bool {
			addon, err = hub.GetManagedClusterAddOnV1Beta1(clusterName, addonName)
			if err != nil {
				return false
			}
			return true
		}).Should(gomega.BeTrue())

		ginkgo.By("Update v1beta1 ManagedClusterAddOn status using v1beta1 client")
		gomega.Eventually(func() bool {
			addon, err = hub.GetManagedClusterAddOnV1Beta1(clusterName, addonName)
			if err != nil {
				return false
			}
			addon.Status.ConfigReferences = []addonv1beta1.ConfigReference{
				{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					DesiredConfig: &addonv1beta1.ConfigSpecHash{
						ConfigReferent: addonv1beta1.ConfigReferent{
							Namespace: "test-ns",
							Name:      "test-config",
						},
					},
				},
			}
			_, err = hub.AddonClient.AddonV1beta1().ManagedClusterAddOns(clusterName).UpdateStatus(
				context.Background(), addon, metav1.UpdateOptions{})
			if err != nil {
				return false
			}
			return true
		}).Should(gomega.BeTrue())

		ginkgo.By("Delete v1beta1 ManagedClusterAddOn using v1beta1 client")
		gomega.Eventually(func() bool {
			err = hub.AddonClient.AddonV1beta1().ManagedClusterAddOns(clusterName).Delete(
				context.Background(), addonName, metav1.DeleteOptions{})
			if err != nil {
				return false
			}
			return true
		}).Should(gomega.BeTrue())
	})

	ginkgo.It("Create a v1beta1 ManagedClusterAddOn and get/update/delete with v1alpha1 client", func() {
		clusterName := universalClusterName
		suffix := rand.String(6)
		addonName := fmt.Sprintf("addon-%s", suffix)

		ginkgo.By("Create a v1beta1 ManagedClusterAddOn")
		addon := &addonv1beta1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: clusterName,
				Name:      addonName,
			},
			Spec: addonv1beta1.ManagedClusterAddOnSpec{},
		}

		_, err := hub.AddonClient.AddonV1beta1().ManagedClusterAddOns(clusterName).Create(
			context.Background(), addon, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Update v1beta1 ManagedClusterAddOn status using v1beta1 client")
		gomega.Eventually(func() bool {
			addon, err = hub.GetManagedClusterAddOnV1Beta1(clusterName, addonName)
			if err != nil {
				return false
			}
			addon.Status.ConfigReferences = []addonv1beta1.ConfigReference{
				{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					DesiredConfig: &addonv1beta1.ConfigSpecHash{
						ConfigReferent: addonv1beta1.ConfigReferent{
							Namespace: "test-ns",
							Name:      "test-config",
						},
					},
				},
			}
			_, err = hub.AddonClient.AddonV1beta1().ManagedClusterAddOns(clusterName).UpdateStatus(
				context.Background(), addon, metav1.UpdateOptions{})
			if err != nil {
				return false
			}
			return true
		}).Should(gomega.BeTrue())

		ginkgo.By("Get v1beta1 ManagedClusterAddOn using v1alpha1 client and verify conversion")
		gomega.Eventually(func() bool {
			v1alpha1Addon, err := hub.GetManagedClusterAddOnV1Alpha1(clusterName, addonName)
			if err != nil {
				return false
			}
			// Verify status.ConfigReferences conversion
			if len(v1alpha1Addon.Status.ConfigReferences) != 1 {
				return false
			}
			if !reflect.DeepEqual(v1alpha1Addon.Status.ConfigReferences[0].ConfigReferent.Name, "test-config") {
				return false
			}
			return true
		}).Should(gomega.BeTrue())

		ginkgo.By("Update v1beta1 ManagedClusterAddOn status using v1alpha1 client")
		gomega.Eventually(func() bool {
			v1alpha1Addon, err := hub.GetManagedClusterAddOnV1Alpha1(clusterName, addonName)
			if err != nil {
				return false
			}
			v1alpha1Addon.Status.ConfigReferences = append(v1alpha1Addon.Status.ConfigReferences,
				addonv1alpha1.ConfigReference{
					ConfigGroupResource: addonv1alpha1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					ConfigReferent: addonv1alpha1.ConfigReferent{
						Namespace: "alpha-ns",
						Name:      "alpha-config",
					},
				})
			_, err = hub.AddonClient.AddonV1alpha1().ManagedClusterAddOns(clusterName).UpdateStatus(
				context.Background(), v1alpha1Addon, metav1.UpdateOptions{})
			if err != nil {
				return false
			}
			return true
		}).Should(gomega.BeTrue())

		ginkgo.By("Verify status update via v1beta1 client")
		gomega.Eventually(func() bool {
			addon, err = hub.GetManagedClusterAddOnV1Beta1(clusterName, addonName)
			if err != nil {
				return false
			}
			return len(addon.Status.ConfigReferences) == 2
		}).Should(gomega.BeTrue())

		ginkgo.By("Delete v1beta1 ManagedClusterAddOn using v1alpha1 client")
		gomega.Eventually(func() bool {
			err = hub.AddonClient.AddonV1alpha1().ManagedClusterAddOns(clusterName).Delete(
				context.Background(), addonName, metav1.DeleteOptions{})
			if err != nil {
				return false
			}
			return true
		}).Should(gomega.BeTrue())
	})
})

var _ = ginkgo.Describe("Create v1alpha1 ClusterManagementAddOn", ginkgo.Label("addon-conversion"), func() {
	ginkgo.It("Create a v1alpha1 ClusterManagementAddOn and get/update/delete with v1alpha1 client", func() {
		suffix := rand.String(6)
		addonName := fmt.Sprintf("cma-%s", suffix)

		ginkgo.By("Create a v1alpha1 ClusterManagementAddOn")
		addon := &addonv1alpha1.ClusterManagementAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name: addonName,
			},
			Spec: addonv1alpha1.ClusterManagementAddOnSpec{
				AddOnMeta: addonv1alpha1.AddOnMeta{
					DisplayName: "Test Addon",
					Description: "Test addon for conversion",
				},
				InstallStrategy: addonv1alpha1.InstallStrategy{
					Type: addonv1alpha1.AddonInstallStrategyManual,
				},
				SupportedConfigs: []addonv1alpha1.ConfigMeta{
					{
						ConfigGroupResource: addonv1alpha1.ConfigGroupResource{
							Group:    "addon.open-cluster-management.io",
							Resource: "addondeploymentconfigs",
						},
						DefaultConfig: &addonv1alpha1.ConfigReferent{Name: "test-config"},
					},
				},
			},
		}

		_, err := hub.CreateClusterManagementAddOnV1Alpha1(addonName, addon)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Get v1alpha1 ClusterManagementAddOn using v1alpha1 client")
		gomega.Eventually(func() bool {
			addon, err = hub.GetClusterManagementAddOnV1Alpha1(addonName)
			if err != nil {
				return false
			}
			return true
		}).Should(gomega.BeTrue())

		ginkgo.By("Update v1alpha1 ClusterManagementAddOn status using v1alpha1 client")
		gomega.Eventually(func() bool {
			addon, err = hub.GetClusterManagementAddOnV1Alpha1(addonName)
			if err != nil {
				return false
			}
			addon.Status.DefaultConfigReferences = []addonv1alpha1.DefaultConfigReference{
				{
					ConfigGroupResource: addonv1alpha1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					DesiredConfig: &addonv1alpha1.ConfigSpecHash{
						ConfigReferent: addonv1alpha1.ConfigReferent{
							Name: "status-config",
						},
					},
				},
			}
			_, err = hub.AddonClient.AddonV1alpha1().ClusterManagementAddOns().UpdateStatus(
				context.Background(), addon, metav1.UpdateOptions{})
			if err != nil {
				return false
			}
			return true
		}).Should(gomega.BeTrue())

		ginkgo.By("Delete v1alpha1 ClusterManagementAddOn using v1alpha1 client")
		gomega.Eventually(func() bool {
			err = hub.DeleteClusterManagementAddOnV1Alpha1(addonName)
			if err != nil {
				return false
			}
			return true
		}).Should(gomega.BeTrue())
	})

	ginkgo.It("Create a v1alpha1 ClusterManagementAddOn and get/update/delete with v1beta1 client", func() {
		suffix := rand.String(6)
		addonName := fmt.Sprintf("cma-%s", suffix)

		ginkgo.By("Create a v1alpha1 ClusterManagementAddOn")
		addon := &addonv1alpha1.ClusterManagementAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name: addonName,
			},
			Spec: addonv1alpha1.ClusterManagementAddOnSpec{
				AddOnMeta: addonv1alpha1.AddOnMeta{
					DisplayName: "Test Addon",
					Description: "Test addon for conversion",
				},
				InstallStrategy: addonv1alpha1.InstallStrategy{
					Type: addonv1alpha1.AddonInstallStrategyManual,
				},
				SupportedConfigs: []addonv1alpha1.ConfigMeta{
					{
						ConfigGroupResource: addonv1alpha1.ConfigGroupResource{
							Group:    "addon.open-cluster-management.io",
							Resource: "addondeploymentconfigs",
						},
						DefaultConfig: &addonv1alpha1.ConfigReferent{Name: "test-config"},
					},
				},
			},
		}

		_, err := hub.CreateClusterManagementAddOnV1Alpha1(addonName, addon)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Update v1alpha1 ClusterManagementAddOn status using v1alpha1 client")
		gomega.Eventually(func() bool {
			addon, err = hub.GetClusterManagementAddOnV1Alpha1(addonName)
			if err != nil {
				return false
			}
			addon.Status.DefaultConfigReferences = []addonv1alpha1.DefaultConfigReference{
				{
					ConfigGroupResource: addonv1alpha1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					DesiredConfig: &addonv1alpha1.ConfigSpecHash{
						ConfigReferent: addonv1alpha1.ConfigReferent{
							Name: "status-config",
						},
					},
				},
			}
			_, err = hub.AddonClient.AddonV1alpha1().ClusterManagementAddOns().UpdateStatus(
				context.Background(), addon, metav1.UpdateOptions{})
			if err != nil {
				return false
			}
			return true
		}).Should(gomega.BeTrue())

		ginkgo.By("Get v1alpha1 ClusterManagementAddOn using v1beta1 client and verify conversion")
		gomega.Eventually(func() bool {
			v1beta1Addon, err := hub.GetClusterManagementAddOnV1Beta1(addonName)
			if err != nil {
				return false
			}
			// Verify spec.supportedConfigs → spec.defaultConfigs conversion
			if len(v1beta1Addon.Spec.DefaultConfigs) != 1 {
				return false
			}
			if v1beta1Addon.Spec.DefaultConfigs[0].Name != "test-config" {
				return false
			}
			// Verify status.DefaultConfigReferences conversion
			if len(v1beta1Addon.Status.DefaultConfigReferences) != 1 {
				return false
			}
			if v1beta1Addon.Status.DefaultConfigReferences[0].DesiredConfig.Name != "status-config" {
				return false
			}
			return true
		}).Should(gomega.BeTrue())

		ginkgo.By("Update v1alpha1 ClusterManagementAddOn using v1beta1 client")
		gomega.Eventually(func() error {
			v1beta1Addon, err := hub.GetClusterManagementAddOnV1Beta1(addonName)
			if err != nil {
				return err
			}
			// Only append if not already present
			if len(v1beta1Addon.Spec.DefaultConfigs) < 2 {
				v1beta1Addon.Spec.DefaultConfigs = append(v1beta1Addon.Spec.DefaultConfigs,
					addonv1beta1.AddOnConfig{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{
							Group:    "addon.open-cluster-management.io",
							Resource: "addontemplates", // Different resource to avoid duplicate key
						},
						ConfigReferent: addonv1beta1.ConfigReferent{Name: "beta-config"},
					})
			}
			_, err = hub.UpdateClusterManagementAddOnV1Beta1(v1beta1Addon)
			return err
		}).Should(gomega.Succeed())

		ginkgo.By("Verify spec update via v1alpha1 client")
		gomega.Eventually(func() bool {
			addon, err = hub.GetClusterManagementAddOnV1Alpha1(addonName)
			if err != nil {
				return false
			}
			return len(addon.Spec.SupportedConfigs) == 2
		}).Should(gomega.BeTrue())

		ginkgo.By("Delete v1alpha1 ClusterManagementAddOn using v1beta1 client")
		gomega.Eventually(func() bool {
			err = hub.DeleteClusterManagementAddOnV1Beta1(addonName)
			if err != nil {
				return false
			}
			return true
		}).Should(gomega.BeTrue())
	})
})

var _ = ginkgo.Describe("Create v1beta1 ClusterManagementAddOn", ginkgo.Label("addon-conversion"), func() {
	ginkgo.It("Create a v1beta1 ClusterManagementAddOn and get/update/delete with v1beta1 client", func() {
		suffix := rand.String(6)
		addonName := fmt.Sprintf("cma-%s", suffix)

		ginkgo.By("Create a v1beta1 ClusterManagementAddOn")
		addon := &addonv1beta1.ClusterManagementAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name: addonName,
			},
			Spec: addonv1beta1.ClusterManagementAddOnSpec{
				AddOnMeta: addonv1beta1.AddOnMeta{
					DisplayName: "Test Beta Addon",
					Description: "Test addon for v1beta1",
				},
				InstallStrategy: addonv1beta1.InstallStrategy{
					Type: addonv1beta1.AddonInstallStrategyManual,
				},
				DefaultConfigs: []addonv1beta1.AddOnConfig{
					{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{
							Group:    "addon.open-cluster-management.io",
							Resource: "addondeploymentconfigs",
						},
						ConfigReferent: addonv1beta1.ConfigReferent{Name: "beta-config"},
					},
				},
			},
		}

		_, err := hub.CreateClusterManagementAddOnV1Beta1(addonName, addon)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Get v1beta1 ClusterManagementAddOn using v1beta1 client")
		gomega.Eventually(func() bool {
			addon, err = hub.GetClusterManagementAddOnV1Beta1(addonName)
			if err != nil {
				return false
			}
			return true
		}).Should(gomega.BeTrue())

		ginkgo.By("Update v1beta1 ClusterManagementAddOn status using v1beta1 client")
		gomega.Eventually(func() bool {
			addon, err = hub.GetClusterManagementAddOnV1Beta1(addonName)
			if err != nil {
				return false
			}
			addon.Status.InstallProgressions = []addonv1beta1.InstallProgression{
				{
					PlacementRef: addonv1beta1.PlacementRef{
						Name:      "test-placement",
						Namespace: "default",
					},
					ConfigReferences: []addonv1beta1.InstallConfigReference{
						{
							ConfigGroupResource: addonv1beta1.ConfigGroupResource{
								Group:    "addon.open-cluster-management.io",
								Resource: "addondeploymentconfigs",
							},
							DesiredConfig: &addonv1beta1.ConfigSpecHash{
								ConfigReferent: addonv1beta1.ConfigReferent{
									Name: "progression-config",
								},
							},
						},
					},
				},
			}
			_, err = hub.AddonClient.AddonV1beta1().ClusterManagementAddOns().UpdateStatus(
				context.Background(), addon, metav1.UpdateOptions{})
			if err != nil {
				return false
			}
			return true
		}).Should(gomega.BeTrue())

		ginkgo.By("Delete v1beta1 ClusterManagementAddOn using v1beta1 client")
		gomega.Eventually(func() bool {
			err = hub.DeleteClusterManagementAddOnV1Beta1(addonName)
			if err != nil {
				return false
			}
			return true
		}).Should(gomega.BeTrue())
	})

	ginkgo.It("Create a v1beta1 ClusterManagementAddOn and get/update/delete with v1alpha1 client", func() {
		suffix := rand.String(6)
		addonName := fmt.Sprintf("cma-%s", suffix)

		ginkgo.By("Create a v1beta1 ClusterManagementAddOn")
		addon := &addonv1beta1.ClusterManagementAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name: addonName,
			},
			Spec: addonv1beta1.ClusterManagementAddOnSpec{
				AddOnMeta: addonv1beta1.AddOnMeta{
					DisplayName: "Test Beta Addon",
					Description: "Test addon for v1beta1",
				},
				InstallStrategy: addonv1beta1.InstallStrategy{
					Type: addonv1beta1.AddonInstallStrategyManual,
				},
				DefaultConfigs: []addonv1beta1.AddOnConfig{
					{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{
							Group:    "addon.open-cluster-management.io",
							Resource: "addondeploymentconfigs",
						},
						ConfigReferent: addonv1beta1.ConfigReferent{Name: "beta-config"},
					},
				},
			},
		}

		_, err := hub.CreateClusterManagementAddOnV1Beta1(addonName, addon)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Update v1beta1 ClusterManagementAddOn status using v1beta1 client")
		gomega.Eventually(func() bool {
			addon, err = hub.GetClusterManagementAddOnV1Beta1(addonName)
			if err != nil {
				return false
			}
			addon.Status.InstallProgressions = []addonv1beta1.InstallProgression{
				{
					PlacementRef: addonv1beta1.PlacementRef{
						Name:      "test-placement",
						Namespace: "default",
					},
					ConfigReferences: []addonv1beta1.InstallConfigReference{
						{
							ConfigGroupResource: addonv1beta1.ConfigGroupResource{
								Group:    "addon.open-cluster-management.io",
								Resource: "addondeploymentconfigs",
							},
							DesiredConfig: &addonv1beta1.ConfigSpecHash{
								ConfigReferent: addonv1beta1.ConfigReferent{
									Name: "progression-config",
								},
							},
						},
					},
				},
			}
			_, err = hub.AddonClient.AddonV1beta1().ClusterManagementAddOns().UpdateStatus(
				context.Background(), addon, metav1.UpdateOptions{})
			if err != nil {
				return false
			}
			return true
		}).Should(gomega.BeTrue())

		ginkgo.By("Get v1beta1 ClusterManagementAddOn using v1alpha1 client and verify conversion")
		gomega.Eventually(func() bool {
			v1alpha1Addon, err := hub.GetClusterManagementAddOnV1Alpha1(addonName)
			if err != nil {
				return false
			}
			// Verify spec.defaultConfigs → spec.supportedConfigs conversion
			if len(v1alpha1Addon.Spec.SupportedConfigs) != 1 {
				return false
			}
			if v1alpha1Addon.Spec.SupportedConfigs[0].DefaultConfig.Name != "beta-config" {
				return false
			}
			// Verify status.InstallProgressions conversion
			if len(v1alpha1Addon.Status.InstallProgressions) != 1 {
				return false
			}
			if len(v1alpha1Addon.Status.InstallProgressions[0].ConfigReferences) != 1 {
				return false
			}
			if v1alpha1Addon.Status.InstallProgressions[0].ConfigReferences[0].DesiredConfig.Name != "progression-config" {
				return false
			}
			return true
		}).Should(gomega.BeTrue())

		ginkgo.By("Update v1beta1 ClusterManagementAddOn using v1alpha1 client")
		gomega.Eventually(func() error {
			v1alpha1Addon, err := hub.GetClusterManagementAddOnV1Alpha1(addonName)
			if err != nil {
				return err
			}
			// Only append if not already present
			if len(v1alpha1Addon.Spec.SupportedConfigs) < 2 {
				v1alpha1Addon.Spec.SupportedConfigs = append(v1alpha1Addon.Spec.SupportedConfigs,
					addonv1alpha1.ConfigMeta{
						ConfigGroupResource: addonv1alpha1.ConfigGroupResource{
							Group:    "addon.open-cluster-management.io",
							Resource: "addontemplates", // Different resource to avoid duplicate key
						},
						DefaultConfig: &addonv1alpha1.ConfigReferent{Name: "alpha-config"},
					})
			}
			_, err = hub.UpdateClusterManagementAddOnV1Alpha1(v1alpha1Addon)
			return err
		}).Should(gomega.Succeed())

		ginkgo.By("Verify spec update via v1beta1 client")
		gomega.Eventually(func() bool {
			addon, err = hub.GetClusterManagementAddOnV1Beta1(addonName)
			if err != nil {
				return false
			}
			return len(addon.Spec.DefaultConfigs) == 2
		}).Should(gomega.BeTrue())

		ginkgo.By("Delete v1beta1 ClusterManagementAddOn using v1alpha1 client")
		gomega.Eventually(func() bool {
			err = hub.DeleteClusterManagementAddOnV1Alpha1(addonName)
			if err != nil {
				return false
			}
			return true
		}).Should(gomega.BeTrue())
	})
})

var _ = ginkgo.Describe("Webhook infrastructure", ginkgo.Label("addon-conversion"), func() {
	ginkgo.It("should have CRD conversion configured", func() {
		ginkgo.By("Verifying ManagedClusterAddOn CRD has webhook conversion")
		mcaCRD, err := hub.APIExtensionsClient.ApiextensionsV1().CustomResourceDefinitions().Get(
			context.Background(), "managedclusteraddons.addon.open-cluster-management.io", metav1.GetOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(string(mcaCRD.Spec.Conversion.Strategy)).To(gomega.Equal("Webhook"))

		ginkgo.By("Verifying ClusterManagementAddOn CRD has webhook conversion")
		cmaCRD, err := hub.APIExtensionsClient.ApiextensionsV1().CustomResourceDefinitions().Get(
			context.Background(), "clustermanagementaddons.addon.open-cluster-management.io", metav1.GetOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(string(cmaCRD.Spec.Conversion.Strategy)).To(gomega.Equal("Webhook"))
	})
})
