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
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
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
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "testcrd", "testcr").WithSupportedConfigs(
				addonapiv1alpha1.ConfigMeta{
					ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addonhubconfigs",
					},
					DefaultConfig: &addonapiv1alpha1.ConfigReferent{
						Name:      "test",
						Namespace: "test",
					},
				}).Build()},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch
				cma := &addonapiv1alpha1.ClusterManagementAddOn{}
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
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "testcrd", "testcr").WithSupportedConfigs(
				addonapiv1alpha1.ConfigMeta{
					ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addonhubconfigs",
					},
				}).Build()},
			validateAddonActions: addontesting.AssertNoActions,
		},
		{
			name:                "update clustermanagementaddon status with type placements with multiple same-GVK",
			syncKey:             "test",
			managedClusteraddon: []runtime.Object{},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "testcrd", "testcr").WithPlacementStrategy(
				addonapiv1alpha1.PlacementStrategy{
					PlacementRef: addonapiv1alpha1.PlacementRef{
						Name:      "placement1",
						Namespace: "test",
					},
				},
				addonapiv1alpha1.PlacementStrategy{
					PlacementRef: addonapiv1alpha1.PlacementRef{
						Name:      "placement2",
						Namespace: "test",
					},
					Configs: []addonapiv1alpha1.AddOnConfig{
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    "addon.open-cluster-management.io",
								Resource: "addondeploymentconfigs",
							},
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Name:      "test",
								Namespace: "test",
							},
						},
					},
				},
				addonapiv1alpha1.PlacementStrategy{
					PlacementRef: addonapiv1alpha1.PlacementRef{
						Name:      "placement3",
						Namespace: "test",
					},
					Configs: []addonapiv1alpha1.AddOnConfig{
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    "addon.open-cluster-management.io",
								Resource: "addondeploymentconfigs",
							},
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Name:      "test",
								Namespace: "test",
							},
						},
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    "addon.open-cluster-management.io",
								Resource: "addondeploymentconfigs",
							},
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Name:      "test",
								Namespace: "test1",
							},
						},
					},
				},
				addonapiv1alpha1.PlacementStrategy{
					PlacementRef: addonapiv1alpha1.PlacementRef{
						Name:      "placement4",
						Namespace: "test",
					},
					Configs: []addonapiv1alpha1.AddOnConfig{
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    "addon.open-cluster-management.io",
								Resource: "addondeploymentconfigs",
							},
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Name:      "test",
								Namespace: "test",
							},
						},
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    "addon.open-cluster-management.io",
								Resource: "addondeploymentconfigs",
							},
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
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
				cma := &addonapiv1alpha1.ClusterManagementAddOn{}
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
					t.Errorf("InstallProgressions ConfigReferences object is not correct: %v", cma.Status.InstallProgressions[0].ConfigReferences)
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
				addonapiv1alpha1.PlacementStrategy{
					PlacementRef: addonapiv1alpha1.PlacementRef{
						Name:      "placement1",
						Namespace: "test",
					},
					Configs: []addonapiv1alpha1.AddOnConfig{
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    "addon.open-cluster-management.io",
								Resource: "addonhubconfigs",
							},
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Name:      "test1",
								Namespace: "test",
							},
						},
					},
				},
				addonapiv1alpha1.PlacementStrategy{
					PlacementRef: addonapiv1alpha1.PlacementRef{
						Name:      "placement2",
						Namespace: "test",
					},
					Configs: []addonapiv1alpha1.AddOnConfig{
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    "addon.open-cluster-management.io",
								Resource: "addonhubconfigs",
							},
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Name:      "test1",
								Namespace: "test",
							},
						},
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    "addon.open-cluster-management.io",
								Resource: "addonhubconfigs",
							},
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Name:      "test2",
								Namespace: "test",
							},
						},
					},
				},
				addonapiv1alpha1.PlacementStrategy{
					PlacementRef: addonapiv1alpha1.PlacementRef{
						Name:      "placement3",
						Namespace: "test",
					},
					Configs: []addonapiv1alpha1.AddOnConfig{
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    "addon.open-cluster-management.io",
								Resource: "addondeploymentconfigs",
							},
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Name:      "test3",
								Namespace: "test",
							},
						},
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    "addon.open-cluster-management.io",
								Resource: "addondeploymentconfigs",
							},
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Name:      "test4",
								Namespace: "test",
							},
						},
					},
				},
			).WithSupportedConfigs(
				addonapiv1alpha1.ConfigMeta{
					ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addonhubconfigs",
					},
					DefaultConfig: &addonapiv1alpha1.ConfigReferent{
						Name:      "test",
						Namespace: "test",
					},
				}).Build()},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch
				cma := &addonapiv1alpha1.ClusterManagementAddOn{}
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
					t.Errorf("InstallProgressions ConfigReferences object is not correct: %v", cma.Status.InstallProgressions[0].ConfigReferences)
				}
				if len(cma.Status.InstallProgressions[2].ConfigReferences) != 3 {
					t.Errorf("InstallProgressions ConfigReferences object is not correct: %v", cma.Status.InstallProgressions[0].ConfigReferences)
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
				if err := addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}
			for _, obj := range c.clusterManagementAddon {
				if err := addonInformers.Addon().V1alpha1().ClusterManagementAddOns().Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}

			syncContext := testingcommon.NewFakeSyncContext(t, c.syncKey)
			recorder := syncContext.Recorder()

			controller := NewCMAInstallProgressionController(
				fakeAddonClient,
				addonInformers.Addon().V1alpha1().ManagedClusterAddOns(),
				addonInformers.Addon().V1alpha1().ClusterManagementAddOns(),
				utils.ManagedByAddonManager,
				recorder,
			)

			err := controller.Sync(context.TODO(), syncContext)
			if err != nil {
				t.Errorf("expected no error when sync: %v", err)
			}
			c.validateAddonActions(t, fakeAddonClient.Actions())
		})
	}
}
