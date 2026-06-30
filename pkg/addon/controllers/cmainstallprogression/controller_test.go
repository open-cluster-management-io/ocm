package cmainstallprogression

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"

	"open-cluster-management.io/addon-framework/pkg/addonmanager/addontesting"
	"open-cluster-management.io/addon-framework/pkg/utils"
	addonv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	fakeaddon "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
)

func TestReconcile(t *testing.T) {
	cases := []struct {
		name                   string
		syncKey                string
		managedClusteraddon    []runtime.Object
		clusterManagementAddon []runtime.Object
		validateAddonActions   func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:                   "no clustermanagementaddon",
			syncKey:                "test",
			managedClusteraddon:    []runtime.Object{},
			clusterManagementAddon: []runtime.Object{},
			validateAddonActions:   addontesting.AssertNoActions,
		},
		{
			name:                   "update clustermanagementaddon status with type manual with no configs",
			syncKey:                "test",
			managedClusteraddon:    []runtime.Object{},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "testcrd", "testcr").Build()},
			validateAddonActions:   addontesting.AssertNoActions,
		},
		{
			name:                "update clustermanagementaddon status with type manual with supported configs",
			syncKey:             "test",
			managedClusteraddon: []runtime.Object{},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "testcrd", "testcr").WithDefaultConfigs(
				addonv1beta1.AddOnConfig{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addonhubconfigs",
					},
					ConfigReferent: addonv1beta1.ConfigReferent{
						Name:      "test",
						Namespace: "test",
					},
				}).Build()},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch
				cma := &addonv1beta1.ClusterManagementAddOn{}
				err := json.Unmarshal(actual, cma)
				if err != nil {
					t.Fatal(err)
				}

				if len(cma.Status.DefaultConfigReferences) != 1 {
					t.Errorf("DefaultConfigReferences object is not correct: %v", cma.Status.DefaultConfigReferences)
				}
				if len(cma.Status.InstallProgressions) != 0 {
					t.Errorf("InstallProgressions object is not correct: %v", cma.Status.InstallProgressions)
				}
			},
		},
		{
			name:                "update clustermanagementaddon status with type manual with invalid supported configs",
			syncKey:             "test",
			managedClusteraddon: []runtime.Object{},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "testcrd", "testcr").WithDefaultConfigs(
				addonv1beta1.AddOnConfig{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addonhubconfigs",
					},
				}).Build()},
			validateAddonActions: addontesting.AssertNoActions,
		},
		{
			name:                "update clustermanagementaddon status filters out sentinel value",
			syncKey:             "test",
			managedClusteraddon: []runtime.Object{},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "testcrd", "testcr").WithDefaultConfigs(
				addonv1beta1.AddOnConfig{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					ConfigReferent: addonv1beta1.ConfigReferent{
						Name: addonv1beta1.ReservedNoDefaultConfigName,
					},
				},
				addonv1beta1.AddOnConfig{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{
						Group:    "proxy.open-cluster-management.io",
						Resource: "managedproxyconfigurations",
					},
					ConfigReferent: addonv1beta1.ConfigReferent{
						Name:      "cluster-proxy",
						Namespace: "open-cluster-management",
					},
				}).Build()},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch
				cma := &addonv1beta1.ClusterManagementAddOn{}
				err := json.Unmarshal(actual, cma)
				if err != nil {
					t.Fatal(err)
				}

				// Should only have 1 DefaultConfigReference (the valid one), sentinel should be filtered out
				if len(cma.Status.DefaultConfigReferences) != 1 {
					t.Errorf("DefaultConfigReferences should have 1 entry (sentinel filtered), got: %d", len(cma.Status.DefaultConfigReferences))
				}
				if len(cma.Status.DefaultConfigReferences) > 0 {
					if cma.Status.DefaultConfigReferences[0].DesiredConfig.Name == addonv1beta1.ReservedNoDefaultConfigName {
						t.Errorf("Sentinel value should have been filtered out but was found in DefaultConfigReferences")
					}
					if cma.Status.DefaultConfigReferences[0].DesiredConfig.Name != "cluster-proxy" {
						t.Errorf("DefaultConfigReferences[0].Name = %v, want cluster-proxy", cma.Status.DefaultConfigReferences[0].DesiredConfig.Name)
					}
				}
				if len(cma.Status.InstallProgressions) != 0 {
					t.Errorf("InstallProgressions object is not correct: %v", cma.Status.InstallProgressions)
				}
			},
		},
		{
			name:                "update clustermanagementaddon status with type placements with multiple same-GVK",
			syncKey:             "test",
			managedClusteraddon: []runtime.Object{},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "testcrd", "testcr").WithPlacementStrategy(
				addonv1beta1.PlacementStrategy{
					PlacementRef: addonv1beta1.PlacementRef{
						Name:      "placement1",
						Namespace: "test",
					},
				},
				addonv1beta1.PlacementStrategy{
					PlacementRef: addonv1beta1.PlacementRef{
						Name:      "placement2",
						Namespace: "test",
					},
					Configs: []addonv1beta1.AddOnConfig{
						{
							ConfigGroupResource: addonv1beta1.ConfigGroupResource{
								Group:    "addon.open-cluster-management.io",
								Resource: "addondeploymentconfigs",
							},
							ConfigReferent: addonv1beta1.ConfigReferent{
								Name:      "test",
								Namespace: "test",
							},
						},
					},
				},
				addonv1beta1.PlacementStrategy{
					PlacementRef: addonv1beta1.PlacementRef{
						Name:      "placement3",
						Namespace: "test",
					},
					Configs: []addonv1beta1.AddOnConfig{
						{
							ConfigGroupResource: addonv1beta1.ConfigGroupResource{
								Group:    "addon.open-cluster-management.io",
								Resource: "addondeploymentconfigs",
							},
							ConfigReferent: addonv1beta1.ConfigReferent{
								Name:      "test",
								Namespace: "test",
							},
						},
						{
							ConfigGroupResource: addonv1beta1.ConfigGroupResource{
								Group:    "addon.open-cluster-management.io",
								Resource: "addondeploymentconfigs",
							},
							ConfigReferent: addonv1beta1.ConfigReferent{
								Name:      "test",
								Namespace: "test1",
							},
						},
					},
				},
				addonv1beta1.PlacementStrategy{
					PlacementRef: addonv1beta1.PlacementRef{
						Name:      "placement4",
						Namespace: "test",
					},
					Configs: []addonv1beta1.AddOnConfig{
						{
							ConfigGroupResource: addonv1beta1.ConfigGroupResource{
								Group:    "addon.open-cluster-management.io",
								Resource: "addondeploymentconfigs",
							},
							ConfigReferent: addonv1beta1.ConfigReferent{
								Name:      "test",
								Namespace: "test",
							},
						},
						{
							ConfigGroupResource: addonv1beta1.ConfigGroupResource{
								Group:    "addon.open-cluster-management.io",
								Resource: "addondeploymentconfigs",
							},
							ConfigReferent: addonv1beta1.ConfigReferent{
								Name:      "test",
								Namespace: "test",
							},
						},
					},
				},
			).Build()},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch
				cma := &addonv1beta1.ClusterManagementAddOn{}
				err := json.Unmarshal(actual, cma)
				if err != nil {
					t.Fatal(err)
				}

				if len(cma.Status.DefaultConfigReferences) != 0 {
					t.Errorf("DefaultConfigReferences object is not correct: %v", cma.Status.DefaultConfigReferences)
				}
				if len(cma.Status.InstallProgressions) != 4 {
					t.Errorf("InstallProgressions object is not correct: %v", cma.Status.InstallProgressions)
				}
				if len(cma.Status.InstallProgressions[0].ConfigReferences) != 0 {
					t.Errorf("InstallProgressions ConfigReferences object is not correct: %v", cma.Status.InstallProgressions[0].ConfigReferences)
				}
				if len(cma.Status.InstallProgressions[1].ConfigReferences) != 1 {
					t.Errorf("InstallProgressions ConfigReferences object is not correct: %v", cma.Status.InstallProgressions[0].ConfigReferences)
				}
				if len(cma.Status.InstallProgressions[2].ConfigReferences) != 2 {
					t.Fatalf("InstallProgressions ConfigReferences object is not correct: %v", cma.Status.InstallProgressions[0].ConfigReferences)
				}
				// Verify ordering matches the spec order: test/test first, then test1/test
				if cma.Status.InstallProgressions[2].ConfigReferences[0].DesiredConfig.Namespace != "test" ||
					cma.Status.InstallProgressions[2].ConfigReferences[0].DesiredConfig.Name != "test" {
					t.Errorf("InstallProgressions ConfigReferences[0] ordering is not correct: %v", cma.Status.InstallProgressions[2].ConfigReferences[0].DesiredConfig)
				}
				if cma.Status.InstallProgressions[2].ConfigReferences[1].DesiredConfig.Namespace != "test1" ||
					cma.Status.InstallProgressions[2].ConfigReferences[1].DesiredConfig.Name != "test" {
					t.Errorf("InstallProgressions ConfigReferences[1] ordering is not correct: %v", cma.Status.InstallProgressions[2].ConfigReferences[1].DesiredConfig)
				}
				if len(cma.Status.InstallProgressions[3].ConfigReferences) != 1 {
					t.Errorf("InstallProgressions ConfigReferences object is not correct: %v", cma.Status.InstallProgressions[0].ConfigReferences)
				}
			},
		},
		{
			name:                "update clustermanagementaddon status with type placements with multiple same-GVK and default configs",
			syncKey:             "test",
			managedClusteraddon: []runtime.Object{},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "testcrd", "testcr").WithPlacementStrategy(
				addonv1beta1.PlacementStrategy{
					PlacementRef: addonv1beta1.PlacementRef{
						Name:      "placement1",
						Namespace: "test",
					},
					Configs: []addonv1beta1.AddOnConfig{
						{
							ConfigGroupResource: addonv1beta1.ConfigGroupResource{
								Group:    "addon.open-cluster-management.io",
								Resource: "addonhubconfigs",
							},
							ConfigReferent: addonv1beta1.ConfigReferent{
								Name:      "test1",
								Namespace: "test",
							},
						},
					},
				},
				addonv1beta1.PlacementStrategy{
					PlacementRef: addonv1beta1.PlacementRef{
						Name:      "placement2",
						Namespace: "test",
					},
					Configs: []addonv1beta1.AddOnConfig{
						{
							ConfigGroupResource: addonv1beta1.ConfigGroupResource{
								Group:    "addon.open-cluster-management.io",
								Resource: "addonhubconfigs",
							},
							ConfigReferent: addonv1beta1.ConfigReferent{
								Name:      "test1",
								Namespace: "test",
							},
						},
						{
							ConfigGroupResource: addonv1beta1.ConfigGroupResource{
								Group:    "addon.open-cluster-management.io",
								Resource: "addonhubconfigs",
							},
							ConfigReferent: addonv1beta1.ConfigReferent{
								Name:      "test2",
								Namespace: "test",
							},
						},
					},
				},
				addonv1beta1.PlacementStrategy{
					PlacementRef: addonv1beta1.PlacementRef{
						Name:      "placement3",
						Namespace: "test",
					},
					Configs: []addonv1beta1.AddOnConfig{
						{
							ConfigGroupResource: addonv1beta1.ConfigGroupResource{
								Group:    "addon.open-cluster-management.io",
								Resource: "addondeploymentconfigs",
							},
							ConfigReferent: addonv1beta1.ConfigReferent{
								Name:      "test3",
								Namespace: "test",
							},
						},
						{
							ConfigGroupResource: addonv1beta1.ConfigGroupResource{
								Group:    "addon.open-cluster-management.io",
								Resource: "addondeploymentconfigs",
							},
							ConfigReferent: addonv1beta1.ConfigReferent{
								Name:      "test4",
								Namespace: "test",
							},
						},
					},
				},
			).WithDefaultConfigs(
				addonv1beta1.AddOnConfig{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addonhubconfigs",
					},
					ConfigReferent: addonv1beta1.ConfigReferent{
						Name:      "test",
						Namespace: "test",
					},
				}).Build()},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch
				cma := &addonv1beta1.ClusterManagementAddOn{}
				err := json.Unmarshal(actual, cma)
				if err != nil {
					t.Fatal(err)
				}

				if len(cma.Status.DefaultConfigReferences) != 1 {
					t.Errorf("DefaultConfigReferences object is not correct: %v", cma.Status.DefaultConfigReferences)
				}
				if len(cma.Status.InstallProgressions) != 3 {
					t.Errorf("InstallProgressions object is not correct: %v", cma.Status.InstallProgressions)
				}
				if len(cma.Status.InstallProgressions[0].ConfigReferences) != 1 {
					t.Errorf("InstallProgressions ConfigReferences object is not correct: %v", cma.Status.InstallProgressions[0].ConfigReferences)
				}
				if cma.Status.InstallProgressions[0].ConfigReferences[0].DesiredConfig.Name != "test1" {
					t.Errorf("InstallProgressions ConfigReferences object is not correct: %v", cma.Status.InstallProgressions[0].ConfigReferences[0].DesiredConfig.Name)
				}
				if len(cma.Status.InstallProgressions[1].ConfigReferences) != 2 {
					t.Fatalf("InstallProgressions ConfigReferences object is not correct: %v", cma.Status.InstallProgressions[0].ConfigReferences)
				}
				// Verify ordering matches the spec order: test1 first, then test2
				if cma.Status.InstallProgressions[1].ConfigReferences[0].DesiredConfig.Name != "test1" {
					t.Errorf("InstallProgressions ConfigReferences[0] ordering is not correct: %v", cma.Status.InstallProgressions[1].ConfigReferences[0].DesiredConfig)
				}
				if cma.Status.InstallProgressions[1].ConfigReferences[1].DesiredConfig.Name != "test2" {
					t.Errorf("InstallProgressions ConfigReferences[1] ordering is not correct: %v", cma.Status.InstallProgressions[1].ConfigReferences[1].DesiredConfig)
				}
				if len(cma.Status.InstallProgressions[2].ConfigReferences) != 3 {
					t.Errorf("InstallProgressions ConfigReferences object is not correct: %v", cma.Status.InstallProgressions[0].ConfigReferences)
				}
			},
		},
		{
			name:                "update clustermanagementaddon InstallProgressions filters out sentinel value",
			syncKey:             "test",
			managedClusteraddon: []runtime.Object{},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "testcrd", "testcr").WithPlacementStrategy(
				addonv1beta1.PlacementStrategy{
					PlacementRef: addonv1beta1.PlacementRef{
						Name:      "placement1",
						Namespace: "test",
					},
				},
			).WithDefaultConfigs(
				addonv1beta1.AddOnConfig{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					ConfigReferent: addonv1beta1.ConfigReferent{
						Name: addonv1beta1.ReservedNoDefaultConfigName,
					},
				},
				addonv1beta1.AddOnConfig{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{
						Group:    "proxy.open-cluster-management.io",
						Resource: "managedproxyconfigurations",
					},
					ConfigReferent: addonv1beta1.ConfigReferent{
						Name:      "cluster-proxy",
						Namespace: "open-cluster-management",
					},
				}).Build()},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch
				cma := &addonv1beta1.ClusterManagementAddOn{}
				err := json.Unmarshal(actual, cma)
				if err != nil {
					t.Fatal(err)
				}

				// DefaultConfigReferences should only have the valid config, sentinel filtered
				if len(cma.Status.DefaultConfigReferences) != 1 {
					t.Errorf("DefaultConfigReferences should have 1 entry (sentinel filtered), got: %d", len(cma.Status.DefaultConfigReferences))
				}
				if len(cma.Status.DefaultConfigReferences) > 0 && cma.Status.DefaultConfigReferences[0].DesiredConfig.Name == addonv1beta1.ReservedNoDefaultConfigName {
					t.Errorf("Sentinel value should have been filtered from DefaultConfigReferences")
				}

				// InstallProgressions should exist
				if len(cma.Status.InstallProgressions) != 1 {
					t.Errorf("InstallProgressions should have 1 entry, got: %d", len(cma.Status.InstallProgressions))
				}

				// InstallProgressions[0].ConfigReferences should only have valid config, sentinel filtered
				if len(cma.Status.InstallProgressions) > 0 {
					if len(cma.Status.InstallProgressions[0].ConfigReferences) != 1 {
						t.Errorf("InstallProgressions[0].ConfigReferences should have 1 entry (sentinel filtered), got: %d", len(cma.Status.InstallProgressions[0].ConfigReferences))
					}
					if len(cma.Status.InstallProgressions[0].ConfigReferences) > 0 {
						if cma.Status.InstallProgressions[0].ConfigReferences[0].DesiredConfig.Name == addonv1beta1.ReservedNoDefaultConfigName {
							t.Errorf("Sentinel value should have been filtered from InstallProgressions ConfigReferences")
						}
						if cma.Status.InstallProgressions[0].ConfigReferences[0].DesiredConfig.Name != "cluster-proxy" {
							t.Errorf("InstallProgressions[0].ConfigReferences[0].Name = %v, want cluster-proxy", cma.Status.InstallProgressions[0].ConfigReferences[0].DesiredConfig.Name)
						}
					}
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			obj := append(c.clusterManagementAddon, c.managedClusteraddon...) //nolint:gocritic
			fakeAddonClient := fakeaddon.NewSimpleClientset(obj...)

			addonInformers := addoninformers.NewSharedInformerFactory(fakeAddonClient, 10*time.Minute)

			for _, obj := range c.managedClusteraddon {
				if err := addonInformers.Addon().V1beta1().ManagedClusterAddOns().Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}
			for _, obj := range c.clusterManagementAddon {
				if err := addonInformers.Addon().V1beta1().ClusterManagementAddOns().Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}

			syncContext := testingcommon.NewFakeSyncContext(t, c.syncKey)

			controller := NewCMAInstallProgressionController(
				fakeAddonClient,
				addonInformers.Addon().V1beta1().ManagedClusterAddOns(),
				addonInformers.Addon().V1beta1().ClusterManagementAddOns(),
				utils.ManagedByAddonManager,
			)

			err := controller.Sync(context.TODO(), syncContext, c.syncKey)
			if err != nil {
				t.Errorf("expected no error when sync: %v", err)
			}
			c.validateAddonActions(t, fakeAddonClient.Actions())
		})
	}
}
