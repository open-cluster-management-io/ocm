package integration

import (
	"context"
	"fmt"

	apiequality "k8s.io/apimachinery/pkg/api/equality"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/rand"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

const addOnDefaultConfigSpecHash = "287d774850847584cc3ebd8b72e2ad3ef8ac6c31803a59324943a7f94054b08a"
const addOnTest1ConfigSpecHash = "d76dad0a6448910652950163cc4324e4616ab5143046555c5ad5b003a622ab8d"
const addOnTest2ConfigSpecHash = "0e23290bf8414508e3eee63431ece7b4d988a65ffe7e11727e1374d8277abb63"

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

		// prepare default config
		configDefaultNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: configDefaultNamespace}}
		_, err = hubKubeClient.CoreV1().Namespaces().Create(context.Background(), configDefaultNS, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		addOnDefaultConfig := &addonapiv1alpha1.AddOnDeploymentConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configDefaultName,
				Namespace: configDefaultNamespace,
			},
			Spec: addonapiv1alpha1.AddOnDeploymentConfigSpec{
				CustomizedVariables: []addonapiv1alpha1.CustomizedVariable{
					{
						Name:  "test",
						Value: "test",
					},
				},
			},
		}
		_, err = hubAddonClient.AddonV1alpha1().AddOnDeploymentConfigs(configDefaultNamespace).Create(context.Background(), addOnDefaultConfig, metav1.CreateOptions{})
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
					RolloutStrategy: addonapiv1alpha1.RolloutStrategy{
						Type: addonapiv1alpha1.AddonRolloutStrategyUpdateAll,
					},
				},
			},
		}
		updateClusterManagementAddOn(context.Background(), cma)

		placement := &clusterv1beta1.Placement{ObjectMeta: metav1.ObjectMeta{Name: "test-placement", Namespace: configDefaultNamespace}}
		_, err = hubClusterClient.ClusterV1beta1().Placements(configDefaultNamespace).Create(context.Background(), placement, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		decision := &clusterv1beta1.PlacementDecision{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-placement",
				Namespace: configDefaultNamespace,
				Labels:    map[string]string{clusterv1beta1.PlacementLabel: "test-placement"},
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
			Spec: addonapiv1alpha1.AddOnDeploymentConfigSpec{
				CustomizedVariables: []addonapiv1alpha1.CustomizedVariable{
					{
						Name:  "test1",
						Value: "test1",
					},
				},
			},
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
			Spec: addonapiv1alpha1.AddOnDeploymentConfigSpec{
				CustomizedVariables: []addonapiv1alpha1.CustomizedVariable{
					{
						Name:  "test1",
						Value: "test1",
					},
				},
			},
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

		addOnConfig.Spec.CustomizedVariables = append(addOnConfig.Spec.CustomizedVariables, addonapiv1alpha1.CustomizedVariable{
			Name:  "test2",
			Value: "test2",
		})
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
		addOnConfig := &addonapiv1alpha1.AddOnDeploymentConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "addon-config",
				Namespace: managedClusterName,
			},
			Spec: addonapiv1alpha1.AddOnDeploymentConfigSpec{
				CustomizedVariables: []addonapiv1alpha1.CustomizedVariable{
					{
						Name:  "test1",
						Value: "test1",
					},
				},
			},
		}
		_, err = hubAddonClient.AddonV1alpha1().AddOnDeploymentConfigs(managedClusterName).Create(context.Background(), addOnConfig, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

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
				SpecHash: "",
			},
		})
	})
})

