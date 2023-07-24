package integration

import (
	"context"
	"fmt"

	ginkgo "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/rand"

	"open-cluster-management.io/addon-framework/pkg/addonmanager/constants"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	workapiv1 "open-cluster-management.io/api/work/v1"
)

const (
	upgradeDeploymentJson = `{
		"apiVersion": "apps/v1",
		"kind": "Deployment",
		"metadata": {
			"name": "nginx-deployment",
			"namespace": "default"
		},
		"spec": {
			"replicas": 1,
			"selector": {
				"matchLabels": {
					"app": "nginx"
				}
			},
			"template": {
				"metadata": {
					"creationTimestamp": null,
					"labels": {
						"app": "nginx"
					}
				},
				"spec": {
					"containers": [
						{
							"image": "nginx:1.14.2",
							"name": "nginx",
							"ports": [
								{
									"containerPort": 80,
									"protocol": "TCP"
								}
							]
						}
					]
				}
			}
		}
	}`
)

var _ = ginkgo.Describe("Addon upgrade", func() {
	var configDefaultNamespace string
	var configDefaultName string
	var configUpdateName string
	var placementName string
	var placementNamespace string
	var manifestWorkName string
	var clusterNames []string
	var suffix string
	var err error
	var cma *addonapiv1alpha1.ClusterManagementAddOn

	ginkgo.BeforeEach(func() {
		suffix = rand.String(5)
		configDefaultNamespace = fmt.Sprintf("default-config-%s", suffix)
		configDefaultName = fmt.Sprintf("default-config-%s", suffix)
		configUpdateName = fmt.Sprintf("update-config-%s", suffix)
		placementName = fmt.Sprintf("ns-%s", suffix)
		placementNamespace = fmt.Sprintf("ns-%s", suffix)
		manifestWorkName = fmt.Sprintf("%s-0", constants.DeployWorkNamePrefix(testAddOnConfigsImpl.name))

		// prepare cma
		cma = &addonapiv1alpha1.ClusterManagementAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name: testAddOnConfigsImpl.name,
				Annotations: map[string]string{
					addonapiv1alpha1.AddonLifecycleAnnotationKey: addonapiv1alpha1.AddonLifecycleAddonManagerAnnotationValue,
				},
			},
			Spec: addonapiv1alpha1.ClusterManagementAddOnSpec{
				InstallStrategy: addonapiv1alpha1.InstallStrategy{
					Type: addonapiv1alpha1.AddonInstallStrategyPlacements,
					Placements: []addonapiv1alpha1.PlacementStrategy{
						{
							PlacementRef: addonapiv1alpha1.PlacementRef{Name: placementName, Namespace: placementNamespace},
							RolloutStrategy: addonapiv1alpha1.RolloutStrategy{
								Type: addonapiv1alpha1.AddonRolloutStrategyUpdateAll,
							},
							Configs: []addonapiv1alpha1.AddOnConfig{
								{
									ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
										Group:    addOnDeploymentConfigGVR.Group,
										Resource: addOnDeploymentConfigGVR.Resource,
									},
									ConfigReferent: addonapiv1alpha1.ConfigReferent{
										Namespace: configDefaultNamespace,
										Name:      configDefaultName,
									},
								},
							},
						},
					},
				},
			},
		}
		_, err := hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().Create(context.Background(), cma, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		// prepare cluster
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

		// prepare manifestwork obj
		for i := 0; i < 4; i++ {
			obj := &unstructured.Unstructured{}
			err := obj.UnmarshalJSON([]byte(upgradeDeploymentJson))
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			testAddOnConfigsImpl.manifests[clusterNames[i]] = []runtime.Object{obj}
		}

		// prepare placement
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: placementNamespace}}
		_, err = hubKubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		placement := &clusterv1beta1.Placement{ObjectMeta: metav1.ObjectMeta{Name: placementName, Namespace: placementNamespace}}
		_, err = hubClusterClient.ClusterV1beta1().Placements(placementNamespace).Create(context.Background(), placement, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		decision := &clusterv1beta1.PlacementDecision{
			ObjectMeta: metav1.ObjectMeta{
				Name:      placementName,
				Namespace: placementNamespace,
				Labels:    map[string]string{clusterv1beta1.PlacementLabel: placementName},
			},
		}
		decision, err = hubClusterClient.ClusterV1beta1().PlacementDecisions(placementNamespace).Create(context.Background(), decision, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		decision.Status.Decisions = []clusterv1beta1.ClusterDecision{
			{ClusterName: clusterNames[0]},
			{ClusterName: clusterNames[1]},
			{ClusterName: clusterNames[2]},
			{ClusterName: clusterNames[3]},
		}
		_, err = hubClusterClient.ClusterV1beta1().PlacementDecisions(placementNamespace).UpdateStatus(context.Background(), decision, metav1.UpdateOptions{})
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
			Spec: addOnDefaultConfigSpec,
		}
		_, err = hubAddonClient.AddonV1alpha1().AddOnDeploymentConfigs(configDefaultNamespace).Create(
			context.Background(), addOnDefaultConfig, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		// prepare update config
		addOnUpdateConfig := &addonapiv1alpha1.AddOnDeploymentConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configUpdateName,
				Namespace: configDefaultNamespace,
			},
			Spec: addOnTest2ConfigSpec,
		}
		_, err = hubAddonClient.AddonV1alpha1().AddOnDeploymentConfigs(configDefaultNamespace).Create(context.Background(), addOnUpdateConfig, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	ginkgo.AfterEach(func() {
		err = hubKubeClient.CoreV1().Namespaces().Delete(context.Background(), configDefaultNamespace, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		err = hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().Delete(context.Background(), testAddOnConfigsImpl.name, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		for _, managedClusterName := range clusterNames {
			err = hubKubeClient.CoreV1().Namespaces().Delete(context.Background(), managedClusterName, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			err = hubClusterClient.ClusterV1().ManagedClusters().Delete(context.Background(), managedClusterName, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			delete(testAddOnConfigsImpl.registrations, managedClusterName)
		}
	})

	ginkgo.Context("Addon rollout strategy", func() {
		ginkgo.It("Should update when config changes", func() {
			ginkgo.By("fresh install")
			ginkgo.By("check work")
			gomega.Eventually(func() error {
				for i := 0; i < 4; i++ {
					work, err := hubWorkClient.WorkV1().ManifestWorks(clusterNames[i]).Get(
						context.Background(), manifestWorkName, metav1.GetOptions{})
					if err != nil {
						return err
					}

					if len(work.Annotations) == 0 {
						return fmt.Errorf("Unexpected number of work annotations %v", work.Annotations)
					}
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			ginkgo.By("update work status to trigger addon status")
			for i := 0; i < 4; i++ {
				work, err := hubWorkClient.WorkV1().ManifestWorks(clusterNames[i]).Get(context.Background(), manifestWorkName, metav1.GetOptions{})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				meta.SetStatusCondition(
					&work.Status.Conditions,
					metav1.Condition{Type: workapiv1.WorkApplied, Status: metav1.ConditionTrue, Reason: "WorkApplied", ObservedGeneration: work.Generation})
				meta.SetStatusCondition(
					&work.Status.Conditions,
					metav1.Condition{Type: workapiv1.WorkAvailable, Status: metav1.ConditionTrue, Reason: "WorkAvailable", ObservedGeneration: work.Generation})
				_, err = hubWorkClient.WorkV1().ManifestWorks(clusterNames[i]).UpdateStatus(context.Background(), work, metav1.UpdateOptions{})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
			}

			ginkgo.By("check mca status")
			for i := 0; i < 4; i++ {
				assertManagedClusterAddOnConfigReferences(testAddOnConfigsImpl.name, clusterNames[i], addonapiv1alpha1.ConfigReference{
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
					LastAppliedConfig: &addonapiv1alpha1.ConfigSpecHash{
						ConfigReferent: addonapiv1alpha1.ConfigReferent{
							Namespace: configDefaultNamespace,
							Name:      configDefaultName,
						},
						SpecHash: addOnDefaultConfigSpecHash,
					},
				})
				assertManagedClusterAddOnConditions(testAddOnConfigsImpl.name, clusterNames[i], metav1.Condition{
					Type:    addonapiv1alpha1.ManagedClusterAddOnConditionProgressing,
					Status:  metav1.ConditionFalse,
					Reason:  addonapiv1alpha1.ProgressingReasonInstallSucceed,
					Message: "install completed with no errors.",
				})
			}

			ginkgo.By("check cma status")
			assertClusterManagementAddOnInstallProgression(testAddOnConfigsImpl.name, addonapiv1alpha1.InstallProgression{
				PlacementRef: addonapiv1alpha1.PlacementRef{Name: placementNamespace, Namespace: placementNamespace},
				ConfigReferences: []addonapiv1alpha1.InstallConfigReference{
					{
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
						LastAppliedConfig: &addonapiv1alpha1.ConfigSpecHash{
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Namespace: configDefaultNamespace,
								Name:      configDefaultName,
							},
							SpecHash: addOnDefaultConfigSpecHash,
						},
						LastKnownGoodConfig: &addonapiv1alpha1.ConfigSpecHash{
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Namespace: configDefaultNamespace,
								Name:      configDefaultName,
							},
							SpecHash: addOnDefaultConfigSpecHash,
						},
					},
				},
			})
			assertClusterManagementAddOnConditions(testAddOnConfigsImpl.name, metav1.Condition{
				Type:    addonapiv1alpha1.ManagedClusterAddOnConditionProgressing,
				Status:  metav1.ConditionFalse,
				Reason:  addonapiv1alpha1.ProgressingReasonInstallSucceed,
				Message: "4/4 install completed with no errors.",
			})

			ginkgo.By("update all")
			ginkgo.By("upgrade configs to test1")
			addOnConfig, err := hubAddonClient.AddonV1alpha1().AddOnDeploymentConfigs(configDefaultNamespace).Get(
				context.Background(), configDefaultName, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			addOnConfig.Spec = addOnTest1ConfigSpec
			_, err = hubAddonClient.AddonV1alpha1().AddOnDeploymentConfigs(configDefaultNamespace).Update(
				context.Background(), addOnConfig, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			ginkgo.By("check mca status")
			for i := 0; i < 4; i++ {
				assertManagedClusterAddOnConfigReferences(testAddOnConfigsImpl.name, clusterNames[i], addonapiv1alpha1.ConfigReference{
					ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
						Group:    addOnDeploymentConfigGVR.Group,
						Resource: addOnDeploymentConfigGVR.Resource,
					},
					ConfigReferent: addonapiv1alpha1.ConfigReferent{
						Namespace: configDefaultNamespace,
						Name:      configDefaultName,
					},
					LastObservedGeneration: 2,
					DesiredConfig: &addonapiv1alpha1.ConfigSpecHash{
						ConfigReferent: addonapiv1alpha1.ConfigReferent{
							Namespace: configDefaultNamespace,
							Name:      configDefaultName,
						},
						SpecHash: addOnTest1ConfigSpecHash,
					},
					LastAppliedConfig: &addonapiv1alpha1.ConfigSpecHash{
						ConfigReferent: addonapiv1alpha1.ConfigReferent{
							Namespace: configDefaultNamespace,
							Name:      configDefaultName,
						},
						SpecHash: addOnTest1ConfigSpecHash,
					},
				})
				assertManagedClusterAddOnConditions(testAddOnConfigsImpl.name, clusterNames[i], metav1.Condition{
					Type:    addonapiv1alpha1.ManagedClusterAddOnConditionProgressing,
					Status:  metav1.ConditionFalse,
					Reason:  addonapiv1alpha1.ProgressingReasonUpgradeSucceed,
					Message: "upgrade completed with no errors.",
				})
			}

			ginkgo.By("check cma status")
			assertClusterManagementAddOnInstallProgression(testAddOnConfigsImpl.name, addonapiv1alpha1.InstallProgression{
				PlacementRef: addonapiv1alpha1.PlacementRef{Name: placementNamespace, Namespace: placementNamespace},
				ConfigReferences: []addonapiv1alpha1.InstallConfigReference{
					{
						ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
							Group:    addOnDeploymentConfigGVR.Group,
							Resource: addOnDeploymentConfigGVR.Resource,
						},
						DesiredConfig: &addonapiv1alpha1.ConfigSpecHash{
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Namespace: configDefaultNamespace,
								Name:      configDefaultName,
							},
							SpecHash: addOnTest1ConfigSpecHash,
						},
						LastAppliedConfig: &addonapiv1alpha1.ConfigSpecHash{
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Namespace: configDefaultNamespace,
								Name:      configDefaultName,
							},
							SpecHash: addOnTest1ConfigSpecHash,
						},
						LastKnownGoodConfig: &addonapiv1alpha1.ConfigSpecHash{
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Namespace: configDefaultNamespace,
								Name:      configDefaultName,
							},
							SpecHash: addOnTest1ConfigSpecHash,
						},
					},
				},
			})
			assertClusterManagementAddOnConditions(testAddOnConfigsImpl.name, metav1.Condition{
				Type:    addonapiv1alpha1.ManagedClusterAddOnConditionProgressing,
				Status:  metav1.ConditionFalse,
				Reason:  addonapiv1alpha1.ProgressingReasonUpgradeSucceed,
				Message: "4/4 upgrade completed with no errors.",
			})

			ginkgo.By("update work status to avoid addon status update")
			gomega.Eventually(func() error {
				for i := 0; i < 4; i++ {
					work, err := hubWorkClient.WorkV1().ManifestWorks(clusterNames[i]).Get(context.Background(), manifestWorkName, metav1.GetOptions{})
					if err != nil {
						return err
					}
					meta.SetStatusCondition(
						&work.Status.Conditions,
						metav1.Condition{Type: workapiv1.WorkApplied, Status: metav1.ConditionFalse, Reason: "WorkApplied", ObservedGeneration: work.Generation})
					meta.SetStatusCondition(
						&work.Status.Conditions,
						metav1.Condition{Type: workapiv1.WorkAvailable, Status: metav1.ConditionFalse, Reason: "WorkAvailable", ObservedGeneration: work.Generation})
					_, err = hubWorkClient.WorkV1().ManifestWorks(clusterNames[i]).UpdateStatus(context.Background(), work, metav1.UpdateOptions{})
					if err != nil {
						return err
					}
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			ginkgo.By("rolling upgrade")
			ginkgo.By("update cma to rolling update")
			cma, err = hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().Get(context.Background(), cma.Name, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			cma.Spec.InstallStrategy.Placements[0].RolloutStrategy.Type = addonapiv1alpha1.AddonRolloutStrategyRollingUpdate
			cma.Spec.InstallStrategy.Placements[0].RolloutStrategy.RollingUpdate = &addonapiv1alpha1.RollingUpdate{MaxConcurrency: intstr.FromString("50%")}
			cma.Spec.InstallStrategy.Placements[0].Configs[0].ConfigReferent = addonapiv1alpha1.ConfigReferent{Namespace: configDefaultNamespace, Name: configUpdateName}
			_, err = hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().Update(context.Background(), cma, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			ginkgo.By("check mca status")
			for i := 0; i < 2; i++ {
				assertManagedClusterAddOnConfigReferences(testAddOnConfigsImpl.name, clusterNames[i], addonapiv1alpha1.ConfigReference{
					ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
						Group:    addOnDeploymentConfigGVR.Group,
						Resource: addOnDeploymentConfigGVR.Resource,
					},
					ConfigReferent: addonapiv1alpha1.ConfigReferent{
						Namespace: configDefaultNamespace,
						Name:      configUpdateName,
					},
					LastObservedGeneration: 1,
					DesiredConfig: &addonapiv1alpha1.ConfigSpecHash{
						ConfigReferent: addonapiv1alpha1.ConfigReferent{
							Namespace: configDefaultNamespace,
							Name:      configUpdateName,
						},
						SpecHash: addOnTest2ConfigSpecHash,
					},
					LastAppliedConfig: &addonapiv1alpha1.ConfigSpecHash{
						ConfigReferent: addonapiv1alpha1.ConfigReferent{
							Namespace: configDefaultNamespace,
							Name:      configDefaultName,
						},
						SpecHash: addOnTest1ConfigSpecHash,
					},
				})
				assertManagedClusterAddOnConditions(testAddOnConfigsImpl.name, clusterNames[i], metav1.Condition{
					Type:    addonapiv1alpha1.ManagedClusterAddOnConditionProgressing,
					Status:  metav1.ConditionTrue,
					Reason:  addonapiv1alpha1.ProgressingReasonUpgrading,
					Message: "upgrading... work is not ready",
				})
			}
			for i := 2; i < 4; i++ {
				assertManagedClusterAddOnConfigReferences(testAddOnConfigsImpl.name, clusterNames[i], addonapiv1alpha1.ConfigReference{
					ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
						Group:    addOnDeploymentConfigGVR.Group,
						Resource: addOnDeploymentConfigGVR.Resource,
					},
					ConfigReferent: addonapiv1alpha1.ConfigReferent{
						Namespace: configDefaultNamespace,
						Name:      configDefaultName,
					},
					LastObservedGeneration: 2,
					DesiredConfig: &addonapiv1alpha1.ConfigSpecHash{
						ConfigReferent: addonapiv1alpha1.ConfigReferent{
							Namespace: configDefaultNamespace,
							Name:      configDefaultName,
						},
						SpecHash: addOnTest1ConfigSpecHash,
					},
					LastAppliedConfig: &addonapiv1alpha1.ConfigSpecHash{
						ConfigReferent: addonapiv1alpha1.ConfigReferent{
							Namespace: configDefaultNamespace,
							Name:      configDefaultName,
						},
						SpecHash: addOnTest1ConfigSpecHash,
					},
				})
				assertManagedClusterAddOnConditions(testAddOnConfigsImpl.name, clusterNames[i], metav1.Condition{
					Type:    addonapiv1alpha1.ManagedClusterAddOnConditionProgressing,
					Status:  metav1.ConditionFalse,
					Reason:  addonapiv1alpha1.ProgressingReasonUpgradeSucceed,
					Message: "upgrade completed with no errors.",
				})
			}

			ginkgo.By("check cma status")
			assertClusterManagementAddOnInstallProgression(testAddOnConfigsImpl.name, addonapiv1alpha1.InstallProgression{
				PlacementRef: addonapiv1alpha1.PlacementRef{Name: placementNamespace, Namespace: placementNamespace},
				ConfigReferences: []addonapiv1alpha1.InstallConfigReference{
					{
						ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
							Group:    addOnDeploymentConfigGVR.Group,
							Resource: addOnDeploymentConfigGVR.Resource,
						},
						DesiredConfig: &addonapiv1alpha1.ConfigSpecHash{
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Namespace: configDefaultNamespace,
								Name:      configUpdateName,
							},
							SpecHash: addOnTest2ConfigSpecHash,
						},
						LastAppliedConfig: &addonapiv1alpha1.ConfigSpecHash{
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Namespace: configDefaultNamespace,
								Name:      configDefaultName,
							},
							SpecHash: addOnTest1ConfigSpecHash,
						},
						LastKnownGoodConfig: &addonapiv1alpha1.ConfigSpecHash{
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Namespace: configDefaultNamespace,
								Name:      configDefaultName,
							},
							SpecHash: addOnTest1ConfigSpecHash,
						},
					},
				},
			})
			assertClusterManagementAddOnConditions(testAddOnConfigsImpl.name, metav1.Condition{
				Type:    addonapiv1alpha1.ManagedClusterAddOnConditionProgressing,
				Status:  metav1.ConditionTrue,
				Reason:  addonapiv1alpha1.ProgressingReasonUpgrading,
				Message: "2/4 upgrading...",
			})

			ginkgo.By("update 2 work status to trigger addon status")
			gomega.Eventually(func() error {
				for i := 0; i < 2; i++ {
					work, err := hubWorkClient.WorkV1().ManifestWorks(clusterNames[i]).Get(context.Background(), manifestWorkName, metav1.GetOptions{})
					if err != nil {
						return err
					}
					meta.SetStatusCondition(
						&work.Status.Conditions,
						metav1.Condition{Type: workapiv1.WorkApplied, Status: metav1.ConditionTrue, Reason: "WorkApplied", ObservedGeneration: work.Generation})
					meta.SetStatusCondition(
						&work.Status.Conditions,
						metav1.Condition{Type: workapiv1.WorkAvailable, Status: metav1.ConditionTrue, Reason: "WorkAvailable", ObservedGeneration: work.Generation})
					_, err = hubWorkClient.WorkV1().ManifestWorks(clusterNames[i]).UpdateStatus(context.Background(), work, metav1.UpdateOptions{})
					if err != nil {
						return err
					}
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			ginkgo.By("check mca status")
			for i := 0; i < 2; i++ {
				assertManagedClusterAddOnConfigReferences(testAddOnConfigsImpl.name, clusterNames[i], addonapiv1alpha1.ConfigReference{
					ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
						Group:    addOnDeploymentConfigGVR.Group,
						Resource: addOnDeploymentConfigGVR.Resource,
					},
					ConfigReferent: addonapiv1alpha1.ConfigReferent{
						Namespace: configDefaultNamespace,
						Name:      configUpdateName,
					},
					LastObservedGeneration: 1,
					DesiredConfig: &addonapiv1alpha1.ConfigSpecHash{
						ConfigReferent: addonapiv1alpha1.ConfigReferent{
							Namespace: configDefaultNamespace,
							Name:      configUpdateName,
						},
						SpecHash: addOnTest2ConfigSpecHash,
					},
					LastAppliedConfig: &addonapiv1alpha1.ConfigSpecHash{
						ConfigReferent: addonapiv1alpha1.ConfigReferent{
							Namespace: configDefaultNamespace,
							Name:      configUpdateName,
						},
						SpecHash: addOnTest2ConfigSpecHash,
					},
				})
				assertManagedClusterAddOnConditions(testAddOnConfigsImpl.name, clusterNames[i], metav1.Condition{
					Type:    addonapiv1alpha1.ManagedClusterAddOnConditionProgressing,
					Status:  metav1.ConditionFalse,
					Reason:  addonapiv1alpha1.ProgressingReasonUpgradeSucceed,
					Message: "upgrade completed with no errors.",
				})
			}

			ginkgo.By("check cma status")
			assertClusterManagementAddOnInstallProgression(testAddOnConfigsImpl.name, addonapiv1alpha1.InstallProgression{
				PlacementRef: addonapiv1alpha1.PlacementRef{Name: placementNamespace, Namespace: placementNamespace},
				ConfigReferences: []addonapiv1alpha1.InstallConfigReference{
					{
						ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
							Group:    addOnDeploymentConfigGVR.Group,
							Resource: addOnDeploymentConfigGVR.Resource,
						},
						DesiredConfig: &addonapiv1alpha1.ConfigSpecHash{
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Namespace: configDefaultNamespace,
								Name:      configUpdateName,
							},
							SpecHash: addOnTest2ConfigSpecHash,
						},
						LastAppliedConfig: &addonapiv1alpha1.ConfigSpecHash{
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Namespace: configDefaultNamespace,
								Name:      configDefaultName,
							},
							SpecHash: addOnTest1ConfigSpecHash,
						},
						LastKnownGoodConfig: &addonapiv1alpha1.ConfigSpecHash{
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Namespace: configDefaultNamespace,
								Name:      configDefaultName,
							},
							SpecHash: addOnTest1ConfigSpecHash,
						},
					},
				},
			})
			assertClusterManagementAddOnConditions(testAddOnConfigsImpl.name, metav1.Condition{
				Type:    addonapiv1alpha1.ManagedClusterAddOnConditionProgressing,
				Status:  metav1.ConditionTrue,
				Reason:  addonapiv1alpha1.ProgressingReasonUpgrading,
				Message: "4/4 upgrading...",
			})

			ginkgo.By("update another 2 work status to trigger addon status")
			gomega.Eventually(func() error {
				for i := 2; i < 4; i++ {
					work, err := hubWorkClient.WorkV1().ManifestWorks(clusterNames[i]).Get(context.Background(), manifestWorkName, metav1.GetOptions{})
					if err != nil {
						return err
					}
					meta.SetStatusCondition(
						&work.Status.Conditions,
						metav1.Condition{Type: workapiv1.WorkApplied, Status: metav1.ConditionTrue, Reason: "WorkApplied", ObservedGeneration: work.Generation})
					meta.SetStatusCondition(
						&work.Status.Conditions,
						metav1.Condition{Type: workapiv1.WorkAvailable, Status: metav1.ConditionTrue, Reason: "WorkAvailable", ObservedGeneration: work.Generation})
					_, err = hubWorkClient.WorkV1().ManifestWorks(clusterNames[i]).UpdateStatus(context.Background(), work, metav1.UpdateOptions{})
					if err != nil {
						return err
					}
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			ginkgo.By("check cma status")
			assertClusterManagementAddOnInstallProgression(testAddOnConfigsImpl.name, addonapiv1alpha1.InstallProgression{
				PlacementRef: addonapiv1alpha1.PlacementRef{Name: placementNamespace, Namespace: placementNamespace},
				ConfigReferences: []addonapiv1alpha1.InstallConfigReference{
					{
						ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
							Group:    addOnDeploymentConfigGVR.Group,
							Resource: addOnDeploymentConfigGVR.Resource,
						},
						DesiredConfig: &addonapiv1alpha1.ConfigSpecHash{
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Namespace: configDefaultNamespace,
								Name:      configUpdateName,
							},
							SpecHash: addOnTest2ConfigSpecHash,
						},
						LastAppliedConfig: &addonapiv1alpha1.ConfigSpecHash{
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Namespace: configDefaultNamespace,
								Name:      configUpdateName,
							},
							SpecHash: addOnTest2ConfigSpecHash,
						},
						LastKnownGoodConfig: &addonapiv1alpha1.ConfigSpecHash{
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Namespace: configDefaultNamespace,
								Name:      configUpdateName,
							},
							SpecHash: addOnTest2ConfigSpecHash,
						},
					},
				},
			})
			assertClusterManagementAddOnConditions(testAddOnConfigsImpl.name, metav1.Condition{
				Type:    addonapiv1alpha1.ManagedClusterAddOnConditionProgressing,
				Status:  metav1.ConditionFalse,
				Reason:  addonapiv1alpha1.ProgressingReasonUpgradeSucceed,
				Message: "4/4 upgrade completed with no errors.",
			})
		})
	})
})
