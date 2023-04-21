package managementaddoninstallprogression

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/addontesting"
	"open-cluster-management.io/api/addon/v1alpha1"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	fakeaddon "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
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
				v1alpha1.ConfigMeta{
					ConfigGroupResource: v1alpha1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addonhubconfigs",
					},
					DefaultConfig: &v1alpha1.ConfigReferent{
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
				v1alpha1.ConfigMeta{
					ConfigGroupResource: v1alpha1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addonhubconfigs",
					},
				}).Build()},
			validateAddonActions: addontesting.AssertNoActions,
		},
		{
			name:                "update clustermanagementaddon status with type placements",
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
						v1alpha1.AddOnConfig{
							ConfigGroupResource: v1alpha1.ConfigGroupResource{
								Group:    "addon.open-cluster-management.io",
								Resource: "addondeploymentconfigs",
							},
							ConfigReferent: v1alpha1.ConfigReferent{
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
				if len(cma.Status.InstallProgressions) != 2 {
					t.Errorf("InstallProgressions object is not correct: %v", cma.Status.InstallProgressions)
				}
				if len(cma.Status.InstallProgressions[0].ConfigReferences) != 0 {
					t.Errorf("InstallProgressions ConfigReferences object is not correct: %v", cma.Status.InstallProgressions[0].ConfigReferences)
				}
				if len(cma.Status.InstallProgressions[1].ConfigReferences) != 1 {
					t.Errorf("InstallProgressions ConfigReferences object is not correct: %v", cma.Status.InstallProgressions[0].ConfigReferences)
				}

			},
		},
		{
			name:                "update clustermanagementaddon status with type placements and default configs",
			syncKey:             "test",
			managedClusteraddon: []runtime.Object{},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "testcrd", "testcr").WithPlacementStrategy(
				addonapiv1alpha1.PlacementStrategy{
					PlacementRef: addonapiv1alpha1.PlacementRef{
						Name:      "placement1",
						Namespace: "test",
					},
					Configs: []addonapiv1alpha1.AddOnConfig{
						v1alpha1.AddOnConfig{
							ConfigGroupResource: v1alpha1.ConfigGroupResource{
								Group:    "addon.open-cluster-management.io",
								Resource: "addonhubconfigs",
							},
							ConfigReferent: v1alpha1.ConfigReferent{
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
						v1alpha1.AddOnConfig{
							ConfigGroupResource: v1alpha1.ConfigGroupResource{
								Group:    "addon.open-cluster-management.io",
								Resource: "addondeploymentconfigs",
							},
							ConfigReferent: v1alpha1.ConfigReferent{
								Name:      "test",
								Namespace: "test",
							},
						},
					},
				},
			).WithSupportedConfigs(
				v1alpha1.ConfigMeta{
					ConfigGroupResource: v1alpha1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addonhubconfigs",
					},
					DefaultConfig: &v1alpha1.ConfigReferent{
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
				if len(cma.Status.InstallProgressions) != 2 {
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
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			obj := append(c.clusterManagementAddon, c.managedClusteraddon...)
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

			controller := managementAddonInstallProgressionController{
				addonClient:                  fakeAddonClient,
				clusterManagementAddonLister: addonInformers.Addon().V1alpha1().ClusterManagementAddOns().Lister(),
				managedClusterAddonLister:    addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Lister(),
			}

			syncContext := addontesting.NewFakeSyncContext(t)
			err := controller.sync(context.TODO(), syncContext, c.syncKey)
			if err != nil {
				t.Errorf("expected no error when sync: %v", err)
			}
			c.validateAddonActions(t, fakeAddonClient.Actions())
		})
	}
}
