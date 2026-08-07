package addonconfiguration

import (
	"context"
	"encoding/json"
	"sort"
	"testing"
	"time"

	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"

	"open-cluster-management.io/addon-framework/pkg/addonmanager/addontesting"
	addonv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	fakeaddon "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	fakecluster "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterv1informers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"

	addonindex "open-cluster-management.io/ocm/pkg/addon/index"
	"open-cluster-management.io/ocm/pkg/common/helpers"
)

func TestAddonConfigReconcile(t *testing.T) {
	cases := []struct {
		name                   string
		managedClusteraddon    []runtime.Object
		clusterManagementAddon *addonv1beta1.ClusterManagementAddOn
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
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch", "patch")
				sort.Sort(byPatchName(actions))
				expectPatchConditionAction(t, actions[0], metav1.ConditionTrue)
				expectPatchConditionAction(t, actions[1], metav1.ConditionTrue)
			},
		},
		{
			name: "manual installStrategy",
			managedClusteraddon: []runtime.Object{
				addontesting.NewAddon("test", "cluster1"),
				addontesting.NewAddon("test", "cluster2"),
			},
			clusterManagementAddon: addontesting.NewClusterManagementAddon("test", "", "").
				WithDefaultConfigs(addonv1beta1.AddOnConfig{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
					ConfigReferent:      addonv1beta1.ConfigReferent{Name: "test"}}).
				WithDefaultConfigReferences(addonv1beta1.DefaultConfigReference{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
					DesiredConfig: &addonv1beta1.ConfigSpecHash{
						ConfigReferent: addonv1beta1.ConfigReferent{Name: "test"},
						SpecHash:       "hash",
					},
				}).Build(),
			placements:         []runtime.Object{},
			placementDecisions: []runtime.Object{},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch", "patch")
				sort.Sort(byPatchName(actions))
				expectPatchConfigurationAction(t, actions[0], []addonv1beta1.ConfigReference{{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
					DesiredConfig: &addonv1beta1.ConfigSpecHash{
						ConfigReferent: addonv1beta1.ConfigReferent{Name: "test"},
						SpecHash:       "hash",
					},
					LastObservedGeneration: 0,
				}})
				expectPatchConfigurationAction(t, actions[1], []addonv1beta1.ConfigReference{{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
					DesiredConfig: &addonv1beta1.ConfigSpecHash{
						ConfigReferent: addonv1beta1.ConfigReferent{Name: "test"},
						SpecHash:       "hash",
					},
					LastObservedGeneration: 0,
				}})
				expectPatchConditionAction(t, actions[0], metav1.ConditionTrue)
				expectPatchConditionAction(t, actions[1], metav1.ConditionTrue)
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
						Labels: map[string]string{
							clusterv1beta1.PlacementLabel:          "test-placement",
							clusterv1beta1.DecisionGroupIndexLabel: "0",
						},
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{{ClusterName: "cluster2"}},
					},
				},
			},
			clusterManagementAddon: addontesting.NewClusterManagementAddon("test", "", "").
				WithDefaultConfigs(addonv1beta1.AddOnConfig{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
					ConfigReferent:      addonv1beta1.ConfigReferent{Name: "test"}}).
				WithDefaultConfigReferences(addonv1beta1.DefaultConfigReference{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
					DesiredConfig: &addonv1beta1.ConfigSpecHash{
						ConfigReferent: addonv1beta1.ConfigReferent{Name: "test"},
						SpecHash:       "hash",
					},
				}).WithPlacementStrategy(addonv1beta1.PlacementStrategy{
				PlacementRef:    addonv1beta1.PlacementRef{Name: "test-placement", Namespace: "default"},
				RolloutStrategy: clusterv1alpha1.RolloutStrategy{Type: clusterv1alpha1.All},
			}).WithInstallProgression(addonv1beta1.InstallProgression{
				PlacementRef: addonv1beta1.PlacementRef{Name: "test-placement", Namespace: "default"},
				ConfigReferences: []addonv1beta1.InstallConfigReference{
					{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "test1"},
							SpecHash:       "hash1",
						},
					},
				},
			}).Build(),
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch", "patch")
				sort.Sort(byPatchName(actions))
				expectPatchConfigurationAction(t, actions[0], []addonv1beta1.ConfigReference{{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
					DesiredConfig: &addonv1beta1.ConfigSpecHash{
						ConfigReferent: addonv1beta1.ConfigReferent{Name: "test"},
						SpecHash:       "hash",
					},
					LastObservedGeneration: 0,
				}})
				expectPatchConfigurationAction(t, actions[1], []addonv1beta1.ConfigReference{{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
					DesiredConfig: &addonv1beta1.ConfigSpecHash{
						ConfigReferent: addonv1beta1.ConfigReferent{Name: "test1"},
						SpecHash:       "hash1",
					},
					LastObservedGeneration: 0,
				}})
				expectPatchConditionAction(t, actions[0], metav1.ConditionTrue)
				expectPatchConditionAction(t, actions[1], metav1.ConditionTrue)
			},
		},
		{
			name: "cma install strategy override with multiple same-GVK",
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
						Labels: map[string]string{
							clusterv1beta1.PlacementLabel:          "test-placement",
							clusterv1beta1.DecisionGroupIndexLabel: "0",
						},
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{{ClusterName: "cluster2"}},
					},
				},
			},
			clusterManagementAddon: addontesting.NewClusterManagementAddon("test", "", "").
				WithDefaultConfigs(addonv1beta1.AddOnConfig{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
					ConfigReferent:      addonv1beta1.ConfigReferent{Name: "test"}},
					addonv1beta1.AddOnConfig{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Bar"},
						ConfigReferent:      addonv1beta1.ConfigReferent{Name: "test"}}).
				WithDefaultConfigReferences(
					addonv1beta1.DefaultConfigReference{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "test"},
							SpecHash:       "hash",
						},
					},
					addonv1beta1.DefaultConfigReference{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Bar"},
						DesiredConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "test"},
							SpecHash:       "hash",
						},
					},
				).WithPlacementStrategy(addonv1beta1.PlacementStrategy{
				PlacementRef:    addonv1beta1.PlacementRef{Name: "test-placement", Namespace: "default"},
				RolloutStrategy: clusterv1alpha1.RolloutStrategy{Type: clusterv1alpha1.All},
			}).WithInstallProgression(addonv1beta1.InstallProgression{
				PlacementRef: addonv1beta1.PlacementRef{Name: "test-placement", Namespace: "default"},
				ConfigReferences: []addonv1beta1.InstallConfigReference{
					{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "test1"},
							SpecHash:       "hash1",
						},
					},
					{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "test2"},
							SpecHash:       "hash2",
						},
					},
				},
			}).Build(),
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch", "patch")
				sort.Sort(byPatchName(actions))
				expectPatchConfigurationAction(t, actions[0], []addonv1beta1.ConfigReference{
					{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Bar"},
						DesiredConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "test"},
							SpecHash:       "hash",
						},
						LastObservedGeneration: 0,
					},
					{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "test"},
							SpecHash:       "hash",
						},
						LastObservedGeneration: 0,
					},
				})
				expectPatchConfigurationAction(t, actions[1], []addonv1beta1.ConfigReference{
					{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Bar"},
						DesiredConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "test"},
							SpecHash:       "hash",
						},
						LastObservedGeneration: 0,
					},
					{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "test1"},
							SpecHash:       "hash1",
						},
						LastObservedGeneration: 0,
					},
					{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "test2"},
							SpecHash:       "hash2",
						},
						LastObservedGeneration: 0,
					},
				})
				expectPatchConditionAction(t, actions[0], metav1.ConditionTrue)
				expectPatchConditionAction(t, actions[1], metav1.ConditionTrue)
			},
		},
		{
			name: "mca override with multiple same-GVK",
			managedClusteraddon: []runtime.Object{
				newManagedClusterAddon("test", "cluster1", []addonv1beta1.AddOnConfig{
					{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						ConfigReferent:      addonv1beta1.ConfigReferent{Name: "test2"},
					},
					{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						ConfigReferent:      addonv1beta1.ConfigReferent{Name: "test3"},
					},
				}, nil, nil),
				addontesting.NewAddon("test", "cluster2"),
				// cluster3 already has configs synced to status before spec hash is updated
				newManagedClusterAddon("test", "cluster3", []addonv1beta1.AddOnConfig{
					{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						ConfigReferent:      addonv1beta1.ConfigReferent{Name: "test2"},
					},
					{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						ConfigReferent:      addonv1beta1.ConfigReferent{Name: "test3"},
					},
				}, []addonv1beta1.ConfigReference{
					{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Bar"},
						DesiredConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "test1"},
							SpecHash:       "",
						},
						LastObservedGeneration: 0,
					},
					{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "test2"},
							SpecHash:       "",
						},
						LastObservedGeneration: 0,
					},
					{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "test3"},
							SpecHash:       "",
						},
						LastObservedGeneration: 0,
					},
				}, nil),
			},
			placements: []runtime.Object{
				&clusterv1beta1.Placement{ObjectMeta: metav1.ObjectMeta{Name: "test-placement", Namespace: "default"}},
			},
			placementDecisions: []runtime.Object{
				&clusterv1beta1.PlacementDecision{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-placement",
						Namespace: "default",
						Labels: map[string]string{
							clusterv1beta1.PlacementLabel:          "test-placement",
							clusterv1beta1.DecisionGroupIndexLabel: "0",
						},
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{{ClusterName: "cluster1"}, {ClusterName: "cluster2"}, {ClusterName: "cluster3"}},
					},
				},
			},
			clusterManagementAddon: addontesting.NewClusterManagementAddon("test", "", "").WithDefaultConfigReferences(addonv1beta1.DefaultConfigReference{
				ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
				DesiredConfig: &addonv1beta1.ConfigSpecHash{
					ConfigReferent: addonv1beta1.ConfigReferent{Name: "test"},
					SpecHash:       "<core-foo-test-hash>",
				},
			}).WithPlacementStrategy(addonv1beta1.PlacementStrategy{
				PlacementRef:    addonv1beta1.PlacementRef{Name: "test-placement", Namespace: "default"},
				RolloutStrategy: clusterv1alpha1.RolloutStrategy{Type: clusterv1alpha1.All},
			}).WithInstallProgression(addonv1beta1.InstallProgression{
				PlacementRef: addonv1beta1.PlacementRef{Name: "test-placement", Namespace: "default"},
				ConfigReferences: []addonv1beta1.InstallConfigReference{
					{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Bar"},
						DesiredConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "test1"},
							SpecHash:       "<core-bar-test1-hash>",
						},
					},
					{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "test1"},
							SpecHash:       "<core-foo-test1-hash>",
						},
					},
				},
			}).Build(),
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch", "patch", "patch")
				sort.Sort(byPatchName(actions))
				expectPatchConfigurationAction(t, actions[0], []addonv1beta1.ConfigReference{
					{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Bar"},
						DesiredConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "test1"},
							SpecHash:       "<core-bar-test1-hash>",
						},
						LastObservedGeneration: 0,
					},
					{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "test2"},
							SpecHash:       "",
						},
						LastObservedGeneration: 0,
					},
					{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "test3"},
							SpecHash:       "",
						},
						LastObservedGeneration: 0,
					},
				})
				expectPatchConfigurationAction(t, actions[1], []addonv1beta1.ConfigReference{
					{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Bar"},
						DesiredConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "test1"},
							SpecHash:       "<core-bar-test1-hash>",
						},
						LastObservedGeneration: 0,
					},
					{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "test1"},
							SpecHash:       "<core-foo-test1-hash>",
						},
						LastObservedGeneration: 0,
					}})
				expectPatchConfigurationAction(t, actions[2], []addonv1beta1.ConfigReference{
					{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Bar"},
						DesiredConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "test1"},
							SpecHash:       "<core-bar-test1-hash>",
						},
						LastObservedGeneration: 0,
					},
					{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "test2"},
							SpecHash:       "",
						},
						LastObservedGeneration: 0,
					},
					{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "test3"},
							SpecHash:       "",
						},
						LastObservedGeneration: 0,
					},
				})
				expectPatchConditionAction(t, actions[0], metav1.ConditionTrue)
				expectPatchConditionAction(t, actions[1], metav1.ConditionTrue)
			},
		},
		{
			name: "reordering spec.configs triggers status update",
			managedClusteraddon: []runtime.Object{
				// spec.configs has test3 before test2 (reversed from status)
				newManagedClusterAddon("test", "cluster1", []addonv1beta1.AddOnConfig{
					{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						ConfigReferent:      addonv1beta1.ConfigReferent{Name: "test3"},
					},
					{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						ConfigReferent:      addonv1beta1.ConfigReferent{Name: "test2"},
					},
				}, []addonv1beta1.ConfigReference{
					// status has test2 before test3 (old order)
					{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "test2"},
							SpecHash:       "",
						},
						LastAppliedConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "test2"},
							SpecHash:       "",
						},
						LastObservedGeneration: 1,
					},
					{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "test3"},
							SpecHash:       "",
						},
						LastAppliedConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "test3"},
							SpecHash:       "",
						},
						LastObservedGeneration: 1,
					},
				}, []metav1.Condition{
					{
						Type:   addonv1beta1.ManagedClusterAddOnConditionProgressing,
						Reason: addonv1beta1.ProgressingReasonCompleted,
					},
				}),
			},
			placements: []runtime.Object{
				&clusterv1beta1.Placement{ObjectMeta: metav1.ObjectMeta{Name: "test-placement", Namespace: "default"}},
			},
			placementDecisions: []runtime.Object{
				&clusterv1beta1.PlacementDecision{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-placement",
						Namespace: "default",
						Labels: map[string]string{
							clusterv1beta1.PlacementLabel:          "test-placement",
							clusterv1beta1.DecisionGroupIndexLabel: "0",
						},
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{{ClusterName: "cluster1"}},
					},
				},
			},
			clusterManagementAddon: addontesting.NewClusterManagementAddon("test", "", "").WithPlacementStrategy(addonv1beta1.PlacementStrategy{
				PlacementRef:    addonv1beta1.PlacementRef{Name: "test-placement", Namespace: "default"},
				RolloutStrategy: clusterv1alpha1.RolloutStrategy{Type: clusterv1alpha1.All},
			}).WithInstallProgression(addonv1beta1.InstallProgression{
				PlacementRef: addonv1beta1.PlacementRef{Name: "test-placement", Namespace: "default"},
			}).Build(),
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				// Status should be reordered to match spec: test3 first, then test2.
				// LastAppliedConfig and LastObservedGeneration are preserved.
				expectPatchConfigurationAction(t, actions[0], []addonv1beta1.ConfigReference{
					{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "test3"},
							SpecHash:       "",
						},
						LastAppliedConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "test3"},
							SpecHash:       "",
						},
						LastObservedGeneration: 1,
					},
					{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "test2"},
							SpecHash:       "",
						},
						LastAppliedConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "test2"},
							SpecHash:       "",
						},
						LastObservedGeneration: 1,
					},
				})
				expectPatchConditionAction(t, actions[0], metav1.ConditionTrue)
			},
		},
		{
			name: "duplicate configs",
			managedClusteraddon: []runtime.Object{
				newManagedClusterAddon("test", "cluster1", []addonv1beta1.AddOnConfig{
					{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						ConfigReferent:      addonv1beta1.ConfigReferent{Name: "test2"},
					},
					{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						ConfigReferent:      addonv1beta1.ConfigReferent{Name: "test2"},
					},
				}, nil, nil),
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
						Labels: map[string]string{
							clusterv1beta1.PlacementLabel:          "test-placement",
							clusterv1beta1.DecisionGroupIndexLabel: "0",
						},
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{{ClusterName: "cluster1"}, {ClusterName: "cluster2"}},
					},
				},
			},
			clusterManagementAddon: addontesting.NewClusterManagementAddon("test", "", "").WithPlacementStrategy(addonv1beta1.PlacementStrategy{
				PlacementRef:    addonv1beta1.PlacementRef{Name: "test-placement", Namespace: "default"},
				RolloutStrategy: clusterv1alpha1.RolloutStrategy{Type: clusterv1alpha1.All},
			}).WithInstallProgression(addonv1beta1.InstallProgression{
				PlacementRef: addonv1beta1.PlacementRef{Name: "test-placement", Namespace: "default"},
				ConfigReferences: []addonv1beta1.InstallConfigReference{
					{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "test1"},
							SpecHash:       "hash1",
						},
					},
					{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "test1"},
							SpecHash:       "hash1",
						},
					},
				},
			}).Build(),
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch", "patch")
				expectPatchConfigurationAction(t, actions[0], []addonv1beta1.ConfigReference{{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
					DesiredConfig: &addonv1beta1.ConfigSpecHash{
						ConfigReferent: addonv1beta1.ConfigReferent{Name: "test2"},
						SpecHash:       "",
					},
					LastObservedGeneration: 0,
				}})
				expectPatchConfigurationAction(t, actions[1], []addonv1beta1.ConfigReference{{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
					DesiredConfig: &addonv1beta1.ConfigSpecHash{
						ConfigReferent: addonv1beta1.ConfigReferent{Name: "test1"},
						SpecHash:       "hash1",
					},
					LastObservedGeneration: 0,
				}})
				expectPatchConditionAction(t, actions[0], metav1.ConditionTrue)
				expectPatchConditionAction(t, actions[1], metav1.ConditionTrue)
			},
		},
		{
			name: "config name/namespce change",
			managedClusteraddon: []runtime.Object{
				newManagedClusterAddon("test", "cluster1", []addonv1beta1.AddOnConfig{}, []addonv1beta1.ConfigReference{{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
					DesiredConfig: &addonv1beta1.ConfigSpecHash{
						ConfigReferent: addonv1beta1.ConfigReferent{Name: "test1"},
						SpecHash:       "hash1",
					},
					LastAppliedConfig: &addonv1beta1.ConfigSpecHash{
						ConfigReferent: addonv1beta1.ConfigReferent{Name: "test1"},
						SpecHash:       "hash1",
					},
					LastObservedGeneration: 1,
				}}, nil),
			},
			placements: []runtime.Object{
				&clusterv1beta1.Placement{ObjectMeta: metav1.ObjectMeta{Name: "test-placement", Namespace: "default"}},
			},
			placementDecisions: []runtime.Object{
				&clusterv1beta1.PlacementDecision{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-placement",
						Namespace: "default",
						Labels: map[string]string{
							clusterv1beta1.PlacementLabel:          "test-placement",
							clusterv1beta1.DecisionGroupIndexLabel: "0",
						},
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{{ClusterName: "cluster1"}},
					},
				},
			},
			clusterManagementAddon: addontesting.NewClusterManagementAddon("test", "", "").WithPlacementStrategy(addonv1beta1.PlacementStrategy{
				PlacementRef:    addonv1beta1.PlacementRef{Name: "test-placement", Namespace: "default"},
				RolloutStrategy: clusterv1alpha1.RolloutStrategy{Type: clusterv1alpha1.All},
			}).WithInstallProgression(addonv1beta1.InstallProgression{
				PlacementRef: addonv1beta1.PlacementRef{Name: "test-placement", Namespace: "default"},
				ConfigReferences: []addonv1beta1.InstallConfigReference{
					{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "test2"},
							SpecHash:       "hash2",
						},
					},
				},
			}).Build(),
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				expectPatchConfigurationAction(t, actions[0], []addonv1beta1.ConfigReference{{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
					DesiredConfig: &addonv1beta1.ConfigSpecHash{
						ConfigReferent: addonv1beta1.ConfigReferent{Name: "test2"},
						SpecHash:       "hash2",
					},
					LastObservedGeneration: 0,
				}})
				expectPatchConditionAction(t, actions[0], metav1.ConditionTrue)
			},
		},
		{
			name: "config spec hash change",
			managedClusteraddon: []runtime.Object{
				newManagedClusterAddon("test", "cluster1", []addonv1beta1.AddOnConfig{}, []addonv1beta1.ConfigReference{{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
					DesiredConfig: &addonv1beta1.ConfigSpecHash{
						ConfigReferent: addonv1beta1.ConfigReferent{Name: "test1"},
						SpecHash:       "hash1",
					},
					LastAppliedConfig: &addonv1beta1.ConfigSpecHash{
						ConfigReferent: addonv1beta1.ConfigReferent{Name: "test1"},
						SpecHash:       "hash1",
					},
					LastObservedGeneration: 1,
				}}, nil),
			},
			placements: []runtime.Object{
				&clusterv1beta1.Placement{ObjectMeta: metav1.ObjectMeta{Name: "test-placement", Namespace: "default"}},
			},
			placementDecisions: []runtime.Object{
				&clusterv1beta1.PlacementDecision{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-placement",
						Namespace: "default",
						Labels: map[string]string{
							clusterv1beta1.PlacementLabel:          "test-placement",
							clusterv1beta1.DecisionGroupIndexLabel: "0",
						},
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{{ClusterName: "cluster1"}},
					},
				},
			},
			clusterManagementAddon: addontesting.NewClusterManagementAddon("test", "", "").WithPlacementStrategy(addonv1beta1.PlacementStrategy{
				PlacementRef:    addonv1beta1.PlacementRef{Name: "test-placement", Namespace: "default"},
				RolloutStrategy: clusterv1alpha1.RolloutStrategy{Type: clusterv1alpha1.All},
				Configs: []addonv1beta1.AddOnConfig{{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
					ConfigReferent:      addonv1beta1.ConfigReferent{Name: "test1"}}},
			}).WithInstallProgression(addonv1beta1.InstallProgression{
				PlacementRef: addonv1beta1.PlacementRef{Name: "test-placement", Namespace: "default"},
				ConfigReferences: []addonv1beta1.InstallConfigReference{
					{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "test1"},
							SpecHash:       "hash1new",
						},
					},
				},
			}).Build(),
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				expectPatchConfigurationAction(t, actions[0], []addonv1beta1.ConfigReference{{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
					DesiredConfig: &addonv1beta1.ConfigSpecHash{
						ConfigReferent: addonv1beta1.ConfigReferent{Name: "test1"},
						SpecHash:       "hash1new",
					},
					LastAppliedConfig: &addonv1beta1.ConfigSpecHash{
						ConfigReferent: addonv1beta1.ConfigReferent{Name: "test1"},
						SpecHash:       "hash1",
					},
					LastObservedGeneration: 1,
				}})
				expectPatchConditionAction(t, actions[0], metav1.ConditionTrue)
			},
		},
		{
			name: "mca noop",
			managedClusteraddon: []runtime.Object{
				newManagedClusterAddon("test", "cluster1", []addonv1beta1.AddOnConfig{}, []addonv1beta1.ConfigReference{{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
					DesiredConfig: &addonv1beta1.ConfigSpecHash{
						ConfigReferent: addonv1beta1.ConfigReferent{Name: "test1"},
						SpecHash:       "hash1",
					},
					LastObservedGeneration: 1,
				}}, []metav1.Condition{{
					Type:    addonv1beta1.ManagedClusterAddOnConditionConfigured,
					Status:  metav1.ConditionTrue,
					Reason:  "ConfigurationsConfigured",
					Message: "Configurations configured",
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
						Labels: map[string]string{
							clusterv1beta1.PlacementLabel:          "test-placement",
							clusterv1beta1.DecisionGroupIndexLabel: "0",
						},
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{{ClusterName: "cluster1"}},
					},
				},
			},
			clusterManagementAddon: addontesting.NewClusterManagementAddon("test", "", "").WithPlacementStrategy(addonv1beta1.PlacementStrategy{
				PlacementRef: addonv1beta1.PlacementRef{Name: "test-placement", Namespace: "default"},
				Configs: []addonv1beta1.AddOnConfig{{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
					ConfigReferent:      addonv1beta1.ConfigReferent{Name: "test1"}}},
				RolloutStrategy: clusterv1alpha1.RolloutStrategy{Type: clusterv1alpha1.All},
			}).WithInstallProgression(addonv1beta1.InstallProgression{
				PlacementRef: addonv1beta1.PlacementRef{Name: "test-placement", Namespace: "default"},
				ConfigReferences: []addonv1beta1.InstallConfigReference{
					{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "test1"},
							SpecHash:       "hash1",
						},
					},
				},
			}).Build(),
			validateAddonActions: addontesting.AssertNoActions,
		},
		{
			name: "placement rollout progressive with MaxConcurrency 1",
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
						Labels: map[string]string{
							clusterv1beta1.PlacementLabel:          "test-placement",
							clusterv1beta1.DecisionGroupIndexLabel: "0",
						},
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{{ClusterName: "cluster2"}, {ClusterName: "cluster3"}},
					},
				},
			},
			clusterManagementAddon: addontesting.NewClusterManagementAddon("test", "", "").WithPlacementStrategy(addonv1beta1.PlacementStrategy{
				PlacementRef: addonv1beta1.PlacementRef{Name: "test-placement", Namespace: "default"},
				RolloutStrategy: clusterv1alpha1.RolloutStrategy{
					Type:        clusterv1alpha1.Progressive,
					Progressive: &clusterv1alpha1.RolloutProgressive{MaxConcurrency: intstr.FromInt(1)},
				},
			}).WithInstallProgression(addonv1beta1.InstallProgression{
				PlacementRef: addonv1beta1.PlacementRef{Name: "test-placement", Namespace: "default"},
				ConfigReferences: []addonv1beta1.InstallConfigReference{
					{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "test1"},
							SpecHash:       "hash1",
						},
					},
				},
			}).Build(),
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch", "patch", "patch")
				sort.Sort(byPatchName(actions))
				// cluster1 is not in installstrategy and has no config
				expectPatchConditionAction(t, actions[0], metav1.ConditionTrue)
				// cluster2 is in installstrategy and is the first to rollout
				expectPatchConfigurationAction(t, actions[1], []addonv1beta1.ConfigReference{{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
					DesiredConfig: &addonv1beta1.ConfigSpecHash{
						ConfigReferent: addonv1beta1.ConfigReferent{Name: "test1"},
						SpecHash:       "hash1",
					},
					LastObservedGeneration: 0,
				}})
				expectPatchConditionAction(t, actions[1], metav1.ConditionTrue)
				// cluster3 is in installstrategy and is not rollout
				expectPatchConditionAction(t, actions[2], metav1.ConditionFalse)
			},
		},
		{
			name: "placement rollout progressive with MaxConcurrency 50%",
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
						Labels: map[string]string{
							clusterv1beta1.PlacementLabel:          "test-placement",
							clusterv1beta1.DecisionGroupIndexLabel: "0",
						},
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{{ClusterName: "cluster2"}, {ClusterName: "cluster3"}},
					},
				},
			},
			clusterManagementAddon: addontesting.NewClusterManagementAddon("test", "", "").WithPlacementStrategy(addonv1beta1.PlacementStrategy{
				PlacementRef: addonv1beta1.PlacementRef{Name: "test-placement", Namespace: "default"},
				RolloutStrategy: clusterv1alpha1.RolloutStrategy{
					Type:        clusterv1alpha1.Progressive,
					Progressive: &clusterv1alpha1.RolloutProgressive{MaxConcurrency: intstr.FromString("50%")},
				},
			}).WithInstallProgression(addonv1beta1.InstallProgression{
				PlacementRef: addonv1beta1.PlacementRef{Name: "test-placement", Namespace: "default"},
				ConfigReferences: []addonv1beta1.InstallConfigReference{
					{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "test1"},
							SpecHash:       "hash1",
						},
					},
				},
			}).Build(),
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch", "patch", "patch")
				sort.Sort(byPatchName(actions))
				// cluster1 is not in installstrategy and has no config
				expectPatchConditionAction(t, actions[0], metav1.ConditionTrue)
				// cluster2 is in installstrategy and is the first to rollout
				expectPatchConfigurationAction(t, actions[1], []addonv1beta1.ConfigReference{{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
					DesiredConfig: &addonv1beta1.ConfigSpecHash{
						ConfigReferent: addonv1beta1.ConfigReferent{Name: "test1"},
						SpecHash:       "hash1",
					},
					LastObservedGeneration: 0,
				}})
				expectPatchConditionAction(t, actions[1], metav1.ConditionTrue)
				// cluster3 is in installstrategy and is not rollout
				expectPatchConditionAction(t, actions[2], metav1.ConditionFalse)
			},
		},
		{
			name: "placement rollout progressive with default MaxConcurrency 100%",
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
						Labels: map[string]string{
							clusterv1beta1.PlacementLabel:          "test-placement",
							clusterv1beta1.DecisionGroupIndexLabel: "0",
						},
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{{ClusterName: "cluster2"}, {ClusterName: "cluster3"}},
					},
				},
			},
			clusterManagementAddon: addontesting.NewClusterManagementAddon("test", "", "").WithPlacementStrategy(addonv1beta1.PlacementStrategy{
				PlacementRef:    addonv1beta1.PlacementRef{Name: "test-placement", Namespace: "default"},
				RolloutStrategy: clusterv1alpha1.RolloutStrategy{Type: clusterv1alpha1.Progressive},
			}).WithInstallProgression(addonv1beta1.InstallProgression{
				PlacementRef: addonv1beta1.PlacementRef{Name: "test-placement", Namespace: "default"},
				ConfigReferences: []addonv1beta1.InstallConfigReference{
					{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "test1"},
							SpecHash:       "hash1",
						},
					},
				},
			}).Build(),
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch", "patch", "patch")
				sort.Sort(byPatchName(actions))
				// cluster1 is not in installstrategy and has no config
				expectPatchConditionAction(t, actions[0], metav1.ConditionTrue)
				// cluster2 is in installstrategy and rollout
				expectPatchConfigurationAction(t, actions[1], []addonv1beta1.ConfigReference{{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
					DesiredConfig: &addonv1beta1.ConfigSpecHash{
						ConfigReferent: addonv1beta1.ConfigReferent{Name: "test1"},
						SpecHash:       "hash1",
					},
					LastObservedGeneration: 0,
				}})
				expectPatchConditionAction(t, actions[1], metav1.ConditionTrue)
				// cluster2 is in installstrategy and rollout
				expectPatchConfigurationAction(t, actions[2], []addonv1beta1.ConfigReference{{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
					DesiredConfig: &addonv1beta1.ConfigSpecHash{
						ConfigReferent: addonv1beta1.ConfigReferent{Name: "test1"},
						SpecHash:       "hash1",
					},
					LastObservedGeneration: 0,
				}})
				expectPatchConditionAction(t, actions[2], metav1.ConditionTrue)
			},
		},
		{
			name: "placement rollout progressive with mandatory decision groups",
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
						Name:      "test-placement-0",
						Namespace: "default",
						Labels: map[string]string{
							clusterv1beta1.PlacementLabel:          "test-placement",
							clusterv1beta1.DecisionGroupNameLabel:  "group1",
							clusterv1beta1.DecisionGroupIndexLabel: "0",
						},
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{{ClusterName: "cluster1"}, {ClusterName: "cluster2"}},
					},
				},
				&clusterv1beta1.PlacementDecision{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-placement-1",
						Namespace: "default",
						Labels: map[string]string{
							clusterv1beta1.PlacementLabel:          "test-placement",
							clusterv1beta1.DecisionGroupIndexLabel: "1",
						},
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{{ClusterName: "cluster3"}},
					},
				},
			},
			clusterManagementAddon: addontesting.NewClusterManagementAddon("test", "", "").WithPlacementStrategy(
				addonv1beta1.PlacementStrategy{
					PlacementRef: addonv1beta1.PlacementRef{Name: "test-placement", Namespace: "default"},
					RolloutStrategy: clusterv1alpha1.RolloutStrategy{
						Type: clusterv1alpha1.Progressive,
						Progressive: &clusterv1alpha1.RolloutProgressive{
							MandatoryDecisionGroups: clusterv1alpha1.MandatoryDecisionGroups{
								MandatoryDecisionGroups: []clusterv1alpha1.MandatoryDecisionGroup{
									{GroupName: "group1"},
								},
							},
						},
					}}).WithInstallProgression(addonv1beta1.InstallProgression{
				PlacementRef: addonv1beta1.PlacementRef{Name: "test-placement", Namespace: "default"},
				ConfigReferences: []addonv1beta1.InstallConfigReference{
					{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "test1"},
							SpecHash:       "hash1",
						},
					},
				},
			}).Build(),
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch", "patch", "patch")
				sort.Sort(byPatchName(actions))
				// cluster1 and cluster2 are rollout
				expectPatchConfigurationAction(t, actions[0], []addonv1beta1.ConfigReference{{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
					DesiredConfig: &addonv1beta1.ConfigSpecHash{
						ConfigReferent: addonv1beta1.ConfigReferent{Name: "test1"},
						SpecHash:       "hash1",
					},
					LastObservedGeneration: 0,
				}})
				expectPatchConfigurationAction(t, actions[1], []addonv1beta1.ConfigReference{{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
					DesiredConfig: &addonv1beta1.ConfigSpecHash{
						ConfigReferent: addonv1beta1.ConfigReferent{Name: "test1"},
						SpecHash:       "hash1",
					},
					LastObservedGeneration: 0,
				}})
				expectPatchConditionAction(t, actions[0], metav1.ConditionTrue)
				expectPatchConditionAction(t, actions[1], metav1.ConditionTrue)
				expectPatchConditionAction(t, actions[2], metav1.ConditionFalse)
			},
		},
		{
			name: "placement rollout progressive per group",
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
						Name:      "test-placement-0",
						Namespace: "default",
						Labels: map[string]string{
							clusterv1beta1.PlacementLabel:          "test-placement",
							clusterv1beta1.DecisionGroupIndexLabel: "0",
						},
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{{ClusterName: "cluster1"}, {ClusterName: "cluster2"}},
					},
				},
				&clusterv1beta1.PlacementDecision{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-placement-1",
						Namespace: "default",
						Labels: map[string]string{
							clusterv1beta1.PlacementLabel:          "test-placement",
							clusterv1beta1.DecisionGroupIndexLabel: "1",
						},
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{{ClusterName: "cluster3"}},
					},
				},
			},
			clusterManagementAddon: addontesting.NewClusterManagementAddon("test", "", "").WithPlacementStrategy(
				addonv1beta1.PlacementStrategy{
					PlacementRef: addonv1beta1.PlacementRef{Name: "test-placement", Namespace: "default"},
					RolloutStrategy: clusterv1alpha1.RolloutStrategy{
						Type: clusterv1alpha1.ProgressivePerGroup},
				}).WithInstallProgression(addonv1beta1.InstallProgression{
				PlacementRef: addonv1beta1.PlacementRef{Name: "test-placement", Namespace: "default"},
				ConfigReferences: []addonv1beta1.InstallConfigReference{
					{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "test1"},
							SpecHash:       "hash1",
						},
					},
				},
			}).Build(),
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch", "patch", "patch")
				sort.Sort(byPatchName(actions))
				// cluster1 and cluster2 are rollout
				expectPatchConfigurationAction(t, actions[0], []addonv1beta1.ConfigReference{{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
					DesiredConfig: &addonv1beta1.ConfigSpecHash{
						ConfigReferent: addonv1beta1.ConfigReferent{Name: "test1"},
						SpecHash:       "hash1",
					},
					LastObservedGeneration: 0,
				}})
				expectPatchConfigurationAction(t, actions[1], []addonv1beta1.ConfigReference{{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
					DesiredConfig: &addonv1beta1.ConfigSpecHash{
						ConfigReferent: addonv1beta1.ConfigReferent{Name: "test1"},
						SpecHash:       "hash1",
					},
					LastObservedGeneration: 0,
				}})
				expectPatchConditionAction(t, actions[0], metav1.ConditionTrue)
				expectPatchConditionAction(t, actions[1], metav1.ConditionTrue)
				expectPatchConditionAction(t, actions[2], metav1.ConditionFalse)
			},
		},
		{
			name: "placement rollout progressive per group with mandatory decision groups",
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
						Name:      "test-placement-0",
						Namespace: "default",
						Labels: map[string]string{
							clusterv1beta1.PlacementLabel:          "test-placement",
							clusterv1beta1.DecisionGroupNameLabel:  "group1",
							clusterv1beta1.DecisionGroupIndexLabel: "0",
						},
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{{ClusterName: "cluster1"}, {ClusterName: "cluster2"}},
					},
				},
				&clusterv1beta1.PlacementDecision{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-placement-1",
						Namespace: "default",
						Labels: map[string]string{
							clusterv1beta1.PlacementLabel:          "test-placement",
							clusterv1beta1.DecisionGroupIndexLabel: "1",
						},
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{{ClusterName: "cluster3"}},
					},
				},
			},
			clusterManagementAddon: addontesting.NewClusterManagementAddon("test", "", "").WithPlacementStrategy(
				addonv1beta1.PlacementStrategy{
					PlacementRef: addonv1beta1.PlacementRef{Name: "test-placement", Namespace: "default"},
					RolloutStrategy: clusterv1alpha1.RolloutStrategy{
						Type: clusterv1alpha1.ProgressivePerGroup,
						ProgressivePerGroup: &clusterv1alpha1.RolloutProgressivePerGroup{
							MandatoryDecisionGroups: clusterv1alpha1.MandatoryDecisionGroups{
								MandatoryDecisionGroups: []clusterv1alpha1.MandatoryDecisionGroup{
									{GroupName: "group1"},
								},
							},
						},
					}}).WithInstallProgression(addonv1beta1.InstallProgression{
				PlacementRef: addonv1beta1.PlacementRef{Name: "test-placement", Namespace: "default"},
				ConfigReferences: []addonv1beta1.InstallConfigReference{
					{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "test1"},
							SpecHash:       "hash1",
						},
					},
				},
			}).Build(),
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch", "patch", "patch")
				sort.Sort(byPatchName(actions))
				// cluster1 and cluster2 are rollout
				expectPatchConfigurationAction(t, actions[0], []addonv1beta1.ConfigReference{{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
					DesiredConfig: &addonv1beta1.ConfigSpecHash{
						ConfigReferent: addonv1beta1.ConfigReferent{Name: "test1"},
						SpecHash:       "hash1",
					},
					LastObservedGeneration: 0,
				}})
				expectPatchConfigurationAction(t, actions[1], []addonv1beta1.ConfigReference{{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
					DesiredConfig: &addonv1beta1.ConfigSpecHash{
						ConfigReferent: addonv1beta1.ConfigReferent{Name: "test1"},
						SpecHash:       "hash1",
					},
					LastObservedGeneration: 0,
				}})
				expectPatchConditionAction(t, actions[0], metav1.ConditionTrue)
				expectPatchConditionAction(t, actions[1], metav1.ConditionTrue)
				expectPatchConditionAction(t, actions[2], metav1.ConditionFalse)
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterObj := append(c.placements, c.placementDecisions...) //nolint:gocritic
			fakeClusterClient := fakecluster.NewSimpleClientset(clusterObj...)
			fakeAddonClient := fakeaddon.NewSimpleClientset(c.managedClusteraddon...)

			addonInformers := addoninformers.NewSharedInformerFactory(fakeAddonClient, 10*time.Minute)
			clusterInformers := clusterv1informers.NewSharedInformerFactory(fakeClusterClient, 10*time.Minute)

			err := addonInformers.Addon().V1beta1().ManagedClusterAddOns().Informer().AddIndexers(
				cache.Indexers{
					addonindex.ManagedClusterAddonByName: addonindex.IndexManagedClusterAddonByName,
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
				if err := addonInformers.Addon().V1beta1().ManagedClusterAddOns().Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}

			controller := &addonConfigurationController{
				addonClient:                  fakeAddonClient,
				placementDecisionGetter:      helpers.PlacementDecisionGetter{Client: clusterInformers.Cluster().V1beta1().PlacementDecisions().Lister()},
				placementLister:              clusterInformers.Cluster().V1beta1().Placements().Lister(),
				clusterManagementAddonLister: addonInformers.Addon().V1beta1().ClusterManagementAddOns().Lister(),
				managedClusterAddonIndexer:   addonInformers.Addon().V1beta1().ManagedClusterAddOns().Informer().GetIndexer(),
			}

			reconcile := &managedClusterAddonConfigurationReconciler{
				addonClient: fakeAddonClient,
			}

			graph, err := controller.buildConfigurationGraph(c.clusterManagementAddon)
			if err != nil {
				t.Errorf("expected no error when build graph: %v", err)
			}
			err = graph.generateRolloutResult()
			if err != nil {
				t.Errorf("expected no error when refresh rollout result: %v", err)
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

func newManagedClusterAddon(
	name, namespace string,
	configs []addonv1beta1.AddOnConfig,
	configStatus []addonv1beta1.ConfigReference,
	conditions []metav1.Condition,
) *addonv1beta1.ManagedClusterAddOn {
	mca := addontesting.NewAddon(name, namespace)
	mca.Spec.Configs = configs
	mca.Status.ConfigReferences = configStatus
	mca.Status.Conditions = conditions
	return mca
}

func expectPatchConfigurationAction(t *testing.T, action clienttesting.Action, expected []addonv1beta1.ConfigReference) {
	patch := action.(clienttesting.PatchActionImpl).GetPatch()
	mca := &addonv1beta1.ManagedClusterAddOn{}
	err := json.Unmarshal(patch, mca)
	if err != nil {
		t.Fatal(err)
	}

	if !apiequality.Semantic.DeepEqual(mca.Status.ConfigReferences, expected) {
		t.Errorf("Configuration not correctly patched, expected %v, actual %v", expected, mca.Status.ConfigReferences)
	}
}

func expectPatchConditionAction(t *testing.T, action clienttesting.Action, expected metav1.ConditionStatus) {
	patch := action.(clienttesting.PatchActionImpl).GetPatch()
	mca := &addonv1beta1.ManagedClusterAddOn{}
	err := json.Unmarshal(patch, mca)
	if err != nil {
		t.Fatal(err)
	}

	actualCond := meta.FindStatusCondition(mca.Status.Conditions, addonv1beta1.ManagedClusterAddOnConditionConfigured)
	if actualCond == nil || actualCond.Status != expected {
		t.Errorf("Condition not correctly patched, expected %v, actual %v", expected, mca.Status.Conditions)
	}
}

func TestMergeAddonConfig(t *testing.T) {
	fooGR := addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"}
	barGR := addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Bar"}

	cases := []struct {
		name             string
		mca              *addonv1beta1.ManagedClusterAddOn
		desiredConfigMap addonConfigMap
		expected         []addonv1beta1.ConfigReference
	}{
		{
			name: "reordering same-GR configs updates status order",
			mca: &addonv1beta1.ManagedClusterAddOn{
				Status: addonv1beta1.ManagedClusterAddOnStatus{
					ConfigReferences: []addonv1beta1.ConfigReference{
						{
							ConfigGroupResource: fooGR,
							DesiredConfig: &addonv1beta1.ConfigSpecHash{
								ConfigReferent: addonv1beta1.ConfigReferent{Name: "alpha"},
								SpecHash:       "hash-alpha",
							},
							LastAppliedConfig: &addonv1beta1.ConfigSpecHash{
								ConfigReferent: addonv1beta1.ConfigReferent{Name: "alpha"},
								SpecHash:       "hash-alpha",
							},
							LastObservedGeneration: 3,
						},
						{
							ConfigGroupResource: fooGR,
							DesiredConfig: &addonv1beta1.ConfigSpecHash{
								ConfigReferent: addonv1beta1.ConfigReferent{Name: "beta"},
								SpecHash:       "hash-beta",
							},
							LastAppliedConfig: &addonv1beta1.ConfigSpecHash{
								ConfigReferent: addonv1beta1.ConfigReferent{Name: "beta"},
								SpecHash:       "hash-beta",
							},
							LastObservedGeneration: 2,
						},
					},
				},
			},
			desiredConfigMap: addonConfigMap{
				fooGR: {
					{
						ConfigGroupResource: fooGR,
						DesiredConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "beta"},
							SpecHash:       "hash-beta-new",
						},
					},
					{
						ConfigGroupResource: fooGR,
						DesiredConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "alpha"},
							SpecHash:       "hash-alpha-new",
						},
					},
				},
			},
			expected: []addonv1beta1.ConfigReference{
				{
					ConfigGroupResource: fooGR,
					DesiredConfig: &addonv1beta1.ConfigSpecHash{
						ConfigReferent: addonv1beta1.ConfigReferent{Name: "beta"},
						SpecHash:       "hash-beta-new",
					},
					LastAppliedConfig: &addonv1beta1.ConfigSpecHash{
						ConfigReferent: addonv1beta1.ConfigReferent{Name: "beta"},
						SpecHash:       "hash-beta",
					},
					LastObservedGeneration: 2,
				},
				{
					ConfigGroupResource: fooGR,
					DesiredConfig: &addonv1beta1.ConfigSpecHash{
						ConfigReferent: addonv1beta1.ConfigReferent{Name: "alpha"},
						SpecHash:       "hash-alpha-new",
					},
					LastAppliedConfig: &addonv1beta1.ConfigSpecHash{
						ConfigReferent: addonv1beta1.ConfigReferent{Name: "alpha"},
						SpecHash:       "hash-alpha",
					},
					LastObservedGeneration: 3,
				},
			},
		},
		{
			name: "new entry is appended without old status fields",
			mca: &addonv1beta1.ManagedClusterAddOn{
				Status: addonv1beta1.ManagedClusterAddOnStatus{
					ConfigReferences: []addonv1beta1.ConfigReference{
						{
							ConfigGroupResource: fooGR,
							DesiredConfig: &addonv1beta1.ConfigSpecHash{
								ConfigReferent: addonv1beta1.ConfigReferent{Name: "existing"},
								SpecHash:       "hash1",
							},
							LastAppliedConfig: &addonv1beta1.ConfigSpecHash{
								ConfigReferent: addonv1beta1.ConfigReferent{Name: "existing"},
								SpecHash:       "hash1",
							},
							LastObservedGeneration: 5,
						},
					},
				},
			},
			desiredConfigMap: addonConfigMap{
				fooGR: {
					{
						ConfigGroupResource: fooGR,
						DesiredConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "existing"},
							SpecHash:       "hash1",
						},
					},
					{
						ConfigGroupResource: fooGR,
						DesiredConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "brand-new"},
							SpecHash:       "hash-new",
						},
					},
				},
			},
			expected: []addonv1beta1.ConfigReference{
				{
					ConfigGroupResource: fooGR,
					DesiredConfig: &addonv1beta1.ConfigSpecHash{
						ConfigReferent: addonv1beta1.ConfigReferent{Name: "existing"},
						SpecHash:       "hash1",
					},
					LastAppliedConfig: &addonv1beta1.ConfigSpecHash{
						ConfigReferent: addonv1beta1.ConfigReferent{Name: "existing"},
						SpecHash:       "hash1",
					},
					LastObservedGeneration: 5,
				},
				{
					ConfigGroupResource: fooGR,
					DesiredConfig: &addonv1beta1.ConfigSpecHash{
						ConfigReferent: addonv1beta1.ConfigReferent{Name: "brand-new"},
						SpecHash:       "hash-new",
					},
				},
			},
		},
		{
			name: "removed entry is dropped from status",
			mca: &addonv1beta1.ManagedClusterAddOn{
				Status: addonv1beta1.ManagedClusterAddOnStatus{
					ConfigReferences: []addonv1beta1.ConfigReference{
						{
							ConfigGroupResource: fooGR,
							DesiredConfig: &addonv1beta1.ConfigSpecHash{
								ConfigReferent: addonv1beta1.ConfigReferent{Name: "keep"},
								SpecHash:       "hash-keep",
							},
							LastObservedGeneration: 1,
						},
						{
							ConfigGroupResource: fooGR,
							DesiredConfig: &addonv1beta1.ConfigSpecHash{
								ConfigReferent: addonv1beta1.ConfigReferent{Name: "remove"},
								SpecHash:       "hash-remove",
							},
							LastObservedGeneration: 1,
						},
					},
				},
			},
			desiredConfigMap: addonConfigMap{
				fooGR: {
					{
						ConfigGroupResource: fooGR,
						DesiredConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "keep"},
							SpecHash:       "hash-keep",
						},
					},
				},
			},
			expected: []addonv1beta1.ConfigReference{
				{
					ConfigGroupResource: fooGR,
					DesiredConfig: &addonv1beta1.ConfigSpecHash{
						ConfigReferent: addonv1beta1.ConfigReferent{Name: "keep"},
						SpecHash:       "hash-keep",
					},
					LastObservedGeneration: 1,
				},
			},
		},
		{
			name: "cross-GR ordering follows sorted GR keys",
			mca: &addonv1beta1.ManagedClusterAddOn{
				Status: addonv1beta1.ManagedClusterAddOnStatus{
					ConfigReferences: []addonv1beta1.ConfigReference{},
				},
			},
			desiredConfigMap: addonConfigMap{
				fooGR: {
					{
						ConfigGroupResource: fooGR,
						DesiredConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "foo-config"},
							SpecHash:       "hash-foo",
						},
					},
				},
				barGR: {
					{
						ConfigGroupResource: barGR,
						DesiredConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "bar-config"},
							SpecHash:       "hash-bar",
						},
					},
				},
			},
			expected: []addonv1beta1.ConfigReference{
				{
					ConfigGroupResource: barGR,
					DesiredConfig: &addonv1beta1.ConfigSpecHash{
						ConfigReferent: addonv1beta1.ConfigReferent{Name: "bar-config"},
						SpecHash:       "hash-bar",
					},
				},
				{
					ConfigGroupResource: fooGR,
					DesiredConfig: &addonv1beta1.ConfigSpecHash{
						ConfigReferent: addonv1beta1.ConfigReferent{Name: "foo-config"},
						SpecHash:       "hash-foo",
					},
				},
			},
		},
	}

	reconciler := &managedClusterAddonConfigurationReconciler{}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := reconciler.mergeAddonConfig(c.mca, c.desiredConfigMap)
			if !apiequality.Semantic.DeepEqual(result.Status.ConfigReferences, c.expected) {
				t.Errorf("mergeAddonConfig result mismatch\nexpected: %v\n  actual: %v",
					c.expected, result.Status.ConfigReferences)
			}
		})
	}
}
