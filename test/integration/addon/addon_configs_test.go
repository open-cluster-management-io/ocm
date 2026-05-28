package integration

import (
	"context"
	"fmt"

	ginkgo "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	addonapiv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
)

var _ = ginkgo.Describe("AddConfigs Beta", func() {
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
		_, err = createClusterManagementAddOnBeta(testAddOnConfigsImpl.name, configDefaultNamespace, configDefaultName)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		assertClusterManagementAddOnAnnotationsBeta(testAddOnConfigsImpl.name)

		// prepare default config
		configDefaultNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: configDefaultNamespace}}
		_, err = hubKubeClient.CoreV1().Namespaces().Create(context.Background(), configDefaultNS, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		addOnDefaultConfig := &addonapiv1beta1.AddOnDeploymentConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configDefaultName,
				Namespace: configDefaultNamespace,
			},
			Spec: addOnDefaultConfigSpecBeta,
		}
		_, err = hubAddonClient.AddonV1beta1().AddOnDeploymentConfigs(configDefaultNamespace).Create(
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
		err = hubAddonClient.AddonV1beta1().ClusterManagementAddOns().Delete(context.Background(), testAddOnConfigsImpl.name, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		delete(testAddOnConfigsImpl.registrations, managedClusterName)
	})

	ginkgo.It("Should use default config", func() {
		addon := &addonapiv1beta1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testAddOnConfigsImpl.name,
				Namespace: managedClusterName,
			},
			Spec: addonapiv1beta1.ManagedClusterAddOnSpec{},
		}
		_, err = hubAddonClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).Create(context.Background(), addon, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		// check cma status
		assertClusterManagementAddOnDefaultConfigReferencesBeta(testAddOnConfigsImpl.name, addonapiv1beta1.DefaultConfigReference{
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
		assertClusterManagementAddOnInstallProgressionBeta(testAddOnConfigsImpl.name)

		// check mca status
		assertManagedClusterAddOnConfigReferencesBeta(testAddOnConfigsImpl.name, managedClusterName, addonapiv1beta1.ConfigReference{
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

	ginkgo.It("Should override default config by install strategy", func() {
		cma, err := hubAddonClient.AddonV1beta1().ClusterManagementAddOns().Get(context.Background(), testAddOnConfigsImpl.name, metav1.GetOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

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
		patchClusterManagementAddOnBeta(context.Background(), cma)

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
		assertClusterManagementAddOnDefaultConfigReferencesBeta(testAddOnConfigsImpl.name, addonapiv1beta1.DefaultConfigReference{
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
		assertClusterManagementAddOnInstallProgressionBeta(testAddOnConfigsImpl.name, addonapiv1beta1.InstallProgression{
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
		assertManagedClusterAddOnConfigReferencesBeta(testAddOnConfigsImpl.name, managedClusterName, addonapiv1beta1.ConfigReference{
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

	ginkgo.It("Should override default config", func() {
		addOnConfig := &addonapiv1beta1.AddOnDeploymentConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "addon-config",
				Namespace: managedClusterName,
			},
			Spec: addOnTest1ConfigSpecBeta,
		}
		_, err = hubAddonClient.AddonV1beta1().AddOnDeploymentConfigs(managedClusterName).Create(context.Background(), addOnConfig, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

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
		_, err = hubAddonClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).Create(context.Background(), addon, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		addon.Status = addonapiv1beta1.ManagedClusterAddOnStatus{
			SupportedConfigs: []addonapiv1beta1.ConfigGroupResource{
				{
					Group:    addOnDeploymentConfigGVR.Group,
					Resource: addOnDeploymentConfigGVR.Resource,
				},
			},
		}
		updateManagedClusterAddOnStatusBeta(context.Background(), addon)

		// check cma status
		assertClusterManagementAddOnDefaultConfigReferencesBeta(testAddOnConfigsImpl.name, addonapiv1beta1.DefaultConfigReference{
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
		assertClusterManagementAddOnInstallProgressionBeta(testAddOnConfigsImpl.name)

		// check mca status
		assertManagedClusterAddOnConfigReferencesBeta(testAddOnConfigsImpl.name, managedClusterName, addonapiv1beta1.ConfigReference{
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

	ginkgo.It("Should update config spec successfully", func() {
		addOnConfig := &addonapiv1beta1.AddOnDeploymentConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "addon-config",
				Namespace: managedClusterName,
			},
			Spec: addOnTest1ConfigSpecBeta,
		}
		_, err = hubAddonClient.AddonV1beta1().AddOnDeploymentConfigs(managedClusterName).Create(context.Background(), addOnConfig, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

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
		addon, err = hubAddonClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).Create(context.Background(), addon, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		addon.Status = addonapiv1beta1.ManagedClusterAddOnStatus{
			SupportedConfigs: []addonapiv1beta1.ConfigGroupResource{
				{
					Group:    addOnDeploymentConfigGVR.Group,
					Resource: addOnDeploymentConfigGVR.Resource,
				},
			},
		}
		updateManagedClusterAddOnStatusBeta(context.Background(), addon)

		// check mca status
		assertManagedClusterAddOnConfigReferencesBeta(testAddOnConfigsImpl.name, managedClusterName, addonapiv1beta1.ConfigReference{
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

		addOnConfig, err = hubAddonClient.AddonV1beta1().AddOnDeploymentConfigs(managedClusterName).Get(context.Background(), addOnConfig.Name, metav1.GetOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		addOnConfig.Spec = addOnTest2ConfigSpecBeta
		_, err = hubAddonClient.AddonV1beta1().AddOnDeploymentConfigs(managedClusterName).Update(context.Background(), addOnConfig, metav1.UpdateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		// check mca status
		assertManagedClusterAddOnConfigReferencesBeta(testAddOnConfigsImpl.name, managedClusterName, addonapiv1beta1.ConfigReference{
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

	ginkgo.It("Should not update unsupported config spec hash", func() {
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
		_, err = hubAddonClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).Create(context.Background(), addon, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		// check mca status
		assertManagedClusterAddOnConfigReferencesBeta(testAddOnConfigsImpl.name, managedClusterName, addonapiv1beta1.ConfigReference{
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
})
