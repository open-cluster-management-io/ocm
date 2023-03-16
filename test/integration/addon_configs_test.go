package integration

import (
	"context"
	"fmt"

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

		gomega.Eventually(func() error {
			actual, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), testAddOnConfigsImpl.name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if len(actual.Status.ConfigReferences) != 1 {
				return fmt.Errorf("Expected 1 config reference, actual: %v", actual.Status.ConfigReferences)
			}

			configReference := actual.Status.ConfigReferences[0]

			if configReference.ConfigReferent.Name != configDefaultName {
				return fmt.Errorf("Expected default config is used, actual: %v", actual.Status.ConfigReferences)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	})

	ginkgo.It("Should override default config by install strategy", func() {
		cma, err := hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().Get(context.Background(), testAddOnConfigsImpl.name, metav1.GetOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		cma.Spec.InstallStrategy = addonapiv1alpha1.InstallStrategy{
			Type: addonapiv1alpha1.AddonInstallStrategyManualPlacements,
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
				},
			},
		}
		_, err = hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().Update(context.Background(), cma, metav1.UpdateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

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

		gomega.Eventually(func() error {
			actual, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), testAddOnConfigsImpl.name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if len(actual.Status.ConfigReferences) != 1 {
				return fmt.Errorf("Expected 1 config reference, actual: %v", actual.Status.ConfigReferences)
			}

			configReference := actual.Status.ConfigReferences[0]

			if configReference.ConfigReferent.Name != "another-config" {
				return fmt.Errorf("Expected config is overridden, actual: %v", actual.Status.ConfigReferences)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
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

		gomega.Eventually(func() error {
			actual, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), testAddOnConfigsImpl.name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if len(actual.Status.ConfigReferences) != 1 {
				return fmt.Errorf("Expected 1 config reference, actual: %v", actual.Status.ConfigReferences)
			}

			configReference := actual.Status.ConfigReferences[0]

			if configReference.ConfigReferent.Name != addOnConfig.Name {
				return fmt.Errorf("Expected default config is overrided, actual: %v", actual.Status.ConfigReferences)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	})

	ginkgo.It("Should update config generation successfully", func() {
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

		gomega.Eventually(func() error {
			actual, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), testAddOnConfigsImpl.name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if len(actual.Status.ConfigReferences) != 1 {
				return fmt.Errorf("Expected 1 config reference, actual: %v", actual.Status.ConfigReferences)
			}

			configReference := actual.Status.ConfigReferences[0]

			if configReference.LastObservedGeneration != 1 {
				return fmt.Errorf("Expected last observed generation is 1, actual: %v", actual.Status.ConfigReferences)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		addOnConfig, err = hubAddonClient.AddonV1alpha1().AddOnDeploymentConfigs(managedClusterName).Get(context.Background(), addOnConfig.Name, metav1.GetOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		addOnConfig.Spec.CustomizedVariables = append(addOnConfig.Spec.CustomizedVariables, addonapiv1alpha1.CustomizedVariable{
			Name:  "test2",
			Value: "test2",
		})
		_, err = hubAddonClient.AddonV1alpha1().AddOnDeploymentConfigs(managedClusterName).Update(context.Background(), addOnConfig, metav1.UpdateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			actual, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), testAddOnConfigsImpl.name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if len(actual.Status.ConfigReferences) != 1 {
				return fmt.Errorf("Expected 1 config reference, actual: %v", actual.Status.ConfigReferences)
			}

			configReference := actual.Status.ConfigReferences[0]

			if configReference.LastObservedGeneration != 2 {
				return fmt.Errorf("Expected last observed generation is 2, actual: %v", actual.Status.ConfigReferences)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
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
