package addonconfiguration

import (
	"context"
	"encoding/json"
	"sort"
	"testing"
	"time"

	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"

	"open-cluster-management.io/addon-framework/pkg/addonmanager/addontesting"
	"open-cluster-management.io/addon-framework/pkg/index"
	"open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	fakeaddon "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	fakecluster "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterv1informers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
)

func TestAddonConfigReconcile(t *testing.T) {
	cases := []struct {
		name                   string
		managedClusteraddon    []runtime.Object
		clusterManagementAddon *addonv1alpha1.ClusterManagementAddOn
		placements             []runtime.Object
		placementDecisions     []runtime.Object
		validateAddonActions   func(t *testing.T, actions []clienttesting.Action)
		expectErr              bool
	}{
		{
			name: "no configuration",
			managedClusteraddon: []runtime.Object{
				addontesting.NewAddon("test", "cluster1"),
				addontesting.NewAddon("test", "cluster2"),
			},
			clusterManagementAddon: addontesting.NewClusterManagementAddon("test", "", "").Build(),
			placements:             []runtime.Object{},
			placementDecisions:     []runtime.Object{},
			validateAddonActions:   addontesting.AssertNoActions,
		},
		{
			name: "manual installStrategy",
			managedClusteraddon: []runtime.Object{
				addontesting.NewAddon("test", "cluster1"),
				addontesting.NewAddon("test", "cluster2"),
			},
			clusterManagementAddon: addontesting.NewClusterManagementAddon("test", "", "").WithSupportedConfigs(addonv1alpha1.ConfigMeta{
				ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
				DefaultConfig:       &addonv1alpha1.ConfigReferent{Name: "test"},
			}).WithDefaultConfigReferences(addonv1alpha1.DefaultConfigReference{
				ConfigGroupResource: v1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
				DesiredConfig: &v1alpha1.ConfigSpecHash{
					ConfigReferent: v1alpha1.ConfigReferent{Name: "test"},
					SpecHash:       "hash",
				},
			}).Build(),
			placements:         []runtime.Object{},
			placementDecisions: []runtime.Object{},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch", "patch")
				sort.Sort(byPatchName(actions))
				expectPatchConfigurationAction(t, actions[0], []addonv1alpha1.ConfigReference{{
					ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
					ConfigReferent:      addonv1alpha1.ConfigReferent{Name: "test"},
					DesiredConfig: &addonv1alpha1.ConfigSpecHash{
						ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test"},
						SpecHash:       "hash",
					},
					LastObservedGeneration: 0,
				}})
			},
		},
		{
			name: "placement installStrategy",
			managedClusteraddon: []runtime.Object{
				addontesting.NewAddon("test", "cluster1"),
				addontesting.NewAddon("test", "cluster2"),
			},
			placements: []runtime.Object{
				&clusterv1beta1.Placement{ObjectMeta: metav1.ObjectMeta{Name: "test-placement", Namespace: "default"}},
			},
			placementDecisions: []runtime.Object{
				&clusterv1beta1.PlacementDecision{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-placement",
						Namespace: "default",
						Labels:    map[string]string{clusterv1beta1.PlacementLabel: "test-placement"},
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{{ClusterName: "cluster2"}},
					},
				},
			},
			clusterManagementAddon: addontesting.NewClusterManagementAddon("test", "", "").WithSupportedConfigs(addonv1alpha1.ConfigMeta{
				ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
				DefaultConfig:       &addonv1alpha1.ConfigReferent{Name: "test"},
			}).WithDefaultConfigReferences(addonv1alpha1.DefaultConfigReference{
				ConfigGroupResource: v1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
				DesiredConfig: &v1alpha1.ConfigSpecHash{
					ConfigReferent: v1alpha1.ConfigReferent{Name: "test"},
					SpecHash:       "hash",
				},
			}).WithPlacementStrategy(addonv1alpha1.PlacementStrategy{
				PlacementRef: addonv1alpha1.PlacementRef{Name: "test-placement", Namespace: "default"},
			}).WithInstallProgression(addonv1alpha1.InstallProgression{
				PlacementRef: addonv1alpha1.PlacementRef{Name: "test-placement", Namespace: "default"},
				ConfigReferences: []addonv1alpha1.InstallConfigReference{
					{
						ConfigGroupResource: v1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &v1alpha1.ConfigSpecHash{
							ConfigReferent: v1alpha1.ConfigReferent{Name: "test1"},
							SpecHash:       "hash1",
						},
					},
				},
			}).Build(),
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch", "patch")
				sort.Sort(byPatchName(actions))
				expectPatchConfigurationAction(t, actions[0], []addonv1alpha1.ConfigReference{{
					ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
					ConfigReferent:      addonv1alpha1.ConfigReferent{Name: "test"},
					DesiredConfig: &addonv1alpha1.ConfigSpecHash{
						ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test"},
						SpecHash:       "hash",
					},
					LastObservedGeneration: 0,
				}})
				expectPatchConfigurationAction(t, actions[1], []addonv1alpha1.ConfigReference{{
					ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
					ConfigReferent:      addonv1alpha1.ConfigReferent{Name: "test1"},
					DesiredConfig: &addonv1alpha1.ConfigSpecHash{
						ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test1"},
						SpecHash:       "hash1",
					},
					LastObservedGeneration: 0,
				}})
			},
		},
		{
			name: "mca override",
			managedClusteraddon: []runtime.Object{
				newManagedClusterAddon("test", "cluster1", []addonv1alpha1.AddOnConfig{{
					ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
					ConfigReferent:      addonv1alpha1.ConfigReferent{Name: "test2"},
				}}, nil),
				addontesting.NewAddon("test", "cluster2"),
			},
			placements: []runtime.Object{
				&clusterv1beta1.Placement{ObjectMeta: metav1.ObjectMeta{Name: "test-placement", Namespace: "default"}},
			},
			placementDecisions: []runtime.Object{
				&clusterv1beta1.PlacementDecision{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-placement",
						Namespace: "default",
						Labels:    map[string]string{clusterv1beta1.PlacementLabel: "test-placement"},
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{{ClusterName: "cluster1"}, {ClusterName: "cluster2"}},
					},
				},
			},
			clusterManagementAddon: addontesting.NewClusterManagementAddon("test", "", "").WithSupportedConfigs(addonv1alpha1.ConfigMeta{
				ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
				DefaultConfig:       &addonv1alpha1.ConfigReferent{Name: "test"},
			}).WithDefaultConfigReferences(addonv1alpha1.DefaultConfigReference{
				ConfigGroupResource: v1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
				DesiredConfig: &v1alpha1.ConfigSpecHash{
					ConfigReferent: v1alpha1.ConfigReferent{Name: "test"},
					SpecHash:       "hash",
				},
			}).WithPlacementStrategy(addonv1alpha1.PlacementStrategy{
				PlacementRef: addonv1alpha1.PlacementRef{Name: "test-placement", Namespace: "default"},
			}).WithInstallProgression(addonv1alpha1.InstallProgression{
				PlacementRef: addonv1alpha1.PlacementRef{Name: "test-placement", Namespace: "default"},
				ConfigReferences: []addonv1alpha1.InstallConfigReference{
					{
						ConfigGroupResource: v1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &v1alpha1.ConfigSpecHash{
							ConfigReferent: v1alpha1.ConfigReferent{Name: "test1"},
							SpecHash:       "hash1",
						},
					},
				},
			}).Build(),
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch", "patch")
				sort.Sort(byPatchName(actions))
				expectPatchConfigurationAction(t, actions[0], []addonv1alpha1.ConfigReference{{
					ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
					ConfigReferent:      addonv1alpha1.ConfigReferent{Name: "test2"},
					DesiredConfig: &addonv1alpha1.ConfigSpecHash{
						ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test2"},
						SpecHash:       "",
					},
					LastObservedGeneration: 0,
				}})
				expectPatchConfigurationAction(t, actions[1], []addonv1alpha1.ConfigReference{{
					ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
					ConfigReferent:      addonv1alpha1.ConfigReferent{Name: "test1"},
					DesiredConfig: &addonv1alpha1.ConfigSpecHash{
						ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test1"},
						SpecHash:       "hash1",
					},
					LastObservedGeneration: 0,
				}})
			},
		},
		{
			name: "config name/namespce change",
			managedClusteraddon: []runtime.Object{
				newManagedClusterAddon("test", "cluster1", []addonv1alpha1.AddOnConfig{}, []addonv1alpha1.ConfigReference{{
					ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
					ConfigReferent:      addonv1alpha1.ConfigReferent{Name: "test1"},
					DesiredConfig: &addonv1alpha1.ConfigSpecHash{
						ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test1"},
						SpecHash:       "hash1",
					},
					LastObservedGeneration: 1,
				}}),
			},
			placements: []runtime.Object{
				&clusterv1beta1.Placement{ObjectMeta: metav1.ObjectMeta{Name: "test-placement", Namespace: "default"}},
			},
			placementDecisions: []runtime.Object{
				&clusterv1beta1.PlacementDecision{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-placement",
						Namespace: "default",
						Labels:    map[string]string{clusterv1beta1.PlacementLabel: "test-placement"},
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{{ClusterName: "cluster1"}},
					},
				},
			},
			clusterManagementAddon: addontesting.NewClusterManagementAddon("test", "", "").WithSupportedConfigs(addonv1alpha1.ConfigMeta{
				ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
				DefaultConfig:       &addonv1alpha1.ConfigReferent{Name: "test"},
			}).WithPlacementStrategy(addonv1alpha1.PlacementStrategy{
				PlacementRef: addonv1alpha1.PlacementRef{Name: "test-placement", Namespace: "default"},
			}).WithInstallProgression(addonv1alpha1.InstallProgression{
				PlacementRef: addonv1alpha1.PlacementRef{Name: "test-placement", Namespace: "default"},
				ConfigReferences: []addonv1alpha1.InstallConfigReference{
					{
						ConfigGroupResource: v1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &v1alpha1.ConfigSpecHash{
							ConfigReferent: v1alpha1.ConfigReferent{Name: "test2"},
							SpecHash:       "hash2",
						},
					},
				},
			}).Build(),
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				expectPatchConfigurationAction(t, actions[0], []addonv1alpha1.ConfigReference{{
					ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
					ConfigReferent:      addonv1alpha1.ConfigReferent{Name: "test2"},
					DesiredConfig: &addonv1alpha1.ConfigSpecHash{
						ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test2"},
						SpecHash:       "hash2",
					},
					LastObservedGeneration: 0,
				}})
			},
		},
		{
			name: "config spec hash change",
			managedClusteraddon: []runtime.Object{
				newManagedClusterAddon("test", "cluster1", []addonv1alpha1.AddOnConfig{}, []addonv1alpha1.ConfigReference{{
					ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
					ConfigReferent:      addonv1alpha1.ConfigReferent{Name: "test1"},
					DesiredConfig: &addonv1alpha1.ConfigSpecHash{
						ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test1"},
						SpecHash:       "hash1",
					},
					LastObservedGeneration: 1,
				}}),
			},
			placements: []runtime.Object{
				&clusterv1beta1.Placement{ObjectMeta: metav1.ObjectMeta{Name: "test-placement", Namespace: "default"}},
			},
			placementDecisions: []runtime.Object{
				&clusterv1beta1.PlacementDecision{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-placement",
						Namespace: "default",
						Labels:    map[string]string{clusterv1beta1.PlacementLabel: "test-placement"},
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{{ClusterName: "cluster1"}},
					},
				},
			},
			clusterManagementAddon: addontesting.NewClusterManagementAddon("test", "", "").WithSupportedConfigs(addonv1alpha1.ConfigMeta{
				ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
				DefaultConfig:       &addonv1alpha1.ConfigReferent{Name: "test"},
			}).WithPlacementStrategy(addonv1alpha1.PlacementStrategy{
				PlacementRef: addonv1alpha1.PlacementRef{Name: "test-placement", Namespace: "default"},
				Configs: []addonv1alpha1.AddOnConfig{v1alpha1.AddOnConfig{
					ConfigGroupResource: v1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
					ConfigReferent:      v1alpha1.ConfigReferent{Name: "test1"}}},
			}).WithInstallProgression(addonv1alpha1.InstallProgression{
				PlacementRef: addonv1alpha1.PlacementRef{Name: "test-placement", Namespace: "default"},
				ConfigReferences: []addonv1alpha1.InstallConfigReference{
					{
						ConfigGroupResource: v1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &v1alpha1.ConfigSpecHash{
							ConfigReferent: v1alpha1.ConfigReferent{Name: "test1"},
							SpecHash:       "hash1new",
						},
					},
				},
			}).Build(),
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				expectPatchConfigurationAction(t, actions[0], []addonv1alpha1.ConfigReference{{
					ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
					ConfigReferent:      addonv1alpha1.ConfigReferent{Name: "test1"},
					DesiredConfig: &addonv1alpha1.ConfigSpecHash{
						ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test1"},
						SpecHash:       "hash1new",
					},
					LastObservedGeneration: 1,
				}})
			},
		},
		{
			name: "mca noop",
			managedClusteraddon: []runtime.Object{
				newManagedClusterAddon("test", "cluster1", []addonv1alpha1.AddOnConfig{}, []addonv1alpha1.ConfigReference{{
					ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
					ConfigReferent:      addonv1alpha1.ConfigReferent{Name: "test1"},
					DesiredConfig: &addonv1alpha1.ConfigSpecHash{
						ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test1"},
						SpecHash:       "hash1",
					},
					LastObservedGeneration: 1,
				}}),
			},
			placements: []runtime.Object{
				&clusterv1beta1.Placement{ObjectMeta: metav1.ObjectMeta{Name: "test-placement", Namespace: "default"}},
			},
			placementDecisions: []runtime.Object{
				&clusterv1beta1.PlacementDecision{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-placement",
						Namespace: "default",
						Labels:    map[string]string{clusterv1beta1.PlacementLabel: "test-placement"},
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{{ClusterName: "cluster1"}},
					},
				},
			},
			clusterManagementAddon: addontesting.NewClusterManagementAddon("test", "", "").WithSupportedConfigs(addonv1alpha1.ConfigMeta{
				ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
				DefaultConfig:       &addonv1alpha1.ConfigReferent{Name: "test"},
			}).WithPlacementStrategy(addonv1alpha1.PlacementStrategy{
				PlacementRef: addonv1alpha1.PlacementRef{Name: "test-placement", Namespace: "default"},
				Configs: []addonv1alpha1.AddOnConfig{v1alpha1.AddOnConfig{
					ConfigGroupResource: v1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
					ConfigReferent:      v1alpha1.ConfigReferent{Name: "test1"}}},
			}).WithInstallProgression(addonv1alpha1.InstallProgression{
				PlacementRef: addonv1alpha1.PlacementRef{Name: "test-placement", Namespace: "default"},
				ConfigReferences: []addonv1alpha1.InstallConfigReference{
					{
						ConfigGroupResource: v1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &v1alpha1.ConfigSpecHash{
							ConfigReferent: v1alpha1.ConfigReferent{Name: "test1"},
							SpecHash:       "hash1",
						},
					},
				},
			}).Build(),
			validateAddonActions: addontesting.AssertNoActions,
		},
		{
			name: "placement rolling update with MaxConcurrency 1",
			managedClusteraddon: []runtime.Object{
				addontesting.NewAddon("test", "cluster1"),
				addontesting.NewAddon("test", "cluster2"),
				addontesting.NewAddon("test", "cluster3"),
			},
			placements: []runtime.Object{
				&clusterv1beta1.Placement{ObjectMeta: metav1.ObjectMeta{Name: "test-placement", Namespace: "default"}},
			},
			placementDecisions: []runtime.Object{
				&clusterv1beta1.PlacementDecision{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-placement",
						Namespace: "default",
						Labels:    map[string]string{clusterv1beta1.PlacementLabel: "test-placement"},
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{{ClusterName: "cluster2"}, {ClusterName: "cluster3"}},
					},
				},
			},
			clusterManagementAddon: addontesting.NewClusterManagementAddon("test", "", "").WithPlacementStrategy(addonv1alpha1.PlacementStrategy{
				PlacementRef: addonv1alpha1.PlacementRef{Name: "test-placement", Namespace: "default"},
				RolloutStrategy: addonv1alpha1.RolloutStrategy{
					Type:          addonv1alpha1.AddonRolloutStrategyRollingUpdate,
					RollingUpdate: &addonv1alpha1.RollingUpdate{MaxConcurrency: intstr.FromInt(1)}},
			}).WithInstallProgression(addonv1alpha1.InstallProgression{
				PlacementRef: addonv1alpha1.PlacementRef{Name: "test-placement", Namespace: "default"},
				ConfigReferences: []addonv1alpha1.InstallConfigReference{
					{
						ConfigGroupResource: v1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &v1alpha1.ConfigSpecHash{
							ConfigReferent: v1alpha1.ConfigReferent{Name: "test1"},
							SpecHash:       "hash1",
						},
					},
				},
			}).Build(),
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				sort.Sort(byPatchName(actions))
				expectPatchConfigurationAction(t, actions[0], []addonv1alpha1.ConfigReference{{
					ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
					ConfigReferent:      addonv1alpha1.ConfigReferent{Name: "test1"},
					DesiredConfig: &addonv1alpha1.ConfigSpecHash{
						ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test1"},
						SpecHash:       "hash1",
					},
					LastObservedGeneration: 0,
				}})
			},
		},
		{
			name: "placement rolling update with MaxConcurrency 0",
			managedClusteraddon: []runtime.Object{
				addontesting.NewAddon("test", "cluster1"),
				addontesting.NewAddon("test", "cluster2"),
				addontesting.NewAddon("test", "cluster3"),
			},
			placements: []runtime.Object{
				&clusterv1beta1.Placement{ObjectMeta: metav1.ObjectMeta{Name: "test-placement", Namespace: "default"}},
			},
			placementDecisions: []runtime.Object{
				&clusterv1beta1.PlacementDecision{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-placement",
						Namespace: "default",
						Labels:    map[string]string{clusterv1beta1.PlacementLabel: "test-placement"},
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{{ClusterName: "cluster2"}, {ClusterName: "cluster3"}},
					},
				},
			},
			clusterManagementAddon: addontesting.NewClusterManagementAddon("test", "", "").WithPlacementStrategy(addonv1alpha1.PlacementStrategy{
				PlacementRef: addonv1alpha1.PlacementRef{Name: "test-placement", Namespace: "default"},
				RolloutStrategy: addonv1alpha1.RolloutStrategy{
					Type:          addonv1alpha1.AddonRolloutStrategyRollingUpdate,
					RollingUpdate: &addonv1alpha1.RollingUpdate{MaxConcurrency: intstr.FromString("0%")}},
			}).WithInstallProgression(addonv1alpha1.InstallProgression{
				PlacementRef: addonv1alpha1.PlacementRef{Name: "test-placement", Namespace: "default"},
				ConfigReferences: []addonv1alpha1.InstallConfigReference{
					{
						ConfigGroupResource: v1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &v1alpha1.ConfigSpecHash{
							ConfigReferent: v1alpha1.ConfigReferent{Name: "test1"},
							SpecHash:       "hash1",
						},
					},
				},
			}).Build(),
			validateAddonActions: addontesting.AssertNoActions,
		},
		{
			name: "placement rolling update with default MaxConcurrency",
			managedClusteraddon: []runtime.Object{
				addontesting.NewAddon("test", "cluster1"),
				addontesting.NewAddon("test", "cluster2"),
				addontesting.NewAddon("test", "cluster3"),
			},
			placements: []runtime.Object{
				&clusterv1beta1.Placement{ObjectMeta: metav1.ObjectMeta{Name: "test-placement", Namespace: "default"}},
			},
			placementDecisions: []runtime.Object{
				&clusterv1beta1.PlacementDecision{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-placement",
						Namespace: "default",
						Labels:    map[string]string{clusterv1beta1.PlacementLabel: "test-placement"},
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{{ClusterName: "cluster2"}, {ClusterName: "cluster3"}},
					},
				},
			},
			clusterManagementAddon: addontesting.NewClusterManagementAddon("test", "", "").WithPlacementStrategy(addonv1alpha1.PlacementStrategy{
				PlacementRef: addonv1alpha1.PlacementRef{Name: "test-placement", Namespace: "default"},
				RolloutStrategy: addonv1alpha1.RolloutStrategy{
					Type:          addonv1alpha1.AddonRolloutStrategyRollingUpdate,
					RollingUpdate: &addonv1alpha1.RollingUpdate{MaxConcurrency: defaultMaxConcurrency}},
			}).WithInstallProgression(addonv1alpha1.InstallProgression{
				PlacementRef: addonv1alpha1.PlacementRef{Name: "test-placement", Namespace: "default"},
				ConfigReferences: []addonv1alpha1.InstallConfigReference{
					{
						ConfigGroupResource: v1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &v1alpha1.ConfigSpecHash{
							ConfigReferent: v1alpha1.ConfigReferent{Name: "test1"},
							SpecHash:       "hash1",
						},
					},
				},
			}).Build(),
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				sort.Sort(byPatchName(actions))
				expectPatchConfigurationAction(t, actions[0], []addonv1alpha1.ConfigReference{{
					ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
					ConfigReferent:      addonv1alpha1.ConfigReferent{Name: "test1"},
					DesiredConfig: &addonv1alpha1.ConfigSpecHash{
						ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test1"},
						SpecHash:       "hash1",
					},
					LastObservedGeneration: 0,
				}})
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterObj := append(c.placements, c.placementDecisions...)
			fakeClusterClient := fakecluster.NewSimpleClientset(clusterObj...)
			fakeAddonClient := fakeaddon.NewSimpleClientset(c.managedClusteraddon...)

			addonInformers := addoninformers.NewSharedInformerFactory(fakeAddonClient, 10*time.Minute)
			clusterInformers := clusterv1informers.NewSharedInformerFactory(fakeClusterClient, 10*time.Minute)

			err := addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Informer().AddIndexers(
				cache.Indexers{
					index.ManagedClusterAddonByName: index.IndexManagedClusterAddonByName,
				})
			if err != nil {
				t.Fatal(err)
			}

			for _, obj := range c.placements {
				if err := clusterInformers.Cluster().V1beta1().Placements().Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}

			for _, obj := range c.placementDecisions {
				if err := clusterInformers.Cluster().V1beta1().PlacementDecisions().Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}

			for _, obj := range c.managedClusteraddon {
				if err := addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}

			controller := &addonConfigurationController{
				addonClient:                  fakeAddonClient,
				placementDecisionLister:      clusterInformers.Cluster().V1beta1().PlacementDecisions().Lister(),
				placementLister:              clusterInformers.Cluster().V1beta1().Placements().Lister(),
				clusterManagementAddonLister: addonInformers.Addon().V1alpha1().ClusterManagementAddOns().Lister(),
				managedClusterAddonIndexer:   addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Informer().GetIndexer(),
			}

			reconcile := &managedClusterAddonConfigurationReconciler{
				addonClient: fakeAddonClient,
			}

			graph, err := controller.buildConfigurationGraph(c.clusterManagementAddon)
			if err != nil {
				t.Errorf("expected no error when build graph: %v", err)
			}

			_, _, err = reconcile.reconcile(context.TODO(), c.clusterManagementAddon, graph)
			if err != nil && !c.expectErr {
				t.Errorf("expected no error when sync: %v", err)
			}
			if err == nil && c.expectErr {
				t.Errorf("Expect error but got no error")
			}

			c.validateAddonActions(t, fakeAddonClient.Actions())
		})
	}
}