func createClusterManagementAddOn(name, defaultConfigNamespace, defaultConfigName string) (*addonapiv1alpha1.ClusterManagementAddOn, error) {
	clusterManagementAddon, err := hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().Get(context.Background(), name, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		clusterManagementAddon, err = hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().Create(
			context.Background(),
			&addonapiv1alpha1.ClusterManagementAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name: name,
				},
				Spec: addonapiv1alpha1.ClusterManagementAddOnSpec{
					SupportedConfigs: []addonapiv1alpha1.ConfigMeta{
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    addOnDeploymentConfigGVR.Group,
								Resource: addOnDeploymentConfigGVR.Resource,
							},
							DefaultConfig: &addonapiv1alpha1.ConfigReferent{
								Name:      defaultConfigName,
								Namespace: defaultConfigNamespace,
							},
						},
					},
					InstallStrategy: addonapiv1alpha1.InstallStrategy{
						Type: addonapiv1alpha1.AddonInstallStrategyManual,
					},
				},
			},
			metav1.CreateOptions{},
		)
		if err != nil {
			return nil, err
		}
		return clusterManagementAddon, nil
	}

	if err != nil {
		return nil, err
	}

	return clusterManagementAddon, nil
}

func updateClusterManagementAddOn(ctx context.Context, new *addonapiv1alpha1.ClusterManagementAddOn) {
	gomega.Eventually(func() bool {
		old, err := hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().Get(context.Background(), new.Name, metav1.GetOptions{})
		old.Spec = new.Spec
		_, err = hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().Update(context.Background(), old, metav1.UpdateOptions{})
		if err == nil {
			return true
		}
		return false
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
}

func updateManagedClusterAddOnStatus(ctx context.Context, new *addonapiv1alpha1.ManagedClusterAddOn) {
	gomega.Eventually(func() bool {
		old, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(new.Namespace).Get(context.Background(), new.Name, metav1.GetOptions{})
		old.Status = new.Status
		_, err = hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(old.Namespace).UpdateStatus(context.Background(), old, metav1.UpdateOptions{})
		if err == nil {
			return true
		}
		return false
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
}

func assertClusterManagementAddOnDefaultConfigReferences(name string, expect ...addonapiv1alpha1.DefaultConfigReference) {
	ginkgo.By(fmt.Sprintf("Check ClusterManagementAddOn %s DefaultConfigReferences", name))

	gomega.Eventually(func() error {
		actual, err := hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().Get(context.Background(), name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		if len(actual.Status.DefaultConfigReferences) != len(expect) {
			return fmt.Errorf("Expected %v default config reference, actual: %v", len(expect), len(actual.Status.DefaultConfigReferences))
		}

		for i, e := range expect {
			actualConfigReference := actual.Status.DefaultConfigReferences[i]

			if !apiequality.Semantic.DeepEqual(actualConfigReference, e) {
				return fmt.Errorf("Expected default config is %v, actual: %v", e, actualConfigReference)
			}
		}

		return nil
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
}

func assertClusterManagementAddOnInstallProgression(name string, expect ...addonapiv1alpha1.InstallProgression) {
	ginkgo.By(fmt.Sprintf("Check ClusterManagementAddOn %s InstallProgression", name))

	gomega.Eventually(func() error {
		actual, err := hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().Get(context.Background(), name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		if len(actual.Status.InstallProgressions) != len(expect) {
			return fmt.Errorf("Expected %v install progression, actual: %v", len(expect), len(actual.Status.InstallProgressions))
		}

		for i, e := range expect {
			actualInstallProgression := actual.Status.InstallProgressions[i]

			if !apiequality.Semantic.DeepEqual(actualInstallProgression, e) {
				return fmt.Errorf("Expected default config is %v, actual: %v", e, actualInstallProgression)
			}
		}

		return nil
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
}

func assertManagedClusterAddOnConfigReferences(name, namespace string, expect ...addonapiv1alpha1.ConfigReference) {
	ginkgo.By(fmt.Sprintf("Check ManagedClusterAddOn %s ConfigReferences", name))

	gomega.Eventually(func() error {
		actual, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(namespace).Get(context.Background(), name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		if len(actual.Status.ConfigReferences) != len(expect) {
			return fmt.Errorf("Expected %v config reference, actual: %v", len(expect), len(actual.Status.ConfigReferences))
		}

		for i, e := range expect {
			actualConfigReference := actual.Status.ConfigReferences[i]

			if !apiequality.Semantic.DeepEqual(actualConfigReference, e) {
				return fmt.Errorf("Expected config reference is %v, actual: %v", e, actualConfigReference)
			}
		}

		return nil
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
}
