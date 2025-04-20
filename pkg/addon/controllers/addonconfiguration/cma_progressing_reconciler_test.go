package addonconfiguration

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2/ktesting"

	"open-cluster-management.io/addon-framework/pkg/addonmanager/addontesting"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	fakeaddon "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	fakecluster "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterv1informers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	addonindex "open-cluster-management.io/ocm/pkg/addon/index"
	"open-cluster-management.io/ocm/pkg/common/helpers"
)

func TestMgmtAddonProgressingReconcile(t *testing.T) {
	cases := []struct {
		name                   string
		managedClusteraddon    []runtime.Object
		clusterManagementAddon []runtime.Object
		placements             []runtime.Object
		placementDecisions     []runtime.Object
		validateAddonActions   func(t *testing.T, actions []clienttesting.Action)
		expectErr              bool
	}{
		{
			name:                "no managedClusteraddon",
			managedClusteraddon: []runtime.Object{},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "", "").
				WithPlacementStrategy(addonv1alpha1.PlacementStrategy{
					PlacementRef:    addonv1alpha1.PlacementRef{Name: "placement1", Namespace: "test"},
					RolloutStrategy: clusterv1alpha1.RolloutStrategy{Type: clusterv1alpha1.All},
				}).WithInstallProgression(addonv1alpha1.InstallProgression{
				PlacementRef: addonv1alpha1.PlacementRef{Name: "placement1", Namespace: "test"},
				ConfigReferences: []addonv1alpha1.InstallConfigReference{
					{
						ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &addonv1alpha1.ConfigSpecHash{
							ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test1"},
							SpecHash:       "hash1",
						},
					},
				},
			}).Build()},
			placements: []runtime.Object{
				&clusterv1beta1.Placement{ObjectMeta: metav1.ObjectMeta{Name: "placement1", Namespace: "test"}},
			},
			placementDecisions: []runtime.Object{
				&clusterv1beta1.PlacementDecision{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "placement1",
						Namespace: "test",
						Labels: map[string]string{
							clusterv1beta1.PlacementLabel:          "placement1",
							clusterv1beta1.DecisionGroupIndexLabel: "0",
						},
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{{ClusterName: "cluster1"}, {ClusterName: "cluster2"}},
					},
				},
			},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch
				cma := &addonv1alpha1.ClusterManagementAddOn{}
				err := json.Unmarshal(actual, cma)
				if err != nil {
					t.Fatal(err)
				}

				if len(cma.Status.DefaultConfigReferences) != 0 {
					t.Errorf("DefaultConfigReferences object is not correct: %v", cma.Status.DefaultConfigReferences)
				}
				if cma.Status.InstallProgressions[0].ConfigReferences[0].LastAppliedConfig != nil {
					t.Errorf("InstallProgressions LastAppliedConfig is not correct: %v", cma.Status.InstallProgressions[0].ConfigReferences[0])
				}
				if cma.Status.InstallProgressions[0].ConfigReferences[0].LastKnownGoodConfig != nil {
					t.Errorf("InstallProgressions LastKnownGoodConfig is not correct: %v", cma.Status.InstallProgressions[0].ConfigReferences[0])
				}
				if cma.Status.InstallProgressions[0].Conditions[0].Reason != addonv1alpha1.ProgressingReasonProgressing {
					t.Errorf("InstallProgressions condition is not correct: %v", cma.Status.InstallProgressions[0].Conditions[0].Reason)
				}
				if cma.Status.InstallProgressions[0].Conditions[0].Message != "0/2 progressing..., 0 failed 0 timeout." {
					t.Errorf("InstallProgressions condition is not correct: %v", cma.Status.InstallProgressions[0].Conditions[0].Message)
				}
			},
		},
		{
			name:                "no placement",
			managedClusteraddon: []runtime.Object{},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "", "").WithPlacementStrategy().
				WithInstallProgression(addonv1alpha1.InstallProgression{
					PlacementRef: addonv1alpha1.PlacementRef{Name: "placement1", Namespace: "test"},
					ConfigReferences: []addonv1alpha1.InstallConfigReference{
						{
							ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
							DesiredConfig: &addonv1alpha1.ConfigSpecHash{
								ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test1"},
								SpecHash:       "hash1",
							},
						},
					},
				}).Build()},
			validateAddonActions: addontesting.AssertNoActions,
		},
		{
			name: "update clustermanagementaddon status with condition Progressing installing",
			managedClusteraddon: []runtime.Object{func() *addonv1alpha1.ManagedClusterAddOn {
				addon := addontesting.NewAddon("test", "cluster1")
				addon.Status.ConfigReferences = []addonv1alpha1.ConfigReference{
					{
						ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &addonv1alpha1.ConfigSpecHash{
							ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test1"},
							SpecHash:       "hash1",
						},
					},
					{
						ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &addonv1alpha1.ConfigSpecHash{
							ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test2"},
							SpecHash:       "hash2",
						},
					},
				}
				return addon
			}()},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "", "").
				WithPlacementStrategy(addonv1alpha1.PlacementStrategy{
					PlacementRef:    addonv1alpha1.PlacementRef{Name: "placement1", Namespace: "test"},
					RolloutStrategy: clusterv1alpha1.RolloutStrategy{Type: clusterv1alpha1.All},
				}).WithInstallProgression(addonv1alpha1.InstallProgression{
				PlacementRef: addonv1alpha1.PlacementRef{Name: "placement1", Namespace: "test"},
				ConfigReferences: []addonv1alpha1.InstallConfigReference{
					{
						ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &addonv1alpha1.ConfigSpecHash{
							ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test1"},
							SpecHash:       "hash1",
						},
					},
					{
						ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &addonv1alpha1.ConfigSpecHash{
							ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test2"},
							SpecHash:       "hash2",
						},
					},
				},
			}).Build()},
			placements: []runtime.Object{
				&clusterv1beta1.Placement{ObjectMeta: metav1.ObjectMeta{Name: "placement1", Namespace: "test"}},
			},
			placementDecisions: []runtime.Object{
				&clusterv1beta1.PlacementDecision{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "placement1",
						Namespace: "test",
						Labels: map[string]string{
							clusterv1beta1.PlacementLabel:          "placement1",
							clusterv1beta1.DecisionGroupIndexLabel: "0",
						},
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{{ClusterName: "cluster1"}, {ClusterName: "cluster2"}},
					},
				},
			},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch
				cma := &addonv1alpha1.ClusterManagementAddOn{}
				err := json.Unmarshal(actual, cma)
				if err != nil {
					t.Fatal(err)
				}

				if len(cma.Status.DefaultConfigReferences) != 0 {
					t.Errorf("DefaultConfigReferences object is not correct: %v", cma.Status.DefaultConfigReferences)
				}
				if cma.Status.InstallProgressions[0].ConfigReferences[0].LastAppliedConfig != nil {
					t.Errorf("InstallProgressions LastAppliedConfig is not correct: %v", cma.Status.InstallProgressions[0].ConfigReferences[0])
				}
				if cma.Status.InstallProgressions[0].ConfigReferences[0].LastKnownGoodConfig != nil {
					t.Errorf("InstallProgressions LastKnownGoodConfig is not correct: %v", cma.Status.InstallProgressions[0].ConfigReferences[0])
				}
				if cma.Status.InstallProgressions[0].Conditions[0].Reason != addonv1alpha1.ProgressingReasonProgressing {
					t.Errorf("InstallProgressions condition is not correct: %v", cma.Status.InstallProgressions[0].Conditions[0].Reason)
				}
				if cma.Status.InstallProgressions[0].Conditions[0].Message != "1/2 progressing..., 0 failed 0 timeout." {
					t.Errorf("InstallProgressions condition is not correct: %v", cma.Status.InstallProgressions[0].Conditions[0].Message)
				}
			},
		},
		{
			name: "update clustermanagementaddon status with condition Progressing install succeed",
			managedClusteraddon: []runtime.Object{func() *addonv1alpha1.ManagedClusterAddOn {
				addon := addontesting.NewAddon("test", "cluster1")
				addon.Status.ConfigReferences = []addonv1alpha1.ConfigReference{
					{
						ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &addonv1alpha1.ConfigSpecHash{
							ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test1"},
							SpecHash:       "hash1",
						},
						LastAppliedConfig: &addonv1alpha1.ConfigSpecHash{
							ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test1"},
							SpecHash:       "hash1",
						},
					},
					{
						ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &addonv1alpha1.ConfigSpecHash{
							ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test2"},
							SpecHash:       "hash2",
						},
						LastAppliedConfig: &addonv1alpha1.ConfigSpecHash{
							ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test2"},
							SpecHash:       "hash2",
						},
					},
				}
				return addon
			}()},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "", "").
				WithPlacementStrategy(addonv1alpha1.PlacementStrategy{
					PlacementRef:    addonv1alpha1.PlacementRef{Name: "placement1", Namespace: "test"},
					RolloutStrategy: clusterv1alpha1.RolloutStrategy{Type: clusterv1alpha1.All},
				}).WithInstallProgression(addonv1alpha1.InstallProgression{
				PlacementRef: addonv1alpha1.PlacementRef{Name: "placement1", Namespace: "test"},
				ConfigReferences: []addonv1alpha1.InstallConfigReference{
					{
						ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &addonv1alpha1.ConfigSpecHash{
							ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test1"},
							SpecHash:       "hash1",
						},
					},
					{
						ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &addonv1alpha1.ConfigSpecHash{
							ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test2"},
							SpecHash:       "hash2",
						},
					},
				},
			}).Build()},
			placements: []runtime.Object{
				&clusterv1beta1.Placement{ObjectMeta: metav1.ObjectMeta{Name: "placement1", Namespace: "test"}},
			},
			placementDecisions: []runtime.Object{
				&clusterv1beta1.PlacementDecision{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "placement1",
						Namespace: "test",
						Labels: map[string]string{
							clusterv1beta1.PlacementLabel:          "placement1",
							clusterv1beta1.DecisionGroupIndexLabel: "0",
						},
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{{ClusterName: "cluster1"}},
					},
				},
			},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch
				cma := &addonv1alpha1.ClusterManagementAddOn{}
				err := json.Unmarshal(actual, cma)
				if err != nil {
					t.Fatal(err)
				}

				if len(cma.Status.DefaultConfigReferences) != 0 {
					t.Errorf("DefaultConfigReferences object is not correct: %v", cma.Status.DefaultConfigReferences)
				}
				if !apiequality.Semantic.DeepEqual(
					cma.Status.InstallProgressions[0].ConfigReferences[0].LastAppliedConfig,
					cma.Status.InstallProgressions[0].ConfigReferences[0].DesiredConfig) {
					t.Errorf("InstallProgressions LastAppliedConfig is not correct: %v", cma.Status.InstallProgressions[0].ConfigReferences[0])
				}
				if !apiequality.Semantic.DeepEqual(
					cma.Status.InstallProgressions[0].ConfigReferences[0].LastKnownGoodConfig,
					cma.Status.InstallProgressions[0].ConfigReferences[0].DesiredConfig) {
					t.Errorf("InstallProgressions LastKnownGoodConfig is not correct: %v", cma.Status.InstallProgressions[0].ConfigReferences[0])
				}
				if cma.Status.InstallProgressions[0].Conditions[0].Reason != addonv1alpha1.ProgressingReasonCompleted {
					t.Errorf("InstallProgressions condition is not correct: %v", cma.Status.InstallProgressions[0].Conditions)
				}
				if cma.Status.InstallProgressions[0].Conditions[0].Message != "1/1 completed with no errors, 0 failed 0 timeout." {
					t.Errorf("InstallProgressions condition is not correct: %v", cma.Status.InstallProgressions[0].Conditions)
				}
			},
		},
		{
			name: "update clustermanagementaddon status with condition Progressing upgrading",
			managedClusteraddon: []runtime.Object{func() *addonv1alpha1.ManagedClusterAddOn {
				addon := addontesting.NewAddon("test", "cluster1")
				addon.Status.ConfigReferences = []addonv1alpha1.ConfigReference{
					{
						ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &addonv1alpha1.ConfigSpecHash{
							ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test1"},
							SpecHash:       "hash1",
						},
					},
					{
						ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &addonv1alpha1.ConfigSpecHash{
							ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test2"},
							SpecHash:       "hash2",
						},
					},
				}
				return addon
			}()},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "", "").
				WithPlacementStrategy(addonv1alpha1.PlacementStrategy{
					PlacementRef:    addonv1alpha1.PlacementRef{Name: "placement1", Namespace: "test"},
					RolloutStrategy: clusterv1alpha1.RolloutStrategy{Type: clusterv1alpha1.All},
				}).WithInstallProgression(addonv1alpha1.InstallProgression{
				PlacementRef: addonv1alpha1.PlacementRef{Name: "placement1", Namespace: "test"},
				ConfigReferences: []addonv1alpha1.InstallConfigReference{
					{
						ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &addonv1alpha1.ConfigSpecHash{
							ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test1"},
							SpecHash:       "hash1",
						},
						LastAppliedConfig: &addonv1alpha1.ConfigSpecHash{
							ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test1"},
							SpecHash:       "hash",
						},
					},
					{
						ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &addonv1alpha1.ConfigSpecHash{
							ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test2"},
							SpecHash:       "hash2",
						},
						LastAppliedConfig: &addonv1alpha1.ConfigSpecHash{
							ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test2"},
							SpecHash:       "hash",
						},
					},
				},
			}).Build()},
			placements: []runtime.Object{
				&clusterv1beta1.Placement{ObjectMeta: metav1.ObjectMeta{Name: "placement1", Namespace: "test"}},
			},
			placementDecisions: []runtime.Object{
				&clusterv1beta1.PlacementDecision{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "placement1",
						Namespace: "test",
						Labels: map[string]string{
							clusterv1beta1.PlacementLabel:          "placement1",
							clusterv1beta1.DecisionGroupIndexLabel: "0",
						},
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{{ClusterName: "cluster1"}, {ClusterName: "cluster2"}},
					},
				},
			},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch
				cma := &addonv1alpha1.ClusterManagementAddOn{}
				err := json.Unmarshal(actual, cma)
				if err != nil {
					t.Fatal(err)
				}

				if len(cma.Status.DefaultConfigReferences) != 0 {
					t.Errorf("DefaultConfigReferences object is not correct: %v", cma.Status.DefaultConfigReferences)
				}
				if cma.Status.InstallProgressions[0].ConfigReferences[0].LastKnownGoodConfig != nil {
					t.Errorf("InstallProgressions LastKnownGoodConfig is not correct: %v", cma.Status.InstallProgressions[0].ConfigReferences[0])
				}
				if cma.Status.InstallProgressions[0].Conditions[0].Reason != addonv1alpha1.ProgressingReasonProgressing {
					t.Errorf("InstallProgressions condition is not correct: %v", cma.Status.InstallProgressions[0].Conditions)
				}
				if cma.Status.InstallProgressions[0].Conditions[0].Message != "1/2 progressing..., 0 failed 0 timeout." {
					t.Errorf("InstallProgressions condition is not correct: %v", cma.Status.InstallProgressions[0].Conditions)
				}
			},
		},
		{
			name: "update clustermanagementaddon status with condition Progressing upgrade succeed",
			managedClusteraddon: []runtime.Object{func() *addonv1alpha1.ManagedClusterAddOn {
				addon := addontesting.NewAddon("test", "cluster1")
				addon.Status.ConfigReferences = []addonv1alpha1.ConfigReference{
					{
						ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &addonv1alpha1.ConfigSpecHash{
							ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test1"},
							SpecHash:       "hash1",
						},
						LastAppliedConfig: &addonv1alpha1.ConfigSpecHash{
							ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test1"},
							SpecHash:       "hash1",
						},
					},
					{
						ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &addonv1alpha1.ConfigSpecHash{
							ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test2"},
							SpecHash:       "hash2",
						},
						LastAppliedConfig: &addonv1alpha1.ConfigSpecHash{
							ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test2"},
							SpecHash:       "hash2",
						},
					},
				}
				return addon
			}()},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "", "").
				WithPlacementStrategy(addonv1alpha1.PlacementStrategy{
					PlacementRef:    addonv1alpha1.PlacementRef{Name: "placement1", Namespace: "test"},
					RolloutStrategy: clusterv1alpha1.RolloutStrategy{Type: clusterv1alpha1.All},
				}).WithInstallProgression(addonv1alpha1.InstallProgression{
				PlacementRef: addonv1alpha1.PlacementRef{Name: "placement1", Namespace: "test"},
				ConfigReferences: []addonv1alpha1.InstallConfigReference{
					{
						ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &addonv1alpha1.ConfigSpecHash{
							ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test1"},
							SpecHash:       "hash1",
						},
						LastAppliedConfig: &addonv1alpha1.ConfigSpecHash{
							ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test1"},
							SpecHash:       "hash",
						},
					},
					{
						ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &addonv1alpha1.ConfigSpecHash{
							ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test2"},
							SpecHash:       "hash2",
						},
						LastAppliedConfig: &addonv1alpha1.ConfigSpecHash{
							ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test2"},
							SpecHash:       "hash",
						},
					},
				},
			}).Build()},
			placements: []runtime.Object{
				&clusterv1beta1.Placement{ObjectMeta: metav1.ObjectMeta{Name: "placement1", Namespace: "test"}},
			},
			placementDecisions: []runtime.Object{
				&clusterv1beta1.PlacementDecision{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "placement1",
						Namespace: "test",
						Labels: map[string]string{
							clusterv1beta1.PlacementLabel:          "placement1",
							clusterv1beta1.DecisionGroupIndexLabel: "0",
						},
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{{ClusterName: "cluster1"}},
					},
				},
			},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch
				cma := &addonv1alpha1.ClusterManagementAddOn{}
				err := json.Unmarshal(actual, cma)
				if err != nil {
					t.Fatal(err)
				}

				if len(cma.Status.DefaultConfigReferences) != 0 {
					t.Errorf("DefaultConfigReferences object is not correct: %v", cma.Status.DefaultConfigReferences)
				}
				if !apiequality.Semantic.DeepEqual(
					cma.Status.InstallProgressions[0].ConfigReferences[0].LastAppliedConfig,
					cma.Status.InstallProgressions[0].ConfigReferences[0].DesiredConfig) {
					t.Errorf("InstallProgressions LastAppliedConfig is not correct: %v", cma.Status.InstallProgressions[0].ConfigReferences[0])
				}
				if !apiequality.Semantic.DeepEqual(
					cma.Status.InstallProgressions[0].ConfigReferences[0].LastKnownGoodConfig,
					cma.Status.InstallProgressions[0].ConfigReferences[0].DesiredConfig) {
					t.Errorf("InstallProgressions LastKnownGoodConfig is not correct: %v", cma.Status.InstallProgressions[0].ConfigReferences[0])
				}
				if cma.Status.InstallProgressions[0].Conditions[0].Reason != addonv1alpha1.ProgressingReasonCompleted {
					t.Errorf("InstallProgressions condition is not correct: %v", cma.Status.InstallProgressions[0].Conditions)
				}
				if cma.Status.InstallProgressions[0].Conditions[0].Message != "1/1 completed with no errors, 0 failed 0 timeout." {
					t.Errorf("InstallProgressions condition is not correct: %v", cma.Status.InstallProgressions[0].Conditions)
				}
			},
		},
		{
			name: "mca override cma configs",
			managedClusteraddon: []runtime.Object{func() *addonv1alpha1.ManagedClusterAddOn {
				addon := addontesting.NewAddon("test", "cluster1")
				addon.Spec.Configs = []addonv1alpha1.AddOnConfig{
					{
						ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						ConfigReferent:      addonv1alpha1.ConfigReferent{Name: "testmca"},
					},
				}
				addon.Status.ConfigReferences = []addonv1alpha1.ConfigReference{
					{
						ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &addonv1alpha1.ConfigSpecHash{
							ConfigReferent: addonv1alpha1.ConfigReferent{Name: "testmca"},
							SpecHash:       "hashmca",
						},
						LastAppliedConfig: &addonv1alpha1.ConfigSpecHash{
							ConfigReferent: addonv1alpha1.ConfigReferent{Name: "testmca"},
							SpecHash:       "hashmca",
						},
					},
				}
				return addon
			}()},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "", "").
				WithPlacementStrategy(addonv1alpha1.PlacementStrategy{
					PlacementRef:    addonv1alpha1.PlacementRef{Name: "placement1", Namespace: "test"},
					RolloutStrategy: clusterv1alpha1.RolloutStrategy{Type: clusterv1alpha1.All},
				}).WithInstallProgression(addonv1alpha1.InstallProgression{
				PlacementRef: addonv1alpha1.PlacementRef{Name: "placement1", Namespace: "test"},
				ConfigReferences: []addonv1alpha1.InstallConfigReference{
					{
						ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &addonv1alpha1.ConfigSpecHash{
							ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test1"},
							SpecHash:       "hash1",
						},
						LastAppliedConfig: &addonv1alpha1.ConfigSpecHash{
							ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test1"},
							SpecHash:       "hash",
						},
					},
				},
			}).Build()},
			placements: []runtime.Object{
				&clusterv1beta1.Placement{ObjectMeta: metav1.ObjectMeta{Name: "placement1", Namespace: "test"}},
			},
			placementDecisions: []runtime.Object{
				&clusterv1beta1.PlacementDecision{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "placement1",
						Namespace: "test",
						Labels: map[string]string{
							clusterv1beta1.PlacementLabel:          "placement1",
							clusterv1beta1.DecisionGroupIndexLabel: "0",
						},
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{{ClusterName: "cluster1"}},
					},
				},
			},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch
				cma := &addonv1alpha1.ClusterManagementAddOn{}
				err := json.Unmarshal(actual, cma)
				if err != nil {
					t.Fatal(err)
				}

				if len(cma.Status.DefaultConfigReferences) != 0 {
					t.Errorf("DefaultConfigReferences object is not correct: %v", cma.Status.DefaultConfigReferences)
				}
				if cma.Status.InstallProgressions[0].ConfigReferences[0].LastKnownGoodConfig != nil {
					t.Errorf("InstallProgressions LastKnownGoodConfig is not correct: %v", cma.Status.InstallProgressions[0].ConfigReferences[0])
				}
				if cma.Status.InstallProgressions[0].Conditions[0].Reason != addonv1alpha1.ProgressingReasonProgressing {
					t.Errorf("InstallProgressions condition is not correct: %v", cma.Status.InstallProgressions[0].Conditions)
				}
				if cma.Status.InstallProgressions[0].Conditions[0].Message != "0/1 progressing..., 0 failed 0 timeout." {
					t.Errorf("InstallProgressions condition is not correct: %v", cma.Status.InstallProgressions[0].Conditions)
				}
			},
		},
		{
			name: "update clustermanagementaddon status with condition Progressing ConfigurationUnsupported",
			managedClusteraddon: []runtime.Object{func() *addonv1alpha1.ManagedClusterAddOn {
				addon := addontesting.NewAddon("test", "cluster1")
				addon.Status.ConfigReferences = []addonv1alpha1.ConfigReference{
					{
						ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &addonv1alpha1.ConfigSpecHash{
							ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test1"},
							SpecHash:       "hash1",
						},
					},
				}
				return addon
			}()},
			clusterManagementAddon: []runtime.Object{addontesting.NewClusterManagementAddon("test", "", "").
				WithPlacementStrategy(addonv1alpha1.PlacementStrategy{
					PlacementRef:    addonv1alpha1.PlacementRef{Name: "placement1", Namespace: "test"},
					RolloutStrategy: clusterv1alpha1.RolloutStrategy{Type: clusterv1alpha1.All},
				}).WithInstallProgression(addonv1alpha1.InstallProgression{
				PlacementRef: addonv1alpha1.PlacementRef{Name: "placement1", Namespace: "test"},
				ConfigReferences: []addonv1alpha1.InstallConfigReference{
					{
						ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &addonv1alpha1.ConfigSpecHash{
							ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test1"},
							SpecHash:       "hash1",
						},
					},
				},
			}).Build()},
			placements: []runtime.Object{
				&clusterv1beta1.Placement{ObjectMeta: metav1.ObjectMeta{Name: "placement1", Namespace: "test"}},
			},
			placementDecisions: []runtime.Object{
				&clusterv1beta1.PlacementDecision{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "placement1",
						Namespace: "test",
						Labels: map[string]string{
							clusterv1beta1.PlacementLabel:          "placement1",
							clusterv1beta1.DecisionGroupIndexLabel: "0",
						},
					},
					Status: clusterv1beta1.PlacementDecisionStatus{
						Decisions: []clusterv1beta1.ClusterDecision{{ClusterName: "cluster1"}, {ClusterName: "cluster2"}},
					},
				},
			},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "patch")
				actual := actions[0].(clienttesting.PatchActionImpl).Patch
				cma := &addonv1alpha1.ClusterManagementAddOn{}
				err := json.Unmarshal(actual, cma)
				if err != nil {
					t.Fatal(err)
				}

				if len(cma.Status.DefaultConfigReferences) != 0 {
					t.Errorf("DefaultConfigReferences object is not correct: %v", cma.Status.DefaultConfigReferences)
				}
				if cma.Status.InstallProgressions[0].ConfigReferences[0].LastAppliedConfig != nil {
					t.Errorf("InstallProgressions LastAppliedConfig is not correct: %v", cma.Status.InstallProgressions[0].ConfigReferences[0])
				}
				if cma.Status.InstallProgressions[0].ConfigReferences[0].LastKnownGoodConfig != nil {
					t.Errorf("InstallProgressions LastKnownGoodConfig is not correct: %v", cma.Status.InstallProgressions[0].ConfigReferences[0])
				}
				if cma.Status.InstallProgressions[0].Conditions[0].Reason != addonv1alpha1.ProgressingReasonProgressing {
					t.Errorf("InstallProgressions condition is not correct: %v", cma.Status.InstallProgressions[0].Conditions)
				}
				if cma.Status.InstallProgressions[0].Conditions[0].Message != "1/2 progressing..., 0 failed 0 timeout." {
					t.Errorf("InstallProgressions condition is not correct: %v", cma.Status.InstallProgressions[0].Conditions[0].Message)
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			logger, _ := ktesting.NewTestContext(t)
			obj := append(c.clusterManagementAddon, c.managedClusteraddon...) //nolint:gocritic
			clusterObj := append(c.placements, c.placementDecisions...)       //nolint:gocritic
			fakeClusterClient := fakecluster.NewSimpleClientset(clusterObj...)
			fakeAddonClient := fakeaddon.NewSimpleClientset(obj...)

			addonInformers := addoninformers.NewSharedInformerFactory(fakeAddonClient, 10*time.Minute)
			clusterInformers := clusterv1informers.NewSharedInformerFactory(fakeClusterClient, 10*time.Minute)

			err := addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Informer().AddIndexers(
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

			for _, obj := range c.clusterManagementAddon {
				if err = addonInformers.Addon().V1alpha1().ClusterManagementAddOns().Informer().GetStore().Add(obj); err != nil {
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
				placementDecisionGetter:      helpers.PlacementDecisionGetter{Client: clusterInformers.Cluster().V1beta1().PlacementDecisions().Lister()},
				placementLister:              clusterInformers.Cluster().V1beta1().Placements().Lister(),
				clusterManagementAddonLister: addonInformers.Addon().V1alpha1().ClusterManagementAddOns().Lister(),
				managedClusterAddonIndexer:   addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Informer().GetIndexer(),
			}

			reconcile := &cmaProgressingReconciler{
				patcher.NewPatcher[
					*addonv1alpha1.ClusterManagementAddOn, addonv1alpha1.ClusterManagementAddOnSpec, addonv1alpha1.ClusterManagementAddOnStatus](
					fakeAddonClient.AddonV1alpha1().ClusterManagementAddOns()),
			}

			for _, obj := range c.clusterManagementAddon {
				graph, err := controller.buildConfigurationGraph(logger, obj.(*addonv1alpha1.ClusterManagementAddOn))
				if err != nil {
					t.Errorf("expected no error when build graph: %v", err)
				}
				err = graph.generateRolloutResult()
				if err != nil {
					t.Errorf("expected no error when refresh rollout result: %v", err)
				}

				_, _, err = reconcile.reconcile(context.TODO(), obj.(*addonv1alpha1.ClusterManagementAddOn), graph)
				if err != nil && !c.expectErr {
					t.Errorf("expected no error when sync: %v", err)
				}
				if err == nil && c.expectErr {
					t.Errorf("Expect error but got no error")
				}
			}

			c.validateAddonActions(t, fakeAddonClient.Actions())
		})
	}
}
