package integration

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	addonapiv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
)

var _ = Describe("AddConfigs Beta", func() {
	var managedClusterName string
	var configDefaultNamespace string
	var configDefaultName string
	var err error

	BeforeEach(func(ctx SpecContext) {
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
		_, err = hubClusterClient.ClusterV1().ManagedClusters().Create(ctx, managedCluster, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		clusterNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: managedClusterName}}
		_, err = hubKubeClient.CoreV1().Namespaces().Create(ctx, clusterNS, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		// prepare ClusterManagementAddon
		_, err = createClusterManagementAddOnBeta(ctx, testAddOnConfigsImpl.name, configDefaultNamespace, configDefaultName)
		Expect(err).ToNot(HaveOccurred())

		assertClusterManagementAddOnAnnotationsBeta(ctx, testAddOnConfigsImpl.name)

		// prepare default config
		configDefaultNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: configDefaultNamespace}}
		_, err = hubKubeClient.CoreV1().Namespaces().Create(ctx, configDefaultNS, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		addOnDefaultConfig := &addonapiv1beta1.AddOnDeploymentConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configDefaultName,
				Namespace: configDefaultNamespace,
			},
			Spec: addOnDefaultConfigSpecBeta,
		}
		_, err = hubAddonClient.AddonV1beta1().AddOnDeploymentConfigs(configDefaultNamespace).Create(
			ctx, addOnDefaultConfig, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func(ctx SpecContext) {
		err = hubKubeClient.CoreV1().Namespaces().Delete(ctx, managedClusterName, metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())
		err = hubClusterClient.ClusterV1().ManagedClusters().Delete(ctx, managedClusterName, metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())
		err = hubKubeClient.CoreV1().Namespaces().Delete(ctx, configDefaultNamespace, metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())
		err = hubAddonClient.AddonV1beta1().ClusterManagementAddOns().Delete(ctx, testAddOnConfigsImpl.name, metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())
		delete(testAddOnConfigsImpl.registrations, managedClusterName)
	})

	It("Should use default config", func(ctx SpecContext) {
		addon := &addonapiv1beta1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testAddOnConfigsImpl.name,
				Namespace: managedClusterName,
			},
			Spec: addonapiv1beta1.ManagedClusterAddOnSpec{},
		}
		_, err = hubAddonClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).Create(ctx, addon, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		// check cma status
		assertClusterManagementAddOnDefaultConfigReferencesBeta(ctx, testAddOnConfigsImpl.name, addonapiv1beta1.DefaultConfigReference{
			ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
				Group:    addOnDeploymentConfigGVR.Group,
				Resource: addOnDeploymentConfigGVR.Resource,
			},
			DesiredConfig: &addonapiv1beta1.ConfigSpecHash{
				ConfigReferent: addonapiv1beta1.ConfigReferent{
					Namespace: configDefaultNamespace,
					Name:      configDefaultName,
				},
				SpecHash: addOnDefaultConfigSpecHash,
			},
		})
		assertClusterManagementAddOnInstallProgressionBeta(ctx, testAddOnConfigsImpl.name)

		// check mca status
		assertManagedClusterAddOnConfigReferencesBeta(ctx, testAddOnConfigsImpl.name, managedClusterName, addonapiv1beta1.ConfigReference{
			ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
				Group:    addOnDeploymentConfigGVR.Group,
				Resource: addOnDeploymentConfigGVR.Resource,
			},
			LastObservedGeneration: 1,
			DesiredConfig: &addonapiv1beta1.ConfigSpecHash{
				ConfigReferent: addonapiv1beta1.ConfigReferent{
					Namespace: configDefaultNamespace,
					Name:      configDefaultName,
				},
				SpecHash: addOnDefaultConfigSpecHash,
			},
		})
	})

	It("Should override default config by install strategy", func(ctx SpecContext) {
		cma, err := hubAddonClient.AddonV1beta1().ClusterManagementAddOns().Get(ctx, testAddOnConfigsImpl.name, metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())

		cma.Spec.InstallStrategy = addonapiv1beta1.InstallStrategy{
			Type: addonapiv1beta1.AddonInstallStrategyPlacements,
			Placements: []addonapiv1beta1.PlacementStrategy{
				{
					PlacementRef: addonapiv1beta1.PlacementRef{Name: "test-placement", Namespace: configDefaultNamespace},
					Configs: []addonapiv1beta1.AddOnConfig{
						{
							ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
								Group:    addOnDeploymentConfigGVR.Group,
								Resource: addOnDeploymentConfigGVR.Resource,
							},
							ConfigReferent: addonapiv1beta1.ConfigReferent{
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
		patchClusterManagementAddOnBeta(ctx, cma)

		placement := &clusterv1beta1.Placement{ObjectMeta: metav1.ObjectMeta{Name: "test-placement", Namespace: configDefaultNamespace}}
		_, err = hubClusterClient.ClusterV1beta1().Placements(configDefaultNamespace).Create(ctx, placement, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

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
		decision, err = hubClusterClient.ClusterV1beta1().PlacementDecisions(configDefaultNamespace).Create(ctx, decision, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		decision.Status.Decisions = []clusterv1beta1.ClusterDecision{
			{ClusterName: managedClusterName},
		}
		_, err = hubClusterClient.ClusterV1beta1().PlacementDecisions(configDefaultNamespace).UpdateStatus(ctx, decision, metav1.UpdateOptions{})
		Expect(err).ToNot(HaveOccurred())

		// check cma status
		assertClusterManagementAddOnDefaultConfigReferencesBeta(ctx, testAddOnConfigsImpl.name, addonapiv1beta1.DefaultConfigReference{
			ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
				Group:    addOnDeploymentConfigGVR.Group,
				Resource: addOnDeploymentConfigGVR.Resource,
			},
			DesiredConfig: &addonapiv1beta1.ConfigSpecHash{
				ConfigReferent: addonapiv1beta1.ConfigReferent{
					Namespace: configDefaultNamespace,
					Name:      configDefaultName,
				},
				SpecHash: addOnDefaultConfigSpecHash,
			},
		})
		assertClusterManagementAddOnInstallProgressionBeta(ctx, testAddOnConfigsImpl.name, addonapiv1beta1.InstallProgression{
			PlacementRef: addonapiv1beta1.PlacementRef{Name: "test-placement", Namespace: configDefaultNamespace},
			ConfigReferences: []addonapiv1beta1.InstallConfigReference{
				{
					ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
						Group:    addOnDeploymentConfigGVR.Group,
						Resource: addOnDeploymentConfigGVR.Resource,
					},
					DesiredConfig: &addonapiv1beta1.ConfigSpecHash{
						ConfigReferent: addonapiv1beta1.ConfigReferent{
							Namespace: configDefaultNamespace,
							Name:      "another-config",
						},
						SpecHash: "",
					},
				},
			},
		})

		// check mca status
		assertManagedClusterAddOnConfigReferencesBeta(ctx, testAddOnConfigsImpl.name, managedClusterName, addonapiv1beta1.ConfigReference{
			ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
				Group:    addOnDeploymentConfigGVR.Group,
				Resource: addOnDeploymentConfigGVR.Resource,
			},
			LastObservedGeneration: 0,
			DesiredConfig: &addonapiv1beta1.ConfigSpecHash{
				ConfigReferent: addonapiv1beta1.ConfigReferent{
					Namespace: configDefaultNamespace,
					Name:      "another-config",
				},
				SpecHash: "",
			},
		})
	})

	It("Should override default config", func(ctx SpecContext) {
		addOnConfig := &addonapiv1beta1.AddOnDeploymentConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "addon-config",
				Namespace: managedClusterName,
			},
			Spec: addOnTest1ConfigSpecBeta,
		}
		_, err = hubAddonClient.AddonV1beta1().AddOnDeploymentConfigs(managedClusterName).Create(ctx, addOnConfig, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		addon := &addonapiv1beta1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testAddOnConfigsImpl.name,
				Namespace: managedClusterName,
			},
			Spec: addonapiv1beta1.ManagedClusterAddOnSpec{
				Configs: []addonapiv1beta1.AddOnConfig{
					{
						ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
							Group:    addOnDeploymentConfigGVR.Group,
							Resource: addOnDeploymentConfigGVR.Resource,
						},
						ConfigReferent: addonapiv1beta1.ConfigReferent{
							Name:      addOnConfig.Name,
							Namespace: addOnConfig.Namespace,
						},
					},
				},
			},
		}
		_, err = hubAddonClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).Create(ctx, addon, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		addon.Status = addonapiv1beta1.ManagedClusterAddOnStatus{
			SupportedConfigs: []addonapiv1beta1.ConfigGroupResource{
				{
					Group:    addOnDeploymentConfigGVR.Group,
					Resource: addOnDeploymentConfigGVR.Resource,
				},
			},
		}
		updateManagedClusterAddOnStatusBeta(ctx, addon)

		// check cma status
		assertClusterManagementAddOnDefaultConfigReferencesBeta(ctx, testAddOnConfigsImpl.name, addonapiv1beta1.DefaultConfigReference{
			ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
				Group:    addOnDeploymentConfigGVR.Group,
				Resource: addOnDeploymentConfigGVR.Resource,
			},
			DesiredConfig: &addonapiv1beta1.ConfigSpecHash{
				ConfigReferent: addonapiv1beta1.ConfigReferent{
					Namespace: configDefaultNamespace,
					Name:      configDefaultName,
				},
				SpecHash: addOnDefaultConfigSpecHash,
			},
		})
		assertClusterManagementAddOnInstallProgressionBeta(ctx, testAddOnConfigsImpl.name)

		// check mca status
		assertManagedClusterAddOnConfigReferencesBeta(ctx, testAddOnConfigsImpl.name, managedClusterName, addonapiv1beta1.ConfigReference{
			ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
				Group:    addOnDeploymentConfigGVR.Group,
				Resource: addOnDeploymentConfigGVR.Resource,
			},
			LastObservedGeneration: 1,
			DesiredConfig: &addonapiv1beta1.ConfigSpecHash{
				ConfigReferent: addonapiv1beta1.ConfigReferent{
					Namespace: addOnConfig.Namespace,
					Name:      addOnConfig.Name,
				},
				SpecHash: addOnTest1ConfigSpecHash,
			},
		})
	})

	It("Should update config spec successfully", func(ctx SpecContext) {
		addOnConfig := &addonapiv1beta1.AddOnDeploymentConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "addon-config",
				Namespace: managedClusterName,
			},
			Spec: addOnTest1ConfigSpecBeta,
		}
		_, err = hubAddonClient.AddonV1beta1().AddOnDeploymentConfigs(managedClusterName).Create(ctx, addOnConfig, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		addon := &addonapiv1beta1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testAddOnConfigsImpl.name,
				Namespace: managedClusterName,
			},
			Spec: addonapiv1beta1.ManagedClusterAddOnSpec{
				Configs: []addonapiv1beta1.AddOnConfig{
					{
						ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
							Group:    addOnDeploymentConfigGVR.Group,
							Resource: addOnDeploymentConfigGVR.Resource,
						},
						ConfigReferent: addonapiv1beta1.ConfigReferent{
							Name:      addOnConfig.Name,
							Namespace: addOnConfig.Namespace,
						},
					},
				},
			},
		}
		addon, err = hubAddonClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).Create(ctx, addon, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		addon.Status = addonapiv1beta1.ManagedClusterAddOnStatus{
			SupportedConfigs: []addonapiv1beta1.ConfigGroupResource{
				{
					Group:    addOnDeploymentConfigGVR.Group,
					Resource: addOnDeploymentConfigGVR.Resource,
				},
			},
		}
		updateManagedClusterAddOnStatusBeta(ctx, addon)

		// check mca status
		assertManagedClusterAddOnConfigReferencesBeta(ctx, testAddOnConfigsImpl.name, managedClusterName, addonapiv1beta1.ConfigReference{
			ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
				Group:    addOnDeploymentConfigGVR.Group,
				Resource: addOnDeploymentConfigGVR.Resource,
			},
			LastObservedGeneration: 1,
			DesiredConfig: &addonapiv1beta1.ConfigSpecHash{
				ConfigReferent: addonapiv1beta1.ConfigReferent{
					Namespace: addOnConfig.Namespace,
					Name:      addOnConfig.Name,
				},
				SpecHash: addOnTest1ConfigSpecHash,
			},
		})

		addOnConfig, err = hubAddonClient.AddonV1beta1().AddOnDeploymentConfigs(managedClusterName).Get(ctx, addOnConfig.Name, metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())

		addOnConfig.Spec = addOnTest2ConfigSpecBeta
		_, err = hubAddonClient.AddonV1beta1().AddOnDeploymentConfigs(managedClusterName).Update(ctx, addOnConfig, metav1.UpdateOptions{})
		Expect(err).ToNot(HaveOccurred())

		// check mca status
		assertManagedClusterAddOnConfigReferencesBeta(ctx, testAddOnConfigsImpl.name, managedClusterName, addonapiv1beta1.ConfigReference{
			ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
				Group:    addOnDeploymentConfigGVR.Group,
				Resource: addOnDeploymentConfigGVR.Resource,
			},
			LastObservedGeneration: 2,
			DesiredConfig: &addonapiv1beta1.ConfigSpecHash{
				ConfigReferent: addonapiv1beta1.ConfigReferent{
					Namespace: addOnConfig.Namespace,
					Name:      addOnConfig.Name,
				},
				SpecHash: addOnTest2ConfigSpecHash,
			},
		})
	})

	It("Should not update unsupported config spec hash", func(ctx SpecContext) {
		// do not update mca status.SupportedConfigs
		addon := &addonapiv1beta1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testAddOnConfigsImpl.name,
				Namespace: managedClusterName,
			},
			Spec: addonapiv1beta1.ManagedClusterAddOnSpec{
				Configs: []addonapiv1beta1.AddOnConfig{
					{
						ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
							Group:    addOnDeploymentConfigGVR.Group + "test",
							Resource: addOnDeploymentConfigGVR.Resource + "test",
						},
						ConfigReferent: addonapiv1beta1.ConfigReferent{
							Name:      "addon-config-test",
							Namespace: managedClusterName,
						},
					},
				},
			},
		}
		_, err = hubAddonClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).Create(ctx, addon, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		// check mca status
		assertManagedClusterAddOnConfigReferencesBeta(ctx, testAddOnConfigsImpl.name, managedClusterName, addonapiv1beta1.ConfigReference{
			ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
				Group:    addOnDeploymentConfigGVR.Group,
				Resource: addOnDeploymentConfigGVR.Resource,
			},
			LastObservedGeneration: 1,
			DesiredConfig: &addonapiv1beta1.ConfigSpecHash{
				ConfigReferent: addonapiv1beta1.ConfigReferent{
					Namespace: configDefaultNamespace,
					Name:      configDefaultName,
				},
				SpecHash: addOnDefaultConfigSpecHash,
			},
		}, addonapiv1beta1.ConfigReference{
			ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
				Group:    addOnDeploymentConfigGVR.Group + "test",
				Resource: addOnDeploymentConfigGVR.Resource + "test",
			},
			LastObservedGeneration: 0,
			DesiredConfig: &addonapiv1beta1.ConfigSpecHash{
				ConfigReferent: addonapiv1beta1.ConfigReferent{
					Namespace: managedClusterName,
					Name:      "addon-config-test",
				},
				SpecHash: "",
			},
		})
	})

	It("Should preserve config reference ordering for multiple same-GVK configs in a placement", func(ctx SpecContext) {
		// Create two AddonDeploymentConfigs of the same GVK in the placement namespace.
		// The ordering in the CMA placement spec should be preserved in both the CMA status
		// InstallProgressions and the MCA status ConfigReferences.
		config1 := &addonapiv1beta1.AddOnDeploymentConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "config-first",
				Namespace: configDefaultNamespace,
			},
			Spec: addOnTest1ConfigSpecBeta,
		}
		_, err = hubAddonClient.AddonV1beta1().AddOnDeploymentConfigs(configDefaultNamespace).Create(
			ctx, config1, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		config2 := &addonapiv1beta1.AddOnDeploymentConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "config-second",
				Namespace: configDefaultNamespace,
			},
			Spec: addOnTest2ConfigSpecBeta,
		}
		_, err = hubAddonClient.AddonV1beta1().AddOnDeploymentConfigs(configDefaultNamespace).Create(
			ctx, config2, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		// Patch CMA to use a placement strategy with 2 configs of the same GVK.
		// The ordering config-first → config-second must be preserved.
		cma, err := hubAddonClient.AddonV1beta1().ClusterManagementAddOns().Get(
			ctx, testAddOnConfigsImpl.name, metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())

		cma.Spec.InstallStrategy = addonapiv1beta1.InstallStrategy{
			Type: addonapiv1beta1.AddonInstallStrategyPlacements,
			Placements: []addonapiv1beta1.PlacementStrategy{
				{
					PlacementRef: addonapiv1beta1.PlacementRef{
						Name:      "test-placement",
						Namespace: configDefaultNamespace,
					},
					Configs: []addonapiv1beta1.AddOnConfig{
						{
							ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
								Group:    addOnDeploymentConfigGVR.Group,
								Resource: addOnDeploymentConfigGVR.Resource,
							},
							ConfigReferent: addonapiv1beta1.ConfigReferent{
								Namespace: configDefaultNamespace,
								Name:      "config-first",
							},
						},
						{
							ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
								Group:    addOnDeploymentConfigGVR.Group,
								Resource: addOnDeploymentConfigGVR.Resource,
							},
							ConfigReferent: addonapiv1beta1.ConfigReferent{
								Namespace: configDefaultNamespace,
								Name:      "config-second",
							},
						},
					},
					RolloutStrategy: clusterv1alpha1.RolloutStrategy{
						Type: clusterv1alpha1.All,
					},
				},
			},
		}
		patchClusterManagementAddOnBeta(ctx, cma)

		// Create placement and placement decision so the cluster is selected.
		placement := &clusterv1beta1.Placement{
			ObjectMeta: metav1.ObjectMeta{Name: "test-placement", Namespace: configDefaultNamespace},
		}
		_, err = hubClusterClient.ClusterV1beta1().Placements(configDefaultNamespace).Create(
			ctx, placement, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

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
		decision, err = hubClusterClient.ClusterV1beta1().PlacementDecisions(configDefaultNamespace).Create(
			ctx, decision, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		decision.Status.Decisions = []clusterv1beta1.ClusterDecision{
			{ClusterName: managedClusterName},
		}
		_, err = hubClusterClient.ClusterV1beta1().PlacementDecisions(configDefaultNamespace).UpdateStatus(
			ctx, decision, metav1.UpdateOptions{})
		Expect(err).ToNot(HaveOccurred())

		// CMA status: InstallProgressions should have both configs in spec order (config-first → config-second).
		assertClusterManagementAddOnInstallProgressionBeta(ctx, testAddOnConfigsImpl.name, addonapiv1beta1.InstallProgression{
			PlacementRef: addonapiv1beta1.PlacementRef{Name: "test-placement", Namespace: configDefaultNamespace},
			ConfigReferences: []addonapiv1beta1.InstallConfigReference{
				{
					ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
						Group:    addOnDeploymentConfigGVR.Group,
						Resource: addOnDeploymentConfigGVR.Resource,
					},
					DesiredConfig: &addonapiv1beta1.ConfigSpecHash{
						ConfigReferent: addonapiv1beta1.ConfigReferent{
							Namespace: configDefaultNamespace,
							Name:      "config-first",
						},
						SpecHash: addOnTest1ConfigSpecHash,
					},
				},
				{
					ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
						Group:    addOnDeploymentConfigGVR.Group,
						Resource: addOnDeploymentConfigGVR.Resource,
					},
					DesiredConfig: &addonapiv1beta1.ConfigSpecHash{
						ConfigReferent: addonapiv1beta1.ConfigReferent{
							Namespace: configDefaultNamespace,
							Name:      "config-second",
						},
						SpecHash: addOnTest2ConfigSpecHash,
					},
				},
			},
		})

		// MCA status: ConfigReferences should reflect the same order (config-first → config-second).
		assertManagedClusterAddOnConfigReferencesBeta(ctx, testAddOnConfigsImpl.name, managedClusterName,
			addonapiv1beta1.ConfigReference{
				ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
					Group:    addOnDeploymentConfigGVR.Group,
					Resource: addOnDeploymentConfigGVR.Resource,
				},
				LastObservedGeneration: 1,
				DesiredConfig: &addonapiv1beta1.ConfigSpecHash{
					ConfigReferent: addonapiv1beta1.ConfigReferent{
						Namespace: configDefaultNamespace,
						Name:      "config-first",
					},
					SpecHash: addOnTest1ConfigSpecHash,
				},
			},
			addonapiv1beta1.ConfigReference{
				ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
					Group:    addOnDeploymentConfigGVR.Group,
					Resource: addOnDeploymentConfigGVR.Resource,
				},
				LastObservedGeneration: 1,
				DesiredConfig: &addonapiv1beta1.ConfigSpecHash{
					ConfigReferent: addonapiv1beta1.ConfigReferent{
						Namespace: configDefaultNamespace,
						Name:      "config-second",
					},
					SpecHash: addOnTest2ConfigSpecHash,
				},
			},
		)
	})
})
