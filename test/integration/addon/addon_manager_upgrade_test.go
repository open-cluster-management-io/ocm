package integration

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/rand"

	"open-cluster-management.io/addon-framework/pkg/addonmanager/constants"
	addonapiv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	clusterv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
)

const (
	upgradeDeploymentJsonBeta = `{
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

var _ = Describe("Addon upgrade Beta", func() {
	var configDefaultNamespace string
	var configDefaultName string
	var configUpdateName string
	var placementName string
	var placementNamespace string
	var manifestWorkName string
	var clusterNames []string
	var suffix string
	var err error
	var cma *addonapiv1beta1.ClusterManagementAddOn

	BeforeEach(func(ctx SpecContext) {
		clusterNames = nil
		suffix = rand.String(5)
		configDefaultNamespace = fmt.Sprintf("default-config-%s", suffix)
		configDefaultName = fmt.Sprintf("default-config-%s", suffix)
		configUpdateName = fmt.Sprintf("update-config-%s", suffix)
		placementName = fmt.Sprintf("ns-%s", suffix)
		placementNamespace = fmt.Sprintf("ns-%s", suffix)
		manifestWorkName = fmt.Sprintf("%s-0", constants.DeployWorkNamePrefix(testAddOnConfigsImpl.name))

		// prepare cma
		cma = &addonapiv1beta1.ClusterManagementAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name: testAddOnConfigsImpl.name,
			},
			Spec: addonapiv1beta1.ClusterManagementAddOnSpec{
				InstallStrategy: addonapiv1beta1.InstallStrategy{
					Type: addonapiv1beta1.AddonInstallStrategyPlacements,
					Placements: []addonapiv1beta1.PlacementStrategy{
						{
							PlacementRef: addonapiv1beta1.PlacementRef{Name: placementName, Namespace: placementNamespace},
							RolloutStrategy: clusterv1alpha1.RolloutStrategy{
								Type: clusterv1alpha1.All,
							},
							Configs: []addonapiv1beta1.AddOnConfig{
								{
									ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
										Group:    addOnDeploymentConfigGVR.Group,
										Resource: addOnDeploymentConfigGVR.Resource,
									},
									ConfigReferent: addonapiv1beta1.ConfigReferent{
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
		_, err := hubAddonClient.AddonV1beta1().ClusterManagementAddOns().Create(ctx, cma, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		assertClusterManagementAddOnAnnotationsBeta(ctx, testAddOnConfigsImpl.name)

		// prepare cluster
		for i := 0; i < 4; i++ {
			managedClusterName := fmt.Sprintf("managedcluster-%s-%d", suffix, i)
			clusterNames = append(clusterNames, managedClusterName)
			err = createManagedCluster(ctx, hubClusterClient, managedClusterName)
			Expect(err).ToNot(HaveOccurred())
		}

		// prepare manifestwork obj
		for i := 0; i < 4; i++ {
			obj := &unstructured.Unstructured{}
			err := obj.UnmarshalJSON([]byte(upgradeDeploymentJsonBeta))
			Expect(err).ToNot(HaveOccurred())
			testAddOnConfigsImpl.manifests[clusterNames[i]] = []runtime.Object{obj}
		}

		// prepare placement
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: placementNamespace}}
		_, err = hubKubeClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		placement := &clusterv1beta1.Placement{ObjectMeta: metav1.ObjectMeta{Name: placementName, Namespace: placementNamespace}}
		_, err = hubClusterClient.ClusterV1beta1().Placements(placementNamespace).Create(ctx, placement, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		// prepare placement decisions
		err = createPlacementDecision(ctx, hubClusterClient, placementNamespace, placementName, "0", clusterNames[0], clusterNames[1])
		Expect(err).ToNot(HaveOccurred())
		err = createPlacementDecision(ctx, hubClusterClient, placementNamespace, placementName, "1", clusterNames[2], clusterNames[3])
		Expect(err).ToNot(HaveOccurred())

		// prepare default config
		configDefaultNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: configDefaultNamespace}}
		_, err = hubKubeClient.CoreV1().Namespaces().Create(ctx, configDefaultNS, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		err = createAddOnDeploymentConfigBeta(ctx, hubAddonClient, configDefaultNamespace, configDefaultName, addOnDefaultConfigSpecBeta)
		Expect(err).ToNot(HaveOccurred())

		// prepare update config
		err = createAddOnDeploymentConfigBeta(ctx, hubAddonClient, configDefaultNamespace, configUpdateName, addOnTest2ConfigSpecBeta)
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func(ctx SpecContext) {
		err = hubKubeClient.CoreV1().Namespaces().Delete(ctx, configDefaultNamespace, metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())
		err = hubAddonClient.AddonV1beta1().ClusterManagementAddOns().Delete(ctx, testAddOnConfigsImpl.name, metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())
		for _, managedClusterName := range clusterNames {
			err = hubKubeClient.CoreV1().Namespaces().Delete(ctx, managedClusterName, metav1.DeleteOptions{})
			Expect(err).ToNot(HaveOccurred())
			err = hubClusterClient.ClusterV1().ManagedClusters().Delete(ctx, managedClusterName, metav1.DeleteOptions{})
			Expect(err).ToNot(HaveOccurred())
			delete(testAddOnConfigsImpl.registrations, managedClusterName)
		}
	})

	Context("Addon rollout strategy", func() {
		It("Should update when config changes", func(ctx SpecContext) {
			By("fresh install")
			By("check work")
			Eventually(func() error {
				for i := 0; i < 4; i++ {
					work, err := hubWorkClient.WorkV1().ManifestWorks(clusterNames[i]).Get(
						ctx, manifestWorkName, metav1.GetOptions{})
					if err != nil {
						return err
					}

					if len(work.Annotations) == 0 {
						return fmt.Errorf("Unexpected number of work annotations %v", work.Annotations)
					}
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

			By("update work status to trigger addon status")
			for i := 0; i < 4; i++ {
				updateManifestWorkStatus(ctx, hubWorkClient, clusterNames[i], manifestWorkName, metav1.ConditionTrue)
			}

			By("check mca status")
			for i := 0; i < 4; i++ {
				assertManagedClusterAddOnConfigReferencesBeta(ctx, testAddOnConfigsImpl.name, clusterNames[i], addonapiv1beta1.ConfigReference{
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
					LastAppliedConfig: &addonapiv1beta1.ConfigSpecHash{
						ConfigReferent: addonapiv1beta1.ConfigReferent{
							Namespace: configDefaultNamespace,
							Name:      configDefaultName,
						},
						SpecHash: addOnDefaultConfigSpecHash,
					},
				})
				assertManagedClusterAddOnConditionsBeta(ctx, testAddOnConfigsImpl.name, clusterNames[i], metav1.Condition{
					Type:    addonapiv1beta1.ManagedClusterAddOnConditionProgressing,
					Status:  metav1.ConditionFalse,
					Reason:  addonapiv1beta1.ProgressingReasonCompleted,
					Message: "completed with no errors.",
				})
				assertManagedClusterAddOnConditionsBeta(ctx, testAddOnConfigsImpl.name, clusterNames[i], metav1.Condition{
					Type:    addonapiv1beta1.ManagedClusterAddOnConditionConfigured,
					Status:  metav1.ConditionTrue,
					Reason:  "ConfigurationsConfigured",
					Message: "Configurations configured",
				})
			}

			By("check cma status")
			assertClusterManagementAddOnInstallProgressionBeta(ctx, testAddOnConfigsImpl.name, addonapiv1beta1.InstallProgression{
				PlacementRef: addonapiv1beta1.PlacementRef{Name: placementNamespace, Namespace: placementNamespace},
				ConfigReferences: []addonapiv1beta1.InstallConfigReference{
					{
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
						LastAppliedConfig: &addonapiv1beta1.ConfigSpecHash{
							ConfigReferent: addonapiv1beta1.ConfigReferent{
								Namespace: configDefaultNamespace,
								Name:      configDefaultName,
							},
							SpecHash: addOnDefaultConfigSpecHash,
						},
						LastKnownGoodConfig: &addonapiv1beta1.ConfigSpecHash{
							ConfigReferent: addonapiv1beta1.ConfigReferent{
								Namespace: configDefaultNamespace,
								Name:      configDefaultName,
							},
							SpecHash: addOnDefaultConfigSpecHash,
						},
					},
				},
			})
			assertClusterManagementAddOnConditionsBeta(ctx, testAddOnConfigsImpl.name, metav1.Condition{
				Type:    addonapiv1beta1.ManagedClusterAddOnConditionProgressing,
				Status:  metav1.ConditionFalse,
				Reason:  addonapiv1beta1.ProgressingReasonCompleted,
				Message: "selected clusters 4. configured addons 4/4 completed with no errors, 0 failed 0 timeout.",
			})

			By("update all")
			By("upgrade configs to test1")
			updateAddOnDeploymentConfigSpecBeta(ctx, hubAddonClient, configDefaultNamespace, configDefaultName, addOnTest1ConfigSpecBeta)

			By("check mca status")
			for i := 0; i < 4; i++ {
				assertManagedClusterAddOnConfigReferencesBeta(ctx, testAddOnConfigsImpl.name, clusterNames[i], addonapiv1beta1.ConfigReference{
					ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
						Group:    addOnDeploymentConfigGVR.Group,
						Resource: addOnDeploymentConfigGVR.Resource,
					},
					LastObservedGeneration: 2,
					DesiredConfig: &addonapiv1beta1.ConfigSpecHash{
						ConfigReferent: addonapiv1beta1.ConfigReferent{
							Namespace: configDefaultNamespace,
							Name:      configDefaultName,
						},
						SpecHash: addOnTest1ConfigSpecHash,
					},
					LastAppliedConfig: &addonapiv1beta1.ConfigSpecHash{
						ConfigReferent: addonapiv1beta1.ConfigReferent{
							Namespace: configDefaultNamespace,
							Name:      configDefaultName,
						},
						SpecHash: addOnTest1ConfigSpecHash,
					},
				})
				assertManagedClusterAddOnConditionsBeta(ctx, testAddOnConfigsImpl.name, clusterNames[i], metav1.Condition{
					Type:    addonapiv1beta1.ManagedClusterAddOnConditionProgressing,
					Status:  metav1.ConditionFalse,
					Reason:  addonapiv1beta1.ProgressingReasonCompleted,
					Message: "completed with no errors.",
				})
				assertManagedClusterAddOnConditionsBeta(ctx, testAddOnConfigsImpl.name, clusterNames[i], metav1.Condition{
					Type:    addonapiv1beta1.ManagedClusterAddOnConditionConfigured,
					Status:  metav1.ConditionTrue,
					Reason:  "ConfigurationsConfigured",
					Message: "Configurations configured",
				})
			}

			By("check cma status")
			assertClusterManagementAddOnInstallProgressionBeta(ctx, testAddOnConfigsImpl.name, addonapiv1beta1.InstallProgression{
				PlacementRef: addonapiv1beta1.PlacementRef{Name: placementNamespace, Namespace: placementNamespace},
				ConfigReferences: []addonapiv1beta1.InstallConfigReference{
					{
						ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
							Group:    addOnDeploymentConfigGVR.Group,
							Resource: addOnDeploymentConfigGVR.Resource,
						},
						DesiredConfig: &addonapiv1beta1.ConfigSpecHash{
							ConfigReferent: addonapiv1beta1.ConfigReferent{
								Namespace: configDefaultNamespace,
								Name:      configDefaultName,
							},
							SpecHash: addOnTest1ConfigSpecHash,
						},
						LastAppliedConfig: &addonapiv1beta1.ConfigSpecHash{
							ConfigReferent: addonapiv1beta1.ConfigReferent{
								Namespace: configDefaultNamespace,
								Name:      configDefaultName,
							},
							SpecHash: addOnTest1ConfigSpecHash,
						},
						LastKnownGoodConfig: &addonapiv1beta1.ConfigSpecHash{
							ConfigReferent: addonapiv1beta1.ConfigReferent{
								Namespace: configDefaultNamespace,
								Name:      configDefaultName,
							},
							SpecHash: addOnTest1ConfigSpecHash,
						},
					},
				},
			})
			assertClusterManagementAddOnConditionsBeta(ctx, testAddOnConfigsImpl.name, metav1.Condition{
				Type:    addonapiv1beta1.ManagedClusterAddOnConditionProgressing,
				Status:  metav1.ConditionFalse,
				Reason:  addonapiv1beta1.ProgressingReasonCompleted,
				Message: "selected clusters 4. configured addons 4/4 completed with no errors, 0 failed 0 timeout.",
			})

			By("update work status to avoid addon status update")
			// Capture start BEFORE updating work status. When the work status changes to False,
			// the controller will set the addon Progressing condition with a LastTransitionTime.
			// The rollout SDK uses that LastTransitionTime (not the CMA patch time) as the base
			// for ProgressDeadline timeout calculation. If we capture start after the work update,
			// the controller's timeout can fire before our expected 5s duration from start.
			start := metav1.Now()
			for i := 0; i < 2; i++ {
				updateManifestWorkStatus(ctx, hubWorkClient, clusterNames[i], manifestWorkName, metav1.ConditionFalse)
			}

			By("rolling upgrade per cluster with ProgressDeadline and MaxFailures")
			By("update cma to rolling update")
			cma, err = hubAddonClient.AddonV1beta1().ClusterManagementAddOns().Get(ctx, cma.Name, metav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())
			cma.Spec.InstallStrategy.Placements[0].RolloutStrategy = clusterv1alpha1.RolloutStrategy{
				Type: clusterv1alpha1.Progressive,
				Progressive: &clusterv1alpha1.RolloutProgressive{
					MaxConcurrency: intstr.FromInt(2),
					RolloutConfig: clusterv1alpha1.RolloutConfig{
						ProgressDeadline: "5s",
						MaxFailures:      intstr.FromInt32(1),
					}},
			}
			cma.Spec.InstallStrategy.Placements[0].Configs[0].ConfigReferent = addonapiv1beta1.ConfigReferent{Namespace: configDefaultNamespace, Name: configUpdateName}
			patchClusterManagementAddOnBeta(ctx, cma)

			By("check mca status")
			for i := 0; i < 2; i++ {
				assertManagedClusterAddOnConfigReferencesBeta(ctx, testAddOnConfigsImpl.name, clusterNames[i], addonapiv1beta1.ConfigReference{
					ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
						Group:    addOnDeploymentConfigGVR.Group,
						Resource: addOnDeploymentConfigGVR.Resource,
					},
					LastObservedGeneration: 1,
					DesiredConfig: &addonapiv1beta1.ConfigSpecHash{
						ConfigReferent: addonapiv1beta1.ConfigReferent{
							Namespace: configDefaultNamespace,
							Name:      configUpdateName,
						},
						SpecHash: addOnTest2ConfigSpecHash,
					},
				})
				assertManagedClusterAddOnConditionsBeta(ctx, testAddOnConfigsImpl.name, clusterNames[i], metav1.Condition{
					Type:    addonapiv1beta1.ManagedClusterAddOnConditionProgressing,
					Status:  metav1.ConditionTrue,
					Reason:  addonapiv1beta1.ProgressingReasonProgressing,
					Message: "progressing... work is not ready",
				})
				assertManagedClusterAddOnConditionsBeta(ctx, testAddOnConfigsImpl.name, clusterNames[i], metav1.Condition{
					Type:    addonapiv1beta1.ManagedClusterAddOnConditionConfigured,
					Status:  metav1.ConditionTrue,
					Reason:  "ConfigurationsConfigured",
					Message: "Configurations configured",
				})
			}
			for i := 2; i < 4; i++ {
				assertManagedClusterAddOnConfigReferencesBeta(ctx, testAddOnConfigsImpl.name, clusterNames[i], addonapiv1beta1.ConfigReference{
					ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
						Group:    addOnDeploymentConfigGVR.Group,
						Resource: addOnDeploymentConfigGVR.Resource,
					},
					LastObservedGeneration: 2,
					DesiredConfig: &addonapiv1beta1.ConfigSpecHash{
						ConfigReferent: addonapiv1beta1.ConfigReferent{
							Namespace: configDefaultNamespace,
							Name:      configDefaultName,
						},
						SpecHash: addOnTest1ConfigSpecHash,
					},
					LastAppliedConfig: &addonapiv1beta1.ConfigSpecHash{
						ConfigReferent: addonapiv1beta1.ConfigReferent{
							Namespace: configDefaultNamespace,
							Name:      configDefaultName,
						},
						SpecHash: addOnTest1ConfigSpecHash,
					},
				})
				assertManagedClusterAddOnConditionsBeta(ctx, testAddOnConfigsImpl.name, clusterNames[i], metav1.Condition{
					Type:    addonapiv1beta1.ManagedClusterAddOnConditionProgressing,
					Status:  metav1.ConditionFalse,
					Reason:  addonapiv1beta1.ProgressingReasonCompleted,
					Message: "completed with no errors.",
				})
				assertManagedClusterAddOnConditionsBeta(ctx, testAddOnConfigsImpl.name, clusterNames[i], metav1.Condition{
					Type:    addonapiv1beta1.ManagedClusterAddOnConditionConfigured,
					Status:  metav1.ConditionFalse,
					Reason:  "ConfigurationsNotConfigured",
					Message: "Configurations updated and not configured yet",
				})
			}

			By("check cma status")
			assertClusterManagementAddOnInstallProgressionBeta(ctx, testAddOnConfigsImpl.name, addonapiv1beta1.InstallProgression{
				PlacementRef: addonapiv1beta1.PlacementRef{Name: placementNamespace, Namespace: placementNamespace},
				ConfigReferences: []addonapiv1beta1.InstallConfigReference{
					{
						ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
							Group:    addOnDeploymentConfigGVR.Group,
							Resource: addOnDeploymentConfigGVR.Resource,
						},
						DesiredConfig: &addonapiv1beta1.ConfigSpecHash{
							ConfigReferent: addonapiv1beta1.ConfigReferent{
								Namespace: configDefaultNamespace,
								Name:      configUpdateName,
							},
							SpecHash: addOnTest2ConfigSpecHash,
						},
					},
				},
			})
			assertClusterManagementAddOnConditionsBeta(ctx, testAddOnConfigsImpl.name, metav1.Condition{
				Type:    addonapiv1beta1.ManagedClusterAddOnConditionProgressing,
				Status:  metav1.ConditionTrue,
				Reason:  addonapiv1beta1.ProgressingReasonProgressing,
				Message: "selected clusters 4. configured addons 2/4 progressing..., 0 failed 0 timeout.",
			})

			By("timeout after ProgressDeadline 5s and stop rollout since breach MaxFailures 1")
			assertClusterManagementAddOnNoConditionsBeta(ctx, testAddOnConfigsImpl.name, start, 5*time.Second, metav1.Condition{
				Type:    addonapiv1beta1.ManagedClusterAddOnConditionProgressing,
				Status:  metav1.ConditionTrue,
				Reason:  addonapiv1beta1.ProgressingReasonProgressing,
				Message: "selected clusters 4. configured addons 0/4 progressing..., 0 failed 2 timeout.",
			})
			assertClusterManagementAddOnConditionsBeta(ctx, testAddOnConfigsImpl.name, metav1.Condition{
				Type:    addonapiv1beta1.ManagedClusterAddOnConditionProgressing,
				Status:  metav1.ConditionTrue,
				Reason:  addonapiv1beta1.ProgressingReasonProgressing,
				Message: "selected clusters 4. configured addons 0/4 progressing..., 0 failed 2 timeout.",
			})

			By("update timeouted work status to continue rollout since within MaxFailures 1")
			for i := 0; i < 2; i++ {
				updateManifestWorkStatus(ctx, hubWorkClient, clusterNames[i], manifestWorkName, metav1.ConditionTrue)
			}
			for i := 3; i < 4; i++ {
				updateManifestWorkStatus(ctx, hubWorkClient, clusterNames[i], manifestWorkName, metav1.ConditionFalse)
			}

			By("check mca status")
			for i := 0; i < 2; i++ {
				assertManagedClusterAddOnConfigReferencesBeta(ctx, testAddOnConfigsImpl.name, clusterNames[i], addonapiv1beta1.ConfigReference{
					ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
						Group:    addOnDeploymentConfigGVR.Group,
						Resource: addOnDeploymentConfigGVR.Resource,
					},
					LastObservedGeneration: 1,
					DesiredConfig: &addonapiv1beta1.ConfigSpecHash{
						ConfigReferent: addonapiv1beta1.ConfigReferent{
							Namespace: configDefaultNamespace,
							Name:      configUpdateName,
						},
						SpecHash: addOnTest2ConfigSpecHash,
					},
					LastAppliedConfig: &addonapiv1beta1.ConfigSpecHash{
						ConfigReferent: addonapiv1beta1.ConfigReferent{
							Namespace: configDefaultNamespace,
							Name:      configUpdateName,
						},
						SpecHash: addOnTest2ConfigSpecHash,
					},
				})
				assertManagedClusterAddOnConditionsBeta(ctx, testAddOnConfigsImpl.name, clusterNames[i], metav1.Condition{
					Type:    addonapiv1beta1.ManagedClusterAddOnConditionProgressing,
					Status:  metav1.ConditionFalse,
					Reason:  addonapiv1beta1.ProgressingReasonCompleted,
					Message: "completed with no errors.",
				})
				assertManagedClusterAddOnConditionsBeta(ctx, testAddOnConfigsImpl.name, clusterNames[i], metav1.Condition{
					Type:    addonapiv1beta1.ManagedClusterAddOnConditionConfigured,
					Status:  metav1.ConditionTrue,
					Reason:  "ConfigurationsConfigured",
					Message: "Configurations configured",
				})
			}

			By("check cma status")
			assertClusterManagementAddOnConditionsBeta(ctx, testAddOnConfigsImpl.name, metav1.Condition{
				Type:    addonapiv1beta1.ManagedClusterAddOnConditionProgressing,
				Status:  metav1.ConditionTrue,
				Reason:  addonapiv1beta1.ProgressingReasonProgressing,
				Message: "selected clusters 4. configured addons 4/4 progressing..., 0 failed 0 timeout.",
			})

			By("update another 2 work status to trigger addon status")
			for i := 2; i < 4; i++ {
				updateManifestWorkStatus(ctx, hubWorkClient, clusterNames[i], manifestWorkName, metav1.ConditionTrue)
			}

			By("check mca status")
			for i := 2; i < 4; i++ {
				assertManagedClusterAddOnConfigReferencesBeta(ctx, testAddOnConfigsImpl.name, clusterNames[i], addonapiv1beta1.ConfigReference{
					ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
						Group:    addOnDeploymentConfigGVR.Group,
						Resource: addOnDeploymentConfigGVR.Resource,
					},
					LastObservedGeneration: 1,
					DesiredConfig: &addonapiv1beta1.ConfigSpecHash{
						ConfigReferent: addonapiv1beta1.ConfigReferent{
							Namespace: configDefaultNamespace,
							Name:      configUpdateName,
						},
						SpecHash: addOnTest2ConfigSpecHash,
					},
					LastAppliedConfig: &addonapiv1beta1.ConfigSpecHash{
						ConfigReferent: addonapiv1beta1.ConfigReferent{
							Namespace: configDefaultNamespace,
							Name:      configUpdateName,
						},
						SpecHash: addOnTest2ConfigSpecHash,
					},
				})
				assertManagedClusterAddOnConditionsBeta(ctx, testAddOnConfigsImpl.name, clusterNames[i], metav1.Condition{
					Type:    addonapiv1beta1.ManagedClusterAddOnConditionProgressing,
					Status:  metav1.ConditionFalse,
					Reason:  addonapiv1beta1.ProgressingReasonCompleted,
					Message: "completed with no errors.",
				})
				assertManagedClusterAddOnConditionsBeta(ctx, testAddOnConfigsImpl.name, clusterNames[i], metav1.Condition{
					Type:    addonapiv1beta1.ManagedClusterAddOnConditionConfigured,
					Status:  metav1.ConditionTrue,
					Reason:  "ConfigurationsConfigured",
					Message: "Configurations configured",
				})
			}
			By("check cma status")
			assertClusterManagementAddOnInstallProgressionBeta(ctx, testAddOnConfigsImpl.name, addonapiv1beta1.InstallProgression{
				PlacementRef: addonapiv1beta1.PlacementRef{Name: placementNamespace, Namespace: placementNamespace},
				ConfigReferences: []addonapiv1beta1.InstallConfigReference{
					{
						ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
							Group:    addOnDeploymentConfigGVR.Group,
							Resource: addOnDeploymentConfigGVR.Resource,
						},
						DesiredConfig: &addonapiv1beta1.ConfigSpecHash{
							ConfigReferent: addonapiv1beta1.ConfigReferent{
								Namespace: configDefaultNamespace,
								Name:      configUpdateName,
							},
							SpecHash: addOnTest2ConfigSpecHash,
						},
						LastAppliedConfig: &addonapiv1beta1.ConfigSpecHash{
							ConfigReferent: addonapiv1beta1.ConfigReferent{
								Namespace: configDefaultNamespace,
								Name:      configUpdateName,
							},
							SpecHash: addOnTest2ConfigSpecHash,
						},
						LastKnownGoodConfig: &addonapiv1beta1.ConfigSpecHash{
							ConfigReferent: addonapiv1beta1.ConfigReferent{
								Namespace: configDefaultNamespace,
								Name:      configUpdateName,
							},
							SpecHash: addOnTest2ConfigSpecHash,
						},
					},
				},
			})
			assertClusterManagementAddOnConditionsBeta(ctx, testAddOnConfigsImpl.name, metav1.Condition{
				Type:    addonapiv1beta1.ManagedClusterAddOnConditionProgressing,
				Status:  metav1.ConditionFalse,
				Reason:  addonapiv1beta1.ProgressingReasonCompleted,
				Message: "selected clusters 4. configured addons 4/4 completed with no errors, 0 failed 0 timeout.",
			})

			By("update work status to avoid addon status update")
			for i := 0; i < 2; i++ {
				updateManifestWorkStatus(ctx, hubWorkClient, clusterNames[i], manifestWorkName, metav1.ConditionFalse)
			}

			By("rolling upgrade per group with MinSuccessTime")
			By("update cma to rolling update per group")
			cma, err = hubAddonClient.AddonV1beta1().ClusterManagementAddOns().Get(ctx, cma.Name, metav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())
			cma.Spec.InstallStrategy.Placements[0].RolloutStrategy = clusterv1alpha1.RolloutStrategy{
				Type: clusterv1alpha1.ProgressivePerGroup,
				ProgressivePerGroup: &clusterv1alpha1.RolloutProgressivePerGroup{
					RolloutConfig: clusterv1alpha1.RolloutConfig{
						MinSuccessTime: metav1.Duration{
							Duration: 3 * time.Second,
						},
					}},
			}
			patchClusterManagementAddOnBeta(ctx, cma)

			By("upgrade configs to test3")
			updateAddOnDeploymentConfigSpecBeta(ctx, hubAddonClient, configDefaultNamespace, configUpdateName, addOnTest3ConfigSpecBeta)

			By("check mca status")
			for i := 0; i < 2; i++ {
				assertManagedClusterAddOnConfigReferencesBeta(ctx, testAddOnConfigsImpl.name, clusterNames[i], addonapiv1beta1.ConfigReference{
					ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
						Group:    addOnDeploymentConfigGVR.Group,
						Resource: addOnDeploymentConfigGVR.Resource,
					},
					LastObservedGeneration: 2,
					DesiredConfig: &addonapiv1beta1.ConfigSpecHash{
						ConfigReferent: addonapiv1beta1.ConfigReferent{
							Namespace: configDefaultNamespace,
							Name:      configUpdateName,
						},
						SpecHash: addOnTest3ConfigSpecHash,
					},
					LastAppliedConfig: &addonapiv1beta1.ConfigSpecHash{
						ConfigReferent: addonapiv1beta1.ConfigReferent{
							Namespace: configDefaultNamespace,
							Name:      configUpdateName,
						},
						SpecHash: addOnTest2ConfigSpecHash,
					},
				})
				assertManagedClusterAddOnConditionsBeta(ctx, testAddOnConfigsImpl.name, clusterNames[i], metav1.Condition{
					Type:    addonapiv1beta1.ManagedClusterAddOnConditionProgressing,
					Status:  metav1.ConditionTrue,
					Reason:  addonapiv1beta1.ProgressingReasonProgressing,
					Message: "progressing... work is not ready",
				})
				assertManagedClusterAddOnConditionsBeta(ctx, testAddOnConfigsImpl.name, clusterNames[i], metav1.Condition{
					Type:    addonapiv1beta1.ManagedClusterAddOnConditionConfigured,
					Status:  metav1.ConditionTrue,
					Reason:  "ConfigurationsConfigured",
					Message: "Configurations configured",
				})
			}
			for i := 2; i < 4; i++ {
				assertManagedClusterAddOnConfigReferencesBeta(ctx, testAddOnConfigsImpl.name, clusterNames[i], addonapiv1beta1.ConfigReference{
					ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
						Group:    addOnDeploymentConfigGVR.Group,
						Resource: addOnDeploymentConfigGVR.Resource,
					},
					LastObservedGeneration: 2,
					DesiredConfig: &addonapiv1beta1.ConfigSpecHash{
						ConfigReferent: addonapiv1beta1.ConfigReferent{
							Namespace: configDefaultNamespace,
							Name:      configUpdateName,
						},
						SpecHash: addOnTest2ConfigSpecHash,
					},
					LastAppliedConfig: &addonapiv1beta1.ConfigSpecHash{
						ConfigReferent: addonapiv1beta1.ConfigReferent{
							Namespace: configDefaultNamespace,
							Name:      configUpdateName,
						},
						SpecHash: addOnTest2ConfigSpecHash,
					},
				})
				assertManagedClusterAddOnConditionsBeta(ctx, testAddOnConfigsImpl.name, clusterNames[i], metav1.Condition{
					Type:    addonapiv1beta1.ManagedClusterAddOnConditionProgressing,
					Status:  metav1.ConditionFalse,
					Reason:  addonapiv1beta1.ProgressingReasonCompleted,
					Message: "completed with no errors.",
				})
				assertManagedClusterAddOnConditionsBeta(ctx, testAddOnConfigsImpl.name, clusterNames[i], metav1.Condition{
					Type:    addonapiv1beta1.ManagedClusterAddOnConditionConfigured,
					Status:  metav1.ConditionFalse,
					Reason:  "ConfigurationsNotConfigured",
					Message: "Configurations updated and not configured yet",
				})
			}

			By("check cma status")
			assertClusterManagementAddOnInstallProgressionBeta(ctx, testAddOnConfigsImpl.name, addonapiv1beta1.InstallProgression{
				PlacementRef: addonapiv1beta1.PlacementRef{Name: placementNamespace, Namespace: placementNamespace},
				ConfigReferences: []addonapiv1beta1.InstallConfigReference{
					{
						ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
							Group:    addOnDeploymentConfigGVR.Group,
							Resource: addOnDeploymentConfigGVR.Resource,
						},
						DesiredConfig: &addonapiv1beta1.ConfigSpecHash{
							ConfigReferent: addonapiv1beta1.ConfigReferent{
								Namespace: configDefaultNamespace,
								Name:      configUpdateName,
							},
							SpecHash: addOnTest3ConfigSpecHash,
						},
						LastAppliedConfig: &addonapiv1beta1.ConfigSpecHash{
							ConfigReferent: addonapiv1beta1.ConfigReferent{
								Namespace: configDefaultNamespace,
								Name:      configUpdateName,
							},
							SpecHash: addOnTest2ConfigSpecHash,
						},
						LastKnownGoodConfig: &addonapiv1beta1.ConfigSpecHash{
							ConfigReferent: addonapiv1beta1.ConfigReferent{
								Namespace: configDefaultNamespace,
								Name:      configUpdateName,
							},
							SpecHash: addOnTest2ConfigSpecHash,
						},
					},
				},
			})
			assertClusterManagementAddOnConditionsBeta(ctx, testAddOnConfigsImpl.name, metav1.Condition{
				Type:    addonapiv1beta1.ManagedClusterAddOnConditionProgressing,
				Status:  metav1.ConditionTrue,
				Reason:  addonapiv1beta1.ProgressingReasonProgressing,
				Message: "selected clusters 4. configured addons 2/4 progressing..., 0 failed 0 timeout.",
			})

			By("update 2 work status to trigger addon status")
			start = metav1.Now()
			for i := 0; i < 2; i++ {
				updateManifestWorkStatus(ctx, hubWorkClient, clusterNames[i], manifestWorkName, metav1.ConditionTrue)
			}
			for i := 2; i < 4; i++ {
				updateManifestWorkStatus(ctx, hubWorkClient, clusterNames[i], manifestWorkName, metav1.ConditionFalse)
			}
			assertClusterManagementAddOnNoConditionsBeta(ctx, testAddOnConfigsImpl.name, start, 3*time.Second, metav1.Condition{
				Type:    addonapiv1beta1.ManagedClusterAddOnConditionProgressing,
				Status:  metav1.ConditionTrue,
				Reason:  addonapiv1beta1.ProgressingReasonProgressing,
				Message: "selected clusters 4. configured addons 4/4 progressing..., 0 failed 0 timeout.",
			})
			assertClusterManagementAddOnConditionsBeta(ctx, testAddOnConfigsImpl.name, metav1.Condition{
				Type:    addonapiv1beta1.ManagedClusterAddOnConditionProgressing,
				Status:  metav1.ConditionTrue,
				Reason:  addonapiv1beta1.ProgressingReasonProgressing,
				Message: "selected clusters 4. configured addons 4/4 progressing..., 0 failed 0 timeout.",
			})

			By("check mca status")
			for i := 0; i < 2; i++ {
				assertManagedClusterAddOnConfigReferencesBeta(ctx, testAddOnConfigsImpl.name, clusterNames[i], addonapiv1beta1.ConfigReference{
					ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
						Group:    addOnDeploymentConfigGVR.Group,
						Resource: addOnDeploymentConfigGVR.Resource,
					},
					LastObservedGeneration: 2,
					DesiredConfig: &addonapiv1beta1.ConfigSpecHash{
						ConfigReferent: addonapiv1beta1.ConfigReferent{
							Namespace: configDefaultNamespace,
							Name:      configUpdateName,
						},
						SpecHash: addOnTest3ConfigSpecHash,
					},
					LastAppliedConfig: &addonapiv1beta1.ConfigSpecHash{
						ConfigReferent: addonapiv1beta1.ConfigReferent{
							Namespace: configDefaultNamespace,
							Name:      configUpdateName,
						},
						SpecHash: addOnTest3ConfigSpecHash,
					},
				})
				assertManagedClusterAddOnConditionsBeta(ctx, testAddOnConfigsImpl.name, clusterNames[i], metav1.Condition{
					Type:    addonapiv1beta1.ManagedClusterAddOnConditionProgressing,
					Status:  metav1.ConditionFalse,
					Reason:  addonapiv1beta1.ProgressingReasonCompleted,
					Message: "completed with no errors.",
				})
				assertManagedClusterAddOnConditionsBeta(ctx, testAddOnConfigsImpl.name, clusterNames[i], metav1.Condition{
					Type:    addonapiv1beta1.ManagedClusterAddOnConditionConfigured,
					Status:  metav1.ConditionTrue,
					Reason:  "ConfigurationsConfigured",
					Message: "Configurations configured",
				})
			}

			By("check cma status")
			assertClusterManagementAddOnInstallProgressionBeta(ctx, testAddOnConfigsImpl.name, addonapiv1beta1.InstallProgression{
				PlacementRef: addonapiv1beta1.PlacementRef{Name: placementNamespace, Namespace: placementNamespace},
				ConfigReferences: []addonapiv1beta1.InstallConfigReference{
					{
						ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
							Group:    addOnDeploymentConfigGVR.Group,
							Resource: addOnDeploymentConfigGVR.Resource,
						},
						DesiredConfig: &addonapiv1beta1.ConfigSpecHash{
							ConfigReferent: addonapiv1beta1.ConfigReferent{
								Namespace: configDefaultNamespace,
								Name:      configUpdateName,
							},
							SpecHash: addOnTest3ConfigSpecHash,
						},
						LastAppliedConfig: &addonapiv1beta1.ConfigSpecHash{
							ConfigReferent: addonapiv1beta1.ConfigReferent{
								Namespace: configDefaultNamespace,
								Name:      configUpdateName,
							},
							SpecHash: addOnTest2ConfigSpecHash,
						},
						LastKnownGoodConfig: &addonapiv1beta1.ConfigSpecHash{
							ConfigReferent: addonapiv1beta1.ConfigReferent{
								Namespace: configDefaultNamespace,
								Name:      configUpdateName,
							},
							SpecHash: addOnTest2ConfigSpecHash,
						},
					},
				},
			})

			By("update another 2 work status to trigger addon status")
			start = metav1.Now()
			for i := 2; i < 4; i++ {
				updateManifestWorkStatus(ctx, hubWorkClient, clusterNames[i], manifestWorkName, metav1.ConditionTrue)
			}
			assertClusterManagementAddOnNoConditionsBeta(ctx, testAddOnConfigsImpl.name, start, 3*time.Second, metav1.Condition{
				Type:    addonapiv1beta1.ManagedClusterAddOnConditionProgressing,
				Status:  metav1.ConditionFalse,
				Reason:  addonapiv1beta1.ProgressingReasonCompleted,
				Message: "selected clusters 4. configured addons 4/4 completed with no errors, 0 failed 0 timeout.",
			})
			assertClusterManagementAddOnConditionsBeta(ctx, testAddOnConfigsImpl.name, metav1.Condition{
				Type:    addonapiv1beta1.ManagedClusterAddOnConditionProgressing,
				Status:  metav1.ConditionFalse,
				Reason:  addonapiv1beta1.ProgressingReasonCompleted,
				Message: "selected clusters 4. configured addons 4/4 completed with no errors, 0 failed 0 timeout.",
			})

			By("check cma status")
			assertClusterManagementAddOnInstallProgressionBeta(ctx, testAddOnConfigsImpl.name, addonapiv1beta1.InstallProgression{
				PlacementRef: addonapiv1beta1.PlacementRef{Name: placementNamespace, Namespace: placementNamespace},
				ConfigReferences: []addonapiv1beta1.InstallConfigReference{
					{
						ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
							Group:    addOnDeploymentConfigGVR.Group,
							Resource: addOnDeploymentConfigGVR.Resource,
						},
						DesiredConfig: &addonapiv1beta1.ConfigSpecHash{
							ConfigReferent: addonapiv1beta1.ConfigReferent{
								Namespace: configDefaultNamespace,
								Name:      configUpdateName,
							},
							SpecHash: addOnTest3ConfigSpecHash,
						},
						LastAppliedConfig: &addonapiv1beta1.ConfigSpecHash{
							ConfigReferent: addonapiv1beta1.ConfigReferent{
								Namespace: configDefaultNamespace,
								Name:      configUpdateName,
							},
							SpecHash: addOnTest3ConfigSpecHash,
						},
						LastKnownGoodConfig: &addonapiv1beta1.ConfigSpecHash{
							ConfigReferent: addonapiv1beta1.ConfigReferent{
								Namespace: configDefaultNamespace,
								Name:      configUpdateName,
							},
							SpecHash: addOnTest3ConfigSpecHash,
						},
					},
				},
			})
		})
	})
})