// the Age field.
type byPatchName []clienttesting.Action

func (a byPatchName) Len() int      { return len(a) }
func (a byPatchName) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a byPatchName) Less(i, j int) bool {
	patchi := a[i].(clienttesting.PatchActionImpl)
	patchj := a[j].(clienttesting.PatchActionImpl)
	return patchi.Namespace < patchj.Namespace
}

func newManagedClusterAddon(name, namespace string, configs []addonv1alpha1.AddOnConfig, configStatus []addonv1alpha1.ConfigReference) *addonv1alpha1.ManagedClusterAddOn {
	mca := addontesting.NewAddon(name, namespace)
	mca.Spec.Configs = configs
	mca.Status.ConfigReferences = configStatus
	return mca
}

func expectPatchConfigurationAction(t *testing.T, action clienttesting.Action, expected []addonv1alpha1.ConfigReference) {
	patch := action.(clienttesting.PatchActionImpl).GetPatch()
	mca := &addonv1alpha1.ManagedClusterAddOn{}
	err := json.Unmarshal(patch, mca)
	if err != nil {
		t.Fatal(err)
	}

	if !apiequality.Semantic.DeepEqual(mca.Status.ConfigReferences, expected) {
		t.Errorf("Configuration not correctly patched, expected %v, actual %v", expected, mca.Status.ConfigReferences)
	}
}
