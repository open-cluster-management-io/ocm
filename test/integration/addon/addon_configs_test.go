package integration

import (
	"context"
	"fmt"

	ginkgo "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
)

var _ = ginkgo.Describe("AddConfigs", func() {
	var managedClusterName string
	var configDefaultNamespace string
	var configDefaultName string
	var err error

	ginkgo.BeforeEach(func() {
		suffix := rand.String(5)
		managedClusterName = fmt.Sprintf("managedcluster-%s", suffix)
		configDefaultNamespace = fmt.Sprintf("default-config-%s", suffix)
		configDefaultName = fmt.Sprintf("default-config-%s", suffix)

		// prepare cluster
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

		clusterNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: managedClusterName}}
		_, err = hubKubeClient.CoreV1().Namespaces().Create(context.Background(), clusterNS, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		// prepare ClusterManagementAddon
		_, err = createClusterManagementAddOn(testAddOnConfigsImpl.name, configDefaultNamespace, configDefaultName)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		assertClusterManagementAddOnAnnotations(testAddOnConfigsImpl.name)

		// prepare default config
		configDefaultNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: configDefaultNamespace}}
		_, err = hubKubeClient.CoreV1().Namespaces().Create(context.Background(), configDefaultNS, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		addOnDefaultConfig := &addonapiv1alpha1.AddOnDeploymentConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configDefaultName,
				Namespace: configDefaultNamespace,
			},
			Spec: addOnDefaultConfigSpec,
		}
		_, err = hubAddonClient.AddonV1alpha1().AddOnDeploymentConfigs(configDefaultNamespace).Create(
			context.Background(), addOnDefaultConfig, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	ginkgo.AfterEach(func() {
		err = hubKubeClient.CoreV1().Namespaces().Delete(context.Background(), managedClusterName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		err = hubClusterClient.ClusterV1().ManagedClusters().Delete(context.Background(), managedClusterName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		err = hubKubeClient.CoreV1().Namespaces().Delete(context.Background(), configDefaultNamespace, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		err = hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().Delete(context.Background(), testAddOnConfigsImpl.name, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		delete(testAddOnConfigsImpl.registrations, managedClusterName)
	})

	ginkgo.It("Should use default config", func() {
		addon := &addonapiv1alpha1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testAddOnConfigsImpl.name,
				Namespace: managedClusterName,
			},
			Spec: addonapiv1alpha1.ManagedClusterAddOnSpec{
				InstallNamespace: "test",
			},
		}
		_, err = hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Create(context.Background(), addon, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		// check cma status
		assertClusterManagementAddOnDefaultConfigReferences(testAddOnConfigsImpl.name, addonapiv1alpha1.DefaultConfigReference{
			ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
				Group:    addOnDeploymentConfigGVR.Group,
				Resource: addOnDeploymentConfigGVR.Resource,
			},
			DesiredConfig: &addonapiv1alpha1.ConfigSpecHash{
				ConfigReferent: addonapiv1alpha1.ConfigReferent{
					Namespace: configDefaultNamespace,
					Name:      configDefaultName,
				},
				SpecHash: addOnDefaultConfigSpecHash,
			},
		})
		assertClusterManagementAddOnInstallProgression(testAddOnConfigsImpl.name)

		// check mca status
		assertManagedClusterAddOnConfigReferences(testAddOnConfigsImpl.name, managedClusterName, addonapiv1alpha1.ConfigReference{
			ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
				Group:    addOnDeploymentConfigGVR.Group,
				Resource: addOnDeploymentConfigGVR.Resource,
			},
			ConfigReferent: addonapiv1alpha1.ConfigReferent{
				Namespace: configDefaultNamespace,
				Name:      configDefaultName,
			},
			LastObservedGeneration: 1,
			DesiredConfig: &addonapiv1alpha1.ConfigSpecHash{
				ConfigReferent: addonapiv1alpha1.ConfigReferent{
					Namespace: configDefaultNamespace,
					Name:      configDefaultName,
				},
				SpecHash: addOnDefaultConfigSpecHash,
			},
		})
	})

	ginkgo.It("Should override default config by install strategy", func() {
		cma, err := hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().Get(context.Background(), testAddOnConfigsImpl.name, metav1.GetOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		cma.Spec.InstallStrategy = addonapiv1alpha1.InstallStrategy{
			Type: addonapiv1alpha1.AddonInstallStrategyPlacements,
			Placements: []addonapiv1alpha1.PlacementStrategy{
				{
					PlacementRef: addonapiv1alpha1.PlacementRef{Name: "test-placement", Namespace: configDefaultNamespace},
					Configs: []addonapiv1alpha1.AddOnConfig{
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    addOnDeploymentConfigGVR.Group,
								Resource: addOnDeploymentConfigGVR.Resource,
							},
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Namespace: configDefaultNamespace,
								Name:      "another-config",
							},
						},
					},
					RolloutStrategy: clusterv1alpha1.RolloutStrategy{
						Type: clusterv1alpha1.All,
					},
				},
			},
		}
		patchClusterManagementAddOn(context.Background(), cma)

		placement := &clusterv1beta1.Placement{ObjectMeta: metav1.ObjectMeta{Name: "test-placement", Namespace: configDefaultNamespace}}
		_, err = hubClusterClient.ClusterV1beta1().Placements(configDefaultNamespace).Create(context.Background(), placement, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		decision := &clusterv1beta1.PlacementDecision{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-placement",
				Namespace: configDefaultNamespace,
				Labels: map[string]string{
					clusterv1beta1.PlacementLabel:          "test-placement",
					clusterv1beta1.DecisionGroupIndexLabel: "0",
				},
			},
		}
		decision, err = hubClusterClient.ClusterV1beta1().PlacementDecisions(configDefaultNamespace).Create(context.Background(), decision, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		decision.Status.Decisions = []clusterv1beta1.ClusterDecision{
			{ClusterName: managedClusterName},
		}
		_, err = hubClusterClient.ClusterV1beta1().PlacementDecisions(configDefaultNamespace).UpdateStatus(context.Background(), decision, metav1.UpdateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		// check cma status
		assertClusterManagementAddOnDefaultConfigReferences(testAddOnConfigsImpl.name, addonapiv1alpha1.DefaultConfigReference{
			ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
				Group:    addOnDeploymentConfigGVR.Group,
				Resource: addOnDeploymentConfigGVR.Resource,
			},
			DesiredConfig: &addonapiv1alpha1.ConfigSpecHash{
				ConfigReferent: addonapiv1alpha1.ConfigReferent{
					Namespace: configDefaultNamespace,
					Name:      configDefaultName,
				},
				SpecHash: addOnDefaultConfigSpecHash,
			},
		})
		assertClusterManagementAddOnInstallProgression(testAddOnConfigsImpl.name, addonapiv1alpha1.InstallProgression{
			PlacementRef: addonapiv1alpha1.PlacementRef{Name: "test-placement", Namespace: configDefaultNamespace},
			ConfigReferences: []addonapiv1alpha1.InstallConfigReference{
				{
					ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
						Group:    addOnDeploymentConfigGVR.Group,
						Resource: addOnDeploymentConfigGVR.Resource,
					},
					DesiredConfig: &addonapiv1alpha1.ConfigSpecHash{
						ConfigReferent: addonapiv1alpha1.ConfigReferent{
							Namespace: configDefaultNamespace,
							Name:      "another-config",
						},
						SpecHash: "",
					},
				},
			},
		})

		// check mca status
		assertManagedClusterAddOnConfigReferences(testAddOnConfigsImpl.name, managedClusterName, addonapiv1alpha1.ConfigReference{
			ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
				Group:    addOnDeploymentConfigGVR.Group,
				Resource: addOnDeploymentConfigGVR.Resource,
			},
			ConfigReferent: addonapiv1alpha1.ConfigReferent{
				Namespace: configDefaultNamespace,
				Name:      "another-config",
			},
			LastObservedGeneration: 0,
			DesiredConfig: &addonapiv1alpha1.ConfigSpecHash{
				ConfigReferent: addonapiv1alpha1.ConfigReferent{
					Namespace: configDefaultNamespace,
					Name:      "another-config",
				},
				SpecHash: "",
			},
		})
	})

	ginkgo.It("Should override default config", func() {
		addOnConfig := &addonapiv1alpha1.AddOnDeploymentConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "addon-config",
				Namespace: managedClusterName,
			},
			Spec: addOnTest1ConfigSpec,
		}
		_, err = hubAddonClient.AddonV1alpha1().AddOnDeploymentConfigs(managedClusterName).Create(context.Background(), addOnConfig, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		addon := &addonapiv1alpha1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testAddOnConfigsImpl.name,
				Namespace: managedClusterName,
			},
			Spec: addonapiv1alpha1.ManagedClusterAddOnSpec{
				InstallNamespace: "test",
				Configs: []addonapiv1alpha1.AddOnConfig{
					{
						ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
							Group:    addOnDeploymentConfigGVR.Group,
							Resource: addOnDeploymentConfigGVR.Resource,
						},
						ConfigReferent: addonapiv1alpha1.ConfigReferent{
							Name:      addOnConfig.Name,
							Namespace: addOnConfig.Namespace,
						},
					},
				},
			},
		}
		_, err = hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Create(context.Background(), addon, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		addon.Status = addonapiv1alpha1.ManagedClusterAddOnStatus{
			SupportedConfigs: []addonapiv1alpha1.ConfigGroupResource{
				{
					Group:    addOnDeploymentConfigGVR.Group,
					Resource: addOnDeploymentConfigGVR.Resource,
				},
			},
		}
		updateManagedClusterAddOnStatus(context.Background(), addon)

		// check cma status
		assertClusterManagementAddOnDefaultConfigReferences(testAddOnConfigsImpl.name, addonapiv1alpha1.DefaultConfigReference{
			ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
				Group:    addOnDeploymentConfigGVR.Group,
				Resource: addOnDeploymentConfigGVR.Resource,
			},
			DesiredConfig: &addonapiv1alpha1.ConfigSpecHash{
				ConfigReferent: addonapiv1alpha1.ConfigReferent{
					Namespace: configDefaultNamespace,
					Name:      configDefaultName,
				},
				SpecHash: addOnDefaultConfigSpecHash,
			},
		})
		assertClusterManagementAddOnInstallProgression(testAddOnConfigsImpl.name)

		// check mca status
		assertManagedClusterAddOnConfigReferences(testAddOnConfigsImpl.name, managedClusterName, addonapiv1alpha1.ConfigReference{
			ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
				Group:    addOnDeploymentConfigGVR.Group,
				Resource: addOnDeploymentConfigGVR.Resource,
			},
			ConfigReferent: addonapiv1alpha1.ConfigReferent{
				Namespace: addOnConfig.Namespace,
				Name:      addOnConfig.Name,
			},
			LastObservedGeneration: 1,
			DesiredConfig: &addonapiv1alpha1.ConfigSpecHash{
				ConfigReferent: addonapiv1alpha1.ConfigReferent{
					Namespace: addOnConfig.Namespace,
					Name:      addOnConfig.Name,
				},
				SpecHash: addOnTest1ConfigSpecHash,
			},
		})
	})

	ginkgo.It("Should update config spec successfully", func() {
		addOnConfig := &addonapiv1alpha1.AddOnDeploymentConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "addon-config",
				Namespace: managedClusterName,
			},
			Spec: addOnTest1ConfigSpec,
		}
		_, err = hubAddonClient.AddonV1alpha1().AddOnDeploymentConfigs(managedClusterName).Create(context.Background(), addOnConfig, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		addon := &addonapiv1alpha1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testAddOnConfigsImpl.name,
				Namespace: managedClusterName,
			},
			Spec: addonapiv1alpha1.ManagedClusterAddOnSpec{
				InstallNamespace: "test",
				Configs: []addonapiv1alpha1.AddOnConfig{
					{
						ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
							Group:    addOnDeploymentConfigGVR.Group,
							Resource: addOnDeploymentConfigGVR.Resource,
						},
						ConfigReferent: addonapiv1alpha1.ConfigReferent{
							Name:      addOnConfig.Name,
							Namespace: addOnConfig.Namespace,
						},
					},
				},
			},
		}
		addon, err = hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Create(context.Background(), addon, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		addon.Status = addonapiv1alpha1.ManagedClusterAddOnStatus{
			SupportedConfigs: []addonapiv1alpha1.ConfigGroupResource{
				{
					Group:    addOnDeploymentConfigGVR.Group,
					Resource: addOnDeploymentConfigGVR.Resource,
				},
			},
		}
		updateManagedClusterAddOnStatus(context.Background(), addon)

		// check mca status
		assertManagedClusterAddOnConfigReferences(testAddOnConfigsImpl.name, managedClusterName, addonapiv1alpha1.ConfigReference{
			ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
				Group:    addOnDeploymentConfigGVR.Group,
				Resource: addOnDeploymentConfigGVR.Resource,
			},
			ConfigReferent: addonapiv1alpha1.ConfigReferent{
				Namespace: addOnConfig.Namespace,
				Name:      addOnConfig.Name,
			},
			LastObservedGeneration: 1,
			DesiredConfig: &addonapiv1alpha1.ConfigSpecHash{
				ConfigReferent: addonapiv1alpha1.ConfigReferent{
					Namespace: addOnConfig.Namespace,
					Name:      addOnConfig.Name,
				},
				SpecHash: addOnTest1ConfigSpecHash,
			},
		})

		addOnConfig, err = hubAddonClient.AddonV1alpha1().AddOnDeploymentConfigs(managedClusterName).Get(context.Background(), addOnConfig.Name, metav1.GetOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		addOnConfig.Spec = addOnTest2ConfigSpec
		_, err = hubAddonClient.AddonV1alpha1().AddOnDeploymentConfigs(managedClusterName).Update(context.Background(), addOnConfig, metav1.UpdateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		// check mca status
		assertManagedClusterAddOnConfigReferences(testAddOnConfigsImpl.name, managedClusterName, addonapiv1alpha1.ConfigReference{
			ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
				Group:    addOnDeploymentConfigGVR.Group,
				Resource: addOnDeploymentConfigGVR.Resource,
			},
			ConfigReferent: addonapiv1alpha1.ConfigReferent{
				Namespace: addOnConfig.Namespace,
				Name:      addOnConfig.Name,
			},
			LastObservedGeneration: 2,
			DesiredConfig: &addonapiv1alpha1.ConfigSpecHash{
				ConfigReferent: addonapiv1alpha1.ConfigReferent{
					Namespace: addOnConfig.Namespace,
					Name:      addOnConfig.Name,
				},
				SpecHash: addOnTest2ConfigSpecHash,
			},
		})
	})

	ginkgo.It("Should not update unsupported config spec hash", func() {
		// do not update mca status.SupportedConfigs
		addon := &addonapiv1alpha1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testAddOnConfigsImpl.name,
				Namespace: managedClusterName,
			},
			Spec: addonapiv1alpha1.ManagedClusterAddOnSpec{
				InstallNamespace: "test",
				Configs: []addonapiv1alpha1.AddOnConfig{
					{
						ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
							Group:    addOnDeploymentConfigGVR.Group + "test",
							Resource: addOnDeploymentConfigGVR.Resource + "test",
						},
						ConfigReferent: addonapiv1alpha1.ConfigReferent{
							Name:      "addon-config-test",
							Namespace: managedClusterName,
						},
					},
				},
			},
		}
		_, err = hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Create(context.Background(), addon, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		// check mca status
		assertManagedClusterAddOnConfigReferences(testAddOnConfigsImpl.name, managedClusterName, addonapiv1alpha1.ConfigReference{
			ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
				Group:    addOnDeploymentConfigGVR.Group,
				Resource: addOnDeploymentConfigGVR.Resource,
			},
			ConfigReferent: addonapiv1alpha1.ConfigReferent{
				Namespace: configDefaultNamespace,
				Name:      configDefaultName,
			},
			LastObservedGeneration: 1,
			DesiredConfig: &addonapiv1alpha1.ConfigSpecHash{
				ConfigReferent: addonapiv1alpha1.ConfigReferent{
					Namespace: configDefaultNamespace,
					Name:      configDefaultName,
				},
				SpecHash: addOnDefaultConfigSpecHash,
			},
		}, addonapiv1alpha1.ConfigReference{
			ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
				Group:    addOnDeploymentConfigGVR.Group + "test",
				Resource: addOnDeploymentConfigGVR.Resource + "test",
			},
			ConfigReferent: addonapiv1alpha1.ConfigReferent{
				Namespace: managedClusterName,
				Name:      "addon-config-test",
			},
			LastObservedGeneration: 0,
			DesiredConfig: &addonapiv1alpha1.ConfigSpecHash{
				ConfigReferent: addonapiv1alpha1.ConfigReferent{
					Namespace: managedClusterName,
					Name:      "addon-config-test",
				},
				SpecHash: "",
			},
		})
	})
})
