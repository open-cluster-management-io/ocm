package addonconfiguration

import (
	"reflect"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	"open-cluster-management.io/addon-framework/pkg/addonmanager/addontesting"
	addonv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	fakecluster "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterv1informers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	clustersdkv1alpha1 "open-cluster-management.io/sdk-go/pkg/apis/cluster/v1alpha1"

	"open-cluster-management.io/ocm/pkg/common/helpers"
)

var fakeTime = metav1.NewTime(time.Date(2022, time.January, 01, 0, 0, 0, 0, time.UTC))

type placementDesicion struct {
	addonv1beta1.PlacementRef
	clusters []clusterv1beta1.ClusterDecision
}

func TestConfigurationGraph(t *testing.T) {
	cases := []struct {
		name                       string
		defaultConfigs             []addonv1beta1.AddOnConfig
		defaultConfigReference     []addonv1beta1.DefaultConfigReference
		addons                     []*addonv1beta1.ManagedClusterAddOn
		placementDecisions         []placementDesicion
		placementStrategies        []addonv1beta1.PlacementStrategy
		installProgressions        []addonv1beta1.InstallProgression
		expected                   []*addonNode
		expectedConfiguredClusters []sets.Set[string]
	}{
		{
			name:     "no output",
			expected: nil,
		},
		{
			name: "default config only",
			defaultConfigs: []addonv1beta1.AddOnConfig{
				{ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Bar"}},
				{ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
					ConfigReferent: addonv1beta1.ConfigReferent{Name: "test"}},
			},
			defaultConfigReference: []addonv1beta1.DefaultConfigReference{
				newDefaultConfigReference("core", "Foo", "test", "<core-foo-test-hash>"),
			},
			addons: []*addonv1beta1.ManagedClusterAddOn{
				addontesting.NewAddon("test", "cluster1"),
				addontesting.NewAddon("test", "cluster2"),
			},
			expected: []*addonNode{
				{
					desiredConfigs: map[addonv1beta1.ConfigGroupResource][]addonv1beta1.ConfigReference{
						{Group: "core", Resource: "Foo"}: {
							{
								ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
								DesiredConfig: &addonv1beta1.ConfigSpecHash{
									ConfigReferent: addonv1beta1.ConfigReferent{Name: "test"},
									SpecHash:       "<core-foo-test-hash>",
								},
							},
						},
					},
					mca: addontesting.NewAddon("test", "cluster1"),
					status: &clustersdkv1alpha1.ClusterRolloutStatus{
						ClusterName: "cluster1",
						Status:      clustersdkv1alpha1.ToApply},
				},
				{
					desiredConfigs: map[addonv1beta1.ConfigGroupResource][]addonv1beta1.ConfigReference{
						{Group: "core", Resource: "Foo"}: {
							{
								ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
								DesiredConfig: &addonv1beta1.ConfigSpecHash{
									ConfigReferent: addonv1beta1.ConfigReferent{Name: "test"},
									SpecHash:       "<core-foo-test-hash>",
								},
							},
						},
					},
					mca: addontesting.NewAddon("test", "cluster2"),
					status: &clustersdkv1alpha1.ClusterRolloutStatus{
						ClusterName: "cluster2",
						Status:      clustersdkv1alpha1.ToApply},
				},
			},
		},
		{
			name: "with placement strategy",
			defaultConfigs: []addonv1beta1.AddOnConfig{
				{ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Bar"},
					ConfigReferent: addonv1beta1.ConfigReferent{Name: "test"}},
				{ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
					ConfigReferent: addonv1beta1.ConfigReferent{Name: "test"}},
			},
			defaultConfigReference: []addonv1beta1.DefaultConfigReference{
				newDefaultConfigReference("core", "Bar", "test", "<core-bar-test-hash>"),
				newDefaultConfigReference("core", "Foo", "test", "<core-foo-test-hash>"),
			},
			addons: []*addonv1beta1.ManagedClusterAddOn{
				addontesting.NewAddon("test", "cluster1"),
				addontesting.NewAddon("test", "cluster2"),
				addontesting.NewAddon("test", "cluster3"),
			},
			placementDecisions: []placementDesicion{
				{PlacementRef: addonv1beta1.PlacementRef{Name: "placement1", Namespace: "test"},
					clusters: []clusterv1beta1.ClusterDecision{{ClusterName: "cluster1"}}},
				{PlacementRef: addonv1beta1.PlacementRef{Name: "placement2", Namespace: "test"},
					clusters: []clusterv1beta1.ClusterDecision{{ClusterName: "cluster2"}}},
			},
			placementStrategies: []addonv1beta1.PlacementStrategy{
				{PlacementRef: addonv1beta1.PlacementRef{Name: "placement1", Namespace: "test"},
					RolloutStrategy: clusterv1alpha1.RolloutStrategy{Type: clusterv1alpha1.All}},
				{PlacementRef: addonv1beta1.PlacementRef{Name: "placement2", Namespace: "test"},
					RolloutStrategy: clusterv1alpha1.RolloutStrategy{Type: clusterv1alpha1.All}},
			},
			installProgressions: []addonv1beta1.InstallProgression{
				{
					PlacementRef: addonv1beta1.PlacementRef{Name: "placement1", Namespace: "test"},
					ConfigReferences: []addonv1beta1.InstallConfigReference{
						newInstallConfigReference("core", "Bar", "test1", "<core-bar-test1-hash>"),
					},
				},
				{
					PlacementRef: addonv1beta1.PlacementRef{Name: "placement2", Namespace: "test"},
					ConfigReferences: []addonv1beta1.InstallConfigReference{
						newInstallConfigReference("core", "Bar", "test2", "<core-bar-test2-hash>"),
						newInstallConfigReference("core", "Foo", "test2", "<core-foo-test2-hash>"),
					},
				},
			},
			expected: []*addonNode{
				{
					desiredConfigs: map[addonv1beta1.ConfigGroupResource][]addonv1beta1.ConfigReference{
						{Group: "core", Resource: "Bar"}: {
							{
								ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Bar"},
								DesiredConfig: &addonv1beta1.ConfigSpecHash{
									ConfigReferent: addonv1beta1.ConfigReferent{Name: "test1"},
									SpecHash:       "<core-bar-test1-hash>",
								},
							},
						},
						{Group: "core", Resource: "Foo"}: {
							{
								ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
								DesiredConfig: &addonv1beta1.ConfigSpecHash{
									ConfigReferent: addonv1beta1.ConfigReferent{Name: "test"},
									SpecHash:       "<core-foo-test-hash>",
								},
							},
						},
					},
					mca: addontesting.NewAddon("test", "cluster1"),
					status: &clustersdkv1alpha1.ClusterRolloutStatus{
						ClusterName: "cluster1",
						Status:      clustersdkv1alpha1.ToApply},
				},
				{
					desiredConfigs: map[addonv1beta1.ConfigGroupResource][]addonv1beta1.ConfigReference{
						{Group: "core", Resource: "Bar"}: {
							{
								ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Bar"},
								DesiredConfig: &addonv1beta1.ConfigSpecHash{
									ConfigReferent: addonv1beta1.ConfigReferent{Name: "test2"},
									SpecHash:       "<core-bar-test2-hash>",
								},
							},
						},
						{Group: "core", Resource: "Foo"}: {
							{
								ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
								DesiredConfig: &addonv1beta1.ConfigSpecHash{
									ConfigReferent: addonv1beta1.ConfigReferent{Name: "test2"},
									SpecHash:       "<core-foo-test2-hash>",
								},
							},
						},
					},
					mca: addontesting.NewAddon("test", "cluster2"),
					status: &clustersdkv1alpha1.ClusterRolloutStatus{
						ClusterName: "cluster2",
						Status:      clustersdkv1alpha1.ToApply,
					},
				},
				{
					desiredConfigs: map[addonv1beta1.ConfigGroupResource][]addonv1beta1.ConfigReference{
						{Group: "core", Resource: "Bar"}: {
							{
								ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Bar"},
								DesiredConfig: &addonv1beta1.ConfigSpecHash{
									ConfigReferent: addonv1beta1.ConfigReferent{Name: "test"},
									SpecHash:       "<core-bar-test-hash>",
								},
							},
						},
						{Group: "core", Resource: "Foo"}: {
							{
								ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
								DesiredConfig: &addonv1beta1.ConfigSpecHash{
									ConfigReferent: addonv1beta1.ConfigReferent{Name: "test"},
									SpecHash:       "<core-foo-test-hash>",
								},
							},
						},
					},
					mca: addontesting.NewAddon("test", "cluster3"),
					status: &clustersdkv1alpha1.ClusterRolloutStatus{
						ClusterName: "cluster3",
						Status:      clustersdkv1alpha1.ToApply,
					},
				},
			},
		},
		{
			name:                   "mca progressing/failed/succeed",
			defaultConfigReference: []addonv1beta1.DefaultConfigReference{},
			addons: []*addonv1beta1.ManagedClusterAddOn{
				newManagedClusterAddon("test", "cluster1", []addonv1beta1.AddOnConfig{}, []addonv1beta1.ConfigReference{
					{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Bar"},
						DesiredConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "test1"},
							SpecHash:       "<core-bar-test1-hash>",
						},
						LastObservedGeneration: 1,
					},
				}, []metav1.Condition{
					{
						Type:               addonv1beta1.ManagedClusterAddOnConditionProgressing,
						Reason:             addonv1beta1.ProgressingReasonFailed,
						LastTransitionTime: fakeTime,
					},
				}),
				newManagedClusterAddon("test", "cluster2", []addonv1beta1.AddOnConfig{}, []addonv1beta1.ConfigReference{
					{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Bar"},
						DesiredConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "test1"},
							SpecHash:       "<core-bar-test1-hash>",
						},
						LastObservedGeneration: 1,
					},
				}, []metav1.Condition{
					{
						Type:               addonv1beta1.ManagedClusterAddOnConditionProgressing,
						Reason:             addonv1beta1.ProgressingReasonProgressing,
						LastTransitionTime: fakeTime,
					},
				}),
				newManagedClusterAddon("test", "cluster3", []addonv1beta1.AddOnConfig{}, []addonv1beta1.ConfigReference{
					{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Bar"},
						DesiredConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "test1"},
							SpecHash:       "<core-bar-test1-hash>",
						},
						LastAppliedConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "test1"},
							SpecHash:       "<core-bar-test1-hash>",
						},
						LastObservedGeneration: 1,
					},
				}, []metav1.Condition{
					{
						Type:               addonv1beta1.ManagedClusterAddOnConditionProgressing,
						Reason:             addonv1beta1.ProgressingReasonCompleted,
						LastTransitionTime: fakeTime,
					},
				}),
				newManagedClusterAddon("test", "cluster4", []addonv1beta1.AddOnConfig{}, []addonv1beta1.ConfigReference{
					{
						ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						DesiredConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "testx"},
							SpecHash:       "<core-bar-testx-hash>",
						},
						LastAppliedConfig: &addonv1beta1.ConfigSpecHash{
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "testx"},
							SpecHash:       "<core-bar-testx-hash>",
						},
						LastObservedGeneration: 1,
					},
				}, []metav1.Condition{
					{
						Type:               addonv1beta1.ManagedClusterAddOnConditionProgressing,
						Reason:             addonv1beta1.ProgressingReasonCompleted,
						LastTransitionTime: fakeTime,
					},
				}),
			},
			placementDecisions: []placementDesicion{
				{PlacementRef: addonv1beta1.PlacementRef{Name: "placement1", Namespace: "test"},
					clusters: []clusterv1beta1.ClusterDecision{{ClusterName: "cluster1"}, {ClusterName: "cluster2"},
						{ClusterName: "cluster3"}, {ClusterName: "cluster4"}}},
			},
			placementStrategies: []addonv1beta1.PlacementStrategy{
				{PlacementRef: addonv1beta1.PlacementRef{Name: "placement1", Namespace: "test"},
					RolloutStrategy: clusterv1alpha1.RolloutStrategy{Type: clusterv1alpha1.All}},
			},
			installProgressions: []addonv1beta1.InstallProgression{
				{
					PlacementRef: addonv1beta1.PlacementRef{Name: "placement1", Namespace: "test"},
					ConfigReferences: []addonv1beta1.InstallConfigReference{
						newInstallConfigReference("core", "Bar", "test1", "<core-bar-test1-hash>"),
					},
				},
			},
			expected: []*addonNode{
				{
					desiredConfigs: map[addonv1beta1.ConfigGroupResource][]addonv1beta1.ConfigReference{
						{Group: "core", Resource: "Bar"}: {
							{
								ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Bar"},
								DesiredConfig: &addonv1beta1.ConfigSpecHash{
									ConfigReferent: addonv1beta1.ConfigReferent{Name: "test1"},
									SpecHash:       "<core-bar-test1-hash>",
								},
							},
						},
					},
					mca: addontesting.NewAddon("test", "cluster1"),
					status: &clustersdkv1alpha1.ClusterRolloutStatus{
						ClusterName:        "cluster1",
						Status:             clustersdkv1alpha1.Failed,
						LastTransitionTime: &fakeTime,
					},
				},
				{
					desiredConfigs: map[addonv1beta1.ConfigGroupResource][]addonv1beta1.ConfigReference{
						{Group: "core", Resource: "Bar"}: {
							{
								ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Bar"},
								DesiredConfig: &addonv1beta1.ConfigSpecHash{
									ConfigReferent: addonv1beta1.ConfigReferent{Name: "test1"},
									SpecHash:       "<core-bar-test1-hash>",
								},
							},
						},
					},
					mca: addontesting.NewAddon("test", "cluster2"),
					status: &clustersdkv1alpha1.ClusterRolloutStatus{
						ClusterName:        "cluster2",
						Status:             clustersdkv1alpha1.Progressing,
						LastTransitionTime: &fakeTime,
					},
				},
				{
					desiredConfigs: map[addonv1beta1.ConfigGroupResource][]addonv1beta1.ConfigReference{
						{Group: "core", Resource: "Bar"}: {
							{
								ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Bar"},
								DesiredConfig: &addonv1beta1.ConfigSpecHash{
									ConfigReferent: addonv1beta1.ConfigReferent{Name: "test1"},
									SpecHash:       "<core-bar-test1-hash>",
								},
							},
						},
					},
					mca: addontesting.NewAddon("test", "cluster4"),
					status: &clustersdkv1alpha1.ClusterRolloutStatus{
						ClusterName: "cluster4",
						Status:      clustersdkv1alpha1.ToApply,
					},
				},
			},
		},
		{
			name: "placement overlap",
			defaultConfigs: []addonv1beta1.AddOnConfig{
				{ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Bar"},
					ConfigReferent: addonv1beta1.ConfigReferent{Name: "test"}},
				{ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
					ConfigReferent: addonv1beta1.ConfigReferent{Name: "test"}},
			},
			defaultConfigReference: []addonv1beta1.DefaultConfigReference{
				newDefaultConfigReference("core", "Bar", "test", "<core-bar-test-hash>"),
				newDefaultConfigReference("core", "Foo", "test", "<core-foo-test-hash>"),
			},
			addons: []*addonv1beta1.ManagedClusterAddOn{
				addontesting.NewAddon("test", "cluster1"),
				addontesting.NewAddon("test", "cluster2"),
				addontesting.NewAddon("test", "cluster3"),
			},
			placementStrategies: []addonv1beta1.PlacementStrategy{
				{PlacementRef: addonv1beta1.PlacementRef{Name: "placement1", Namespace: "test"},
					RolloutStrategy: clusterv1alpha1.RolloutStrategy{Type: clusterv1alpha1.All}},
				{PlacementRef: addonv1beta1.PlacementRef{Name: "placement2", Namespace: "test"},
					RolloutStrategy: clusterv1alpha1.RolloutStrategy{Type: clusterv1alpha1.All}},
			},
			placementDecisions: []placementDesicion{
				{PlacementRef: addonv1beta1.PlacementRef{Name: "placement1", Namespace: "test"},
					clusters: []clusterv1beta1.ClusterDecision{{ClusterName: "cluster1"}, {ClusterName: "cluster2"}}},
				{PlacementRef: addonv1beta1.PlacementRef{Name: "placement2", Namespace: "test"},
					clusters: []clusterv1beta1.ClusterDecision{{ClusterName: "cluster2"}, {ClusterName: "cluster3"}}},
			},
			installProgressions: []addonv1beta1.InstallProgression{
				{
					PlacementRef: addonv1beta1.PlacementRef{Name: "placement1", Namespace: "test"},
					ConfigReferences: []addonv1beta1.InstallConfigReference{
						newInstallConfigReference("core", "Bar", "test1", "<core-bar-test1-hash>"),
					},
				},
				{
					PlacementRef: addonv1beta1.PlacementRef{Name: "placement2", Namespace: "test"},
					ConfigReferences: []addonv1beta1.InstallConfigReference{
						newInstallConfigReference("core", "Bar", "test2", "<core-bar-test2-hash>"),
						newInstallConfigReference("core", "Foo", "test2", "<core-foo-test2-hash>"),
					},
				},
			},
			expected: []*addonNode{
				{
					desiredConfigs: map[addonv1beta1.ConfigGroupResource][]addonv1beta1.ConfigReference{
						{Group: "core", Resource: "Bar"}: {
							{
								ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Bar"},
								DesiredConfig: &addonv1beta1.ConfigSpecHash{
									ConfigReferent: addonv1beta1.ConfigReferent{Name: "test1"},
									SpecHash:       "<core-bar-test1-hash>",
								},
							},
						},
						{Group: "core", Resource: "Foo"}: {
							{
								ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
								DesiredConfig: &addonv1beta1.ConfigSpecHash{
									ConfigReferent: addonv1beta1.ConfigReferent{Name: "test"},
									SpecHash:       "<core-foo-test-hash>",
								},
							},
						},
					},
					mca: addontesting.NewAddon("test", "cluster1"),
					status: &clustersdkv1alpha1.ClusterRolloutStatus{
						ClusterName: "cluster1",
						Status:      clustersdkv1alpha1.ToApply},
				},
				{
					desiredConfigs: map[addonv1beta1.ConfigGroupResource][]addonv1beta1.ConfigReference{
						{Group: "core", Resource: "Bar"}: {
							{
								ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Bar"},
								DesiredConfig: &addonv1beta1.ConfigSpecHash{
									ConfigReferent: addonv1beta1.ConfigReferent{Name: "test2"},
									SpecHash:       "<core-bar-test2-hash>",
								},
							},
						},
						{Group: "core", Resource: "Foo"}: {
							{
								ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
								DesiredConfig: &addonv1beta1.ConfigSpecHash{
									ConfigReferent: addonv1beta1.ConfigReferent{Name: "test2"},
									SpecHash:       "<core-foo-test2-hash>",
								},
							},
						},
					},
					mca: addontesting.NewAddon("test", "cluster2"),
					status: &clustersdkv1alpha1.ClusterRolloutStatus{
						ClusterName: "cluster2",
						Status:      clustersdkv1alpha1.ToApply},
				},
				{
					desiredConfigs: map[addonv1beta1.ConfigGroupResource][]addonv1beta1.ConfigReference{
						{Group: "core", Resource: "Bar"}: {
							{
								ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Bar"},
								DesiredConfig: &addonv1beta1.ConfigSpecHash{
									ConfigReferent: addonv1beta1.ConfigReferent{Name: "test2"},
									SpecHash:       "<core-bar-test2-hash>",
								},
							},
						},
						{Group: "core", Resource: "Foo"}: {
							{
								ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
								DesiredConfig: &addonv1beta1.ConfigSpecHash{
									ConfigReferent: addonv1beta1.ConfigReferent{Name: "test2"},
									SpecHash:       "<core-foo-test2-hash>",
								},
							},
						},
					},
					mca: addontesting.NewAddon("test", "cluster3"),
					status: &clustersdkv1alpha1.ClusterRolloutStatus{
						ClusterName: "cluster3",
						Status:      clustersdkv1alpha1.ToApply},
				},
			},
			expectedConfiguredClusters: []sets.Set[string]{
				sets.New[string]("cluster1"),
				sets.New[string]("cluster2", "cluster3"),
			},
		},
		{
			name: "mca override",
			defaultConfigs: []addonv1beta1.AddOnConfig{
				{ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Bar"},
					ConfigReferent: addonv1beta1.ConfigReferent{Name: "test"}},
				{ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
					ConfigReferent: addonv1beta1.ConfigReferent{Name: "test"}},
			},
			defaultConfigReference: []addonv1beta1.DefaultConfigReference{
				newDefaultConfigReference("core", "Bar", "test", "<core-bar-test-hash>"),
				newDefaultConfigReference("core", "Foo", "test", "<core-foo-test-hash>"),
			},
			addons: []*addonv1beta1.ManagedClusterAddOn{
				newManagedClusterAddon("test", "cluster1", []addonv1beta1.AddOnConfig{
					{ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Bar"},
						ConfigReferent: addonv1beta1.ConfigReferent{Name: "test1"}},
				}, nil, nil),
				addontesting.NewAddon("test", "cluster2"),
				addontesting.NewAddon("test", "cluster3"),
			},
			placementStrategies: []addonv1beta1.PlacementStrategy{
				{PlacementRef: addonv1beta1.PlacementRef{Name: "placement1", Namespace: "test"},
					RolloutStrategy: clusterv1alpha1.RolloutStrategy{Type: clusterv1alpha1.All}},
				{PlacementRef: addonv1beta1.PlacementRef{Name: "placement2", Namespace: "test"},
					RolloutStrategy: clusterv1alpha1.RolloutStrategy{Type: clusterv1alpha1.All}},
			},
			placementDecisions: []placementDesicion{
				{PlacementRef: addonv1beta1.PlacementRef{Name: "placement1", Namespace: "test"},
					clusters: []clusterv1beta1.ClusterDecision{{ClusterName: "cluster1"}}},
				{PlacementRef: addonv1beta1.PlacementRef{Name: "placement2", Namespace: "test"},
					clusters: []clusterv1beta1.ClusterDecision{{ClusterName: "cluster2"}}},
			},
			installProgressions: []addonv1beta1.InstallProgression{
				{
					PlacementRef: addonv1beta1.PlacementRef{Name: "placement1", Namespace: "test"},
					ConfigReferences: []addonv1beta1.InstallConfigReference{
						newInstallConfigReference("core", "Foo", "test1", "<core-foo-test1-hash>"),
					},
				},
				{
					PlacementRef: addonv1beta1.PlacementRef{Name: "placement2", Namespace: "test"},
					ConfigReferences: []addonv1beta1.InstallConfigReference{
						newInstallConfigReference("core", "Bar", "test2", "<core-bar-test2-hash>"),
						newInstallConfigReference("core", "Foo", "test2", "<core-foo-test2-hash>"),
					},
				},
			},
			expected: []*addonNode{
				{
					desiredConfigs: map[addonv1beta1.ConfigGroupResource][]addonv1beta1.ConfigReference{
						{Group: "core", Resource: "Bar"}: {
							{
								ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Bar"},
								DesiredConfig: &addonv1beta1.ConfigSpecHash{
									ConfigReferent: addonv1beta1.ConfigReferent{Name: "test1"},
									SpecHash:       "",
								},
							},
						},
						{Group: "core", Resource: "Foo"}: {
							{
								ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
								DesiredConfig: &addonv1beta1.ConfigSpecHash{
									ConfigReferent: addonv1beta1.ConfigReferent{Name: "test1"},
									SpecHash:       "<core-foo-test1-hash>",
								},
							},
						},
					},
					mca: newManagedClusterAddon("test", "cluster1", []addonv1beta1.AddOnConfig{
						{ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Bar"},
							ConfigReferent: addonv1beta1.ConfigReferent{Name: "test1"}},
					}, nil, nil),
					status: &clustersdkv1alpha1.ClusterRolloutStatus{
						ClusterName: "cluster1",
						Status:      clustersdkv1alpha1.ToApply},
				},
				{
					desiredConfigs: map[addonv1beta1.ConfigGroupResource][]addonv1beta1.ConfigReference{
						{Group: "core", Resource: "Bar"}: {
							{
								ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Bar"},
								DesiredConfig: &addonv1beta1.ConfigSpecHash{
									ConfigReferent: addonv1beta1.ConfigReferent{Name: "test2"},
									SpecHash:       "<core-bar-test2-hash>",
								},
							},
						},
						{Group: "core", Resource: "Foo"}: {
							{
								ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
								DesiredConfig: &addonv1beta1.ConfigSpecHash{
									ConfigReferent: addonv1beta1.ConfigReferent{Name: "test2"},
									SpecHash:       "<core-foo-test2-hash>",
								},
							},
						},
					},
					mca: addontesting.NewAddon("test", "cluster2"),
					status: &clustersdkv1alpha1.ClusterRolloutStatus{
						ClusterName: "cluster2",
						Status:      clustersdkv1alpha1.ToApply},
				},
				{
					desiredConfigs: map[addonv1beta1.ConfigGroupResource][]addonv1beta1.ConfigReference{
						{Group: "core", Resource: "Bar"}: {
							{
								ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Bar"},
								DesiredConfig: &addonv1beta1.ConfigSpecHash{
									ConfigReferent: addonv1beta1.ConfigReferent{Name: "test"},
									SpecHash:       "<core-bar-test-hash>",
								},
							},
						},
						{Group: "core", Resource: "Foo"}: {
							{
								ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
								DesiredConfig: &addonv1beta1.ConfigSpecHash{
									ConfigReferent: addonv1beta1.ConfigReferent{Name: "test"},
									SpecHash:       "<core-foo-test-hash>",
								},
							},
						},
					},
					mca: addontesting.NewAddon("test", "cluster3"),
					status: &clustersdkv1alpha1.ClusterRolloutStatus{
						ClusterName: "cluster3",
						Status:      clustersdkv1alpha1.ToApply},
				},
			},
		},
		{
			name: "placement strategy with multiple same-GVKs",
			defaultConfigs: []addonv1beta1.AddOnConfig{
				{ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Bar"},
					ConfigReferent: addonv1beta1.ConfigReferent{Name: "test"}},
				{ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
					ConfigReferent: addonv1beta1.ConfigReferent{Name: "test"}},
			},
			defaultConfigReference: []addonv1beta1.DefaultConfigReference{
				newDefaultConfigReference("core", "Bar", "test", "<core-bar-test-hash>"),
				newDefaultConfigReference("core", "Foo", "test", "<core-foo-test-hash>"),
			},
			addons: []*addonv1beta1.ManagedClusterAddOn{
				addontesting.NewAddon("test", "cluster1"),
				addontesting.NewAddon("test", "cluster2"),
			},
			placementDecisions: []placementDesicion{
				{PlacementRef: addonv1beta1.PlacementRef{Name: "placement1", Namespace: "test"},
					clusters: []clusterv1beta1.ClusterDecision{{ClusterName: "cluster1"}}},
				{PlacementRef: addonv1beta1.PlacementRef{Name: "placement2", Namespace: "test"},
					clusters: []clusterv1beta1.ClusterDecision{{ClusterName: "cluster2"}}},
			},
			placementStrategies: []addonv1beta1.PlacementStrategy{
				{PlacementRef: addonv1beta1.PlacementRef{Name: "placement1", Namespace: "test"},
					RolloutStrategy: clusterv1alpha1.RolloutStrategy{Type: clusterv1alpha1.All}},
				{PlacementRef: addonv1beta1.PlacementRef{Name: "placement2", Namespace: "test"},
					RolloutStrategy: clusterv1alpha1.RolloutStrategy{Type: clusterv1alpha1.All}},
			},
			installProgressions: []addonv1beta1.InstallProgression{
				{
					PlacementRef: addonv1beta1.PlacementRef{Name: "placement1", Namespace: "test"},
					ConfigReferences: []addonv1beta1.InstallConfigReference{
						newInstallConfigReference("core", "Bar", "test1", "<core-bar-test1-hash>"),
						newInstallConfigReference("core", "Bar", "test2", "<core-bar-test2-hash>"),
					},
				},
				{
					PlacementRef: addonv1beta1.PlacementRef{Name: "placement2", Namespace: "test"},
					ConfigReferences: []addonv1beta1.InstallConfigReference{
						newInstallConfigReference("core", "Bar", "test2", "<core-bar-test2-hash>"),
						newInstallConfigReference("core", "Foo", "test2", "<core-foo-test2-hash>"),
					},
				},
			},
			expected: []*addonNode{
				{
					desiredConfigs: map[addonv1beta1.ConfigGroupResource][]addonv1beta1.ConfigReference{
						{Group: "core", Resource: "Bar"}: {
							{
								ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Bar"},
								DesiredConfig: &addonv1beta1.ConfigSpecHash{
									ConfigReferent: addonv1beta1.ConfigReferent{Name: "test1"},
									SpecHash:       "<core-bar-test1-hash>",
								},
							},
							{
								ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Bar"},
								DesiredConfig: &addonv1beta1.ConfigSpecHash{
									ConfigReferent: addonv1beta1.ConfigReferent{Name: "test2"},
									SpecHash:       "<core-bar-test2-hash>",
								},
							},
						},
						{Group: "core", Resource: "Foo"}: {
							{
								ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
								DesiredConfig: &addonv1beta1.ConfigSpecHash{
									ConfigReferent: addonv1beta1.ConfigReferent{Name: "test"},
									SpecHash:       "<core-foo-test-hash>",
								},
							},
						},
					},
					mca: addontesting.NewAddon("test", "cluster1"),
					status: &clustersdkv1alpha1.ClusterRolloutStatus{
						ClusterName: "cluster1",
						Status:      clustersdkv1alpha1.ToApply},
				},
				{
					desiredConfigs: map[addonv1beta1.ConfigGroupResource][]addonv1beta1.ConfigReference{
						{Group: "core", Resource: "Bar"}: {
							{
								ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Bar"},
								DesiredConfig: &addonv1beta1.ConfigSpecHash{
									ConfigReferent: addonv1beta1.ConfigReferent{Name: "test2"},
									SpecHash:       "<core-bar-test2-hash>",
								},
							},
						},
						{Group: "core", Resource: "Foo"}: {
							{
								ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "core", Resource: "Foo"},
								DesiredConfig: &addonv1beta1.ConfigSpecHash{
									ConfigReferent: addonv1beta1.ConfigReferent{Name: "test2"},
									SpecHash:       "<core-foo-test2-hash>",
								},
							},
						},
					},
					mca: addontesting.NewAddon("test", "cluster2"),
					status: &clustersdkv1alpha1.ClusterRolloutStatus{
						ClusterName: "cluster2",
						Status:      clustersdkv1alpha1.ToApply,
					},
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeClusterClient := fakecluster.NewSimpleClientset()
			clusterInformers := clusterv1informers.NewSharedInformerFactory(fakeClusterClient, 10*time.Minute)
			placementDecisionGetter := helpers.PlacementDecisionGetter{Client: clusterInformers.Cluster().V1beta1().PlacementDecisions().Lister()}
			placementLister := clusterInformers.Cluster().V1beta1().Placements().Lister()

			for _, strategy := range c.placementStrategies {
				obj := &clusterv1beta1.Placement{ObjectMeta: metav1.ObjectMeta{Name: strategy.Name, Namespace: strategy.Namespace}}
				if err := clusterInformers.Cluster().V1beta1().Placements().Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}

			for _, decision := range c.placementDecisions {
				obj := &clusterv1beta1.PlacementDecision{
					ObjectMeta: metav1.ObjectMeta{Name: decision.Name, Namespace: decision.Namespace,
						Labels: map[string]string{
							clusterv1beta1.PlacementLabel:          decision.Name,
							clusterv1beta1.DecisionGroupIndexLabel: "0",
						}},
					Status: clusterv1beta1.PlacementDecisionStatus{Decisions: decision.clusters},
				}
				if err := clusterInformers.Cluster().V1beta1().PlacementDecisions().Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}

			graph := newGraph(c.defaultConfigs, c.defaultConfigReference)
			for _, addon := range c.addons {
				graph.addAddonNode(addon)
			}

			for i := range c.placementStrategies {
				graph.addPlacementNode(c.placementStrategies[i], c.installProgressions[i], placementLister, placementDecisionGetter)
			}

			err := graph.generateRolloutResult()
			if err != nil {
				t.Errorf("expected no error when refresh rollout result: %v", err)
			}

			actual := graph.getAddonsToUpdate()
			if len(actual) != len(c.expected) {
				t.Errorf("output length is not correct, expected %v, got %v", len(c.expected), len(actual))
			}

			if len(c.expectedConfiguredClusters) > 0 {
				if len(c.expectedConfiguredClusters) != len(graph.nodes) {
					t.Fatalf("expectedConfiguredClusters length %d does not match graph nodes length %d",
						len(c.expectedConfiguredClusters), len(graph.nodes))
				}
				for i, expectedClusters := range c.expectedConfiguredClusters {
					if !graph.nodes[i].configuredClusters.Equal(expectedClusters) {
						t.Errorf("configuredClusters for placement node %d is not correct, expected %v, got %v",
							i, expectedClusters, graph.nodes[i].configuredClusters)
					}
				}
			}

			for _, ev := range c.expected {
				compared := false
				for _, v := range actual {
					if v == nil || ev == nil {
						t.Errorf("addonNode should not be nil")
					}
					if ev.mca != nil && v.mca != nil && ev.mca.Namespace == v.mca.Namespace {
						if !reflect.DeepEqual(v.mca.Name, ev.mca.Name) {
							t.Errorf("output mca name is not correct, cluster %s, expected %v, got %v", v.mca.Namespace, ev.mca.Name, v.mca.Name)
						}
						if !reflect.DeepEqual(v.desiredConfigs, ev.desiredConfigs) {
							t.Errorf("output desiredConfigs is not correct, cluster %s, expected %v, got %v", v.mca.Namespace, ev.desiredConfigs, v.desiredConfigs)
						}
						if !reflect.DeepEqual(v.status, ev.status) {
							t.Errorf("output status is not correct, cluster %s, expected %v, got %v", v.mca.Namespace, ev.status, v.status)
						}
						compared = true
					}
				}

				if !compared {
					t.Errorf("not found addonNode %v", ev.mca)
				}
			}
		})
	}
}

func newInstallConfigReference(group, resource, name, hash string) addonv1beta1.InstallConfigReference {
	return addonv1beta1.InstallConfigReference{
		ConfigGroupResource: addonv1beta1.ConfigGroupResource{
			Group:    group,
			Resource: resource,
		},
		DesiredConfig: &addonv1beta1.ConfigSpecHash{
			ConfigReferent: addonv1beta1.ConfigReferent{Name: name},
			SpecHash:       hash,
		},
	}
}

func newDefaultConfigReference(group, resource, name, hash string) addonv1beta1.DefaultConfigReference {
	return addonv1beta1.DefaultConfigReference{
		ConfigGroupResource: addonv1beta1.ConfigGroupResource{
			Group:    group,
			Resource: resource,
		},
		DesiredConfig: &addonv1beta1.ConfigSpecHash{
			ConfigReferent: addonv1beta1.ConfigReferent{Name: name},
			SpecHash:       hash,
		},
	}
}

func newConfigReference(group, resource, name, hash string) addonv1beta1.ConfigReference {
	return addonv1beta1.ConfigReference{
		ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: group, Resource: resource},
		DesiredConfig: &addonv1beta1.ConfigSpecHash{
			ConfigReferent: addonv1beta1.ConfigReferent{Name: name},
			SpecHash:       hash,
		},
	}
}

func TestDesiredConfigsEqual(t *testing.T) {
	cases := []struct {
		name            string
		addonDesired    addonConfigMap
		strategyDesired addonConfigMap
		addonConfigs    []addonv1beta1.AddOnConfig
		expected        bool
	}{
		{
			name: "all strategy configs overridden by addon configs",
			strategyDesired: addonConfigMap{
				{Group: "addon", Resource: "AddonDeploymentConfig"}: {
					newConfigReference("addon", "AddonDeploymentConfig", "strategy-cfg", "hash1"),
				},
			},
			addonConfigs: []addonv1beta1.AddOnConfig{
				{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "addon", Resource: "AddonDeploymentConfig"},
					ConfigReferent:      addonv1beta1.ConfigReferent{Name: "addon-cfg"},
				},
			},
			addonDesired: addonConfigMap{
				{Group: "addon", Resource: "AddonDeploymentConfig"}: {
					newConfigReference("addon", "AddonDeploymentConfig", "addon-cfg", "hash-addon"),
				},
			},
			expected: true,
		},
		{
			name: "no addon configs, addonDesired matches strategy",
			strategyDesired: addonConfigMap{
				{Group: "addon", Resource: "AddonDeploymentConfig"}: {
					newConfigReference("addon", "AddonDeploymentConfig", "cfg1", "hash1"),
				},
			},
			addonConfigs: []addonv1beta1.AddOnConfig{},
			addonDesired: addonConfigMap{
				{Group: "addon", Resource: "AddonDeploymentConfig"}: {
					newConfigReference("addon", "AddonDeploymentConfig", "cfg1", "hash1"),
				},
			},
			expected: true,
		},
		{
			name: "addon overrides one GVK, inherited GVK matches",
			strategyDesired: addonConfigMap{
				{Group: "addon", Resource: "AddonDeploymentConfig"}: {
					newConfigReference("addon", "AddonDeploymentConfig", "strategy-cfg", "hash1"),
				},
				{Group: "addon", Resource: "AddonTemplate"}: {
					newConfigReference("addon", "AddonTemplate", "tpl1", "hash-tpl"),
				},
			},
			addonConfigs: []addonv1beta1.AddOnConfig{
				{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "addon", Resource: "AddonDeploymentConfig"},
					ConfigReferent:      addonv1beta1.ConfigReferent{Name: "addon-cfg"},
				},
			},
			addonDesired: addonConfigMap{
				{Group: "addon", Resource: "AddonDeploymentConfig"}: {
					newConfigReference("addon", "AddonDeploymentConfig", "addon-cfg", "hash-addon"),
				},
				{Group: "addon", Resource: "AddonTemplate"}: {
					newConfigReference("addon", "AddonTemplate", "tpl1", "hash-tpl"),
				},
			},
			expected: true,
		},
		{
			name: "addon overrides one GVK, inherited GVK does not match",
			strategyDesired: addonConfigMap{
				{Group: "addon", Resource: "AddonDeploymentConfig"}: {
					newConfigReference("addon", "AddonDeploymentConfig", "strategy-cfg", "hash1"),
				},
				{Group: "addon", Resource: "AddonTemplate"}: {
					newConfigReference("addon", "AddonTemplate", "tpl-new", "hash-tpl-new"),
				},
			},
			addonConfigs: []addonv1beta1.AddOnConfig{
				{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "addon", Resource: "AddonDeploymentConfig"},
					ConfigReferent:      addonv1beta1.ConfigReferent{Name: "addon-cfg"},
				},
			},
			addonDesired: addonConfigMap{
				{Group: "addon", Resource: "AddonDeploymentConfig"}: {
					newConfigReference("addon", "AddonDeploymentConfig", "addon-cfg", "hash-addon"),
				},
				{Group: "addon", Resource: "AddonTemplate"}: {
					newConfigReference("addon", "AddonTemplate", "tpl-old", "hash-tpl-old"),
				},
			},
			expected: false,
		},
		{
			name: "no addon configs, addonDesired does not match strategy",
			strategyDesired: addonConfigMap{
				{Group: "addon", Resource: "AddonDeploymentConfig"}: {
					newConfigReference("addon", "AddonDeploymentConfig", "cfg-new", "hash-new"),
				},
			},
			addonConfigs: []addonv1beta1.AddOnConfig{},
			addonDesired: addonConfigMap{
				{Group: "addon", Resource: "AddonDeploymentConfig"}: {
					newConfigReference("addon", "AddonDeploymentConfig", "cfg-old", "hash-old"),
				},
			},
			expected: false,
		},
		{
			name: "inherited GVK missing from addonDesired",
			strategyDesired: addonConfigMap{
				{Group: "addon", Resource: "AddonDeploymentConfig"}: {
					newConfigReference("addon", "AddonDeploymentConfig", "strategy-cfg", "hash1"),
				},
				{Group: "addon", Resource: "AddonTemplate"}: {
					newConfigReference("addon", "AddonTemplate", "tpl1", "hash-tpl"),
				},
			},
			addonConfigs: []addonv1beta1.AddOnConfig{
				{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{Group: "addon", Resource: "AddonDeploymentConfig"},
					ConfigReferent:      addonv1beta1.ConfigReferent{Name: "addon-cfg"},
				},
			},
			addonDesired: addonConfigMap{
				{Group: "addon", Resource: "AddonDeploymentConfig"}: {
					newConfigReference("addon", "AddonDeploymentConfig", "addon-cfg", "hash-addon"),
				},
			},
			expected: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			actual := desiredConfigsEqual(c.addonDesired, c.strategyDesired, c.addonConfigs)
			if actual != c.expected {
				t.Errorf("expected %v, got %v", c.expected, actual)
			}
		})
	}
}

func TestCountAddonUpgradeSucceedAndFailed(t *testing.T) {
	deploymentConfigGR := addonv1beta1.ConfigGroupResource{Group: "addon", Resource: "AddonDeploymentConfig"}
	templateGR := addonv1beta1.ConfigGroupResource{Group: "addon", Resource: "AddonTemplate"}

	cases := []struct {
		name            string
		node            *installStrategyNode
		expectedSucceed int
		expectedFailed  int
	}{
		{
			name: "1 config overridden by addon, succeed addon counted",
			node: &installStrategyNode{
				desiredConfigs: addonConfigMap{
					deploymentConfigGR: {
						newConfigReference("addon", "AddonDeploymentConfig", "strategy-cfg", "hash1"),
					},
				},
				children: map[string]*addonNode{
					"cluster1": {
						mca: newManagedClusterAddon("test", "cluster1",
							[]addonv1beta1.AddOnConfig{
								{ConfigGroupResource: deploymentConfigGR, ConfigReferent: addonv1beta1.ConfigReferent{Name: "addon-cfg"}},
							},
							nil, nil,
						),
						desiredConfigs: addonConfigMap{
							deploymentConfigGR: {
								newConfigReference("addon", "AddonDeploymentConfig", "addon-cfg", "hash-addon"),
							},
						},
						status: &clustersdkv1alpha1.ClusterRolloutStatus{
							ClusterName: "cluster1",
							Status:      clustersdkv1alpha1.Succeeded,
						},
					},
					"cluster2": {
						mca: newManagedClusterAddon("test", "cluster2",
							[]addonv1beta1.AddOnConfig{
								{ConfigGroupResource: deploymentConfigGR, ConfigReferent: addonv1beta1.ConfigReferent{Name: "addon-cfg2"}},
							},
							nil, nil,
						),
						desiredConfigs: addonConfigMap{
							deploymentConfigGR: {
								newConfigReference("addon", "AddonDeploymentConfig", "addon-cfg2", "hash-addon2"),
							},
						},
						status: &clustersdkv1alpha1.ClusterRolloutStatus{
							ClusterName: "cluster2",
							Status:      clustersdkv1alpha1.Failed,
						},
					},
				},
				rolloutResult: clustersdkv1alpha1.RolloutResult{},
			},
			expectedSucceed: 1,
			expectedFailed:  1,
		},
		{
			name: "2 configs, one overridden by addon, inherited matches",
			node: &installStrategyNode{
				desiredConfigs: addonConfigMap{
					deploymentConfigGR: {
						newConfigReference("addon", "AddonDeploymentConfig", "strategy-cfg", "hash1"),
					},
					templateGR: {
						newConfigReference("addon", "AddonTemplate", "tpl1", "hash-tpl"),
					},
				},
				children: map[string]*addonNode{
					// cluster1: overrides AddonDeploymentConfig, inherits AddonTemplate (matches) -> succeed counted
					"cluster1": {
						mca: newManagedClusterAddon("test", "cluster1",
							[]addonv1beta1.AddOnConfig{
								{ConfigGroupResource: deploymentConfigGR, ConfigReferent: addonv1beta1.ConfigReferent{Name: "addon-cfg"}},
							},
							nil, nil,
						),
						desiredConfigs: addonConfigMap{
							deploymentConfigGR: {
								newConfigReference("addon", "AddonDeploymentConfig", "addon-cfg", "hash-addon"),
							},
							templateGR: {
								newConfigReference("addon", "AddonTemplate", "tpl1", "hash-tpl"),
							},
						},
						status: &clustersdkv1alpha1.ClusterRolloutStatus{
							ClusterName: "cluster1",
							Status:      clustersdkv1alpha1.Succeeded,
						},
					},
					// cluster2: overrides AddonDeploymentConfig, inherited AddonTemplate mismatches -> failed NOT counted
					"cluster2": {
						mca: newManagedClusterAddon("test", "cluster2",
							[]addonv1beta1.AddOnConfig{
								{ConfigGroupResource: deploymentConfigGR, ConfigReferent: addonv1beta1.ConfigReferent{Name: "addon-cfg2"}},
							},
							nil, nil,
						),
						desiredConfigs: addonConfigMap{
							deploymentConfigGR: {
								newConfigReference("addon", "AddonDeploymentConfig", "addon-cfg2", "hash-addon2"),
							},
							templateGR: {
								newConfigReference("addon", "AddonTemplate", "tpl-old", "hash-tpl-old"),
							},
						},
						status: &clustersdkv1alpha1.ClusterRolloutStatus{
							ClusterName: "cluster2",
							Status:      clustersdkv1alpha1.Failed,
						},
					},
					// cluster3: overrides AddonDeploymentConfig, inherits AddonTemplate (matches) -> failed counted
					"cluster3": {
						mca: newManagedClusterAddon("test", "cluster3",
							[]addonv1beta1.AddOnConfig{
								{ConfigGroupResource: deploymentConfigGR, ConfigReferent: addonv1beta1.ConfigReferent{Name: "addon-cfg3"}},
							},
							nil, nil,
						),
						desiredConfigs: addonConfigMap{
							deploymentConfigGR: {
								newConfigReference("addon", "AddonDeploymentConfig", "addon-cfg3", "hash-addon3"),
							},
							templateGR: {
								newConfigReference("addon", "AddonTemplate", "tpl1", "hash-tpl"),
							},
						},
						status: &clustersdkv1alpha1.ClusterRolloutStatus{
							ClusterName: "cluster3",
							Status:      clustersdkv1alpha1.Failed,
						},
					},
				},
				rolloutResult: clustersdkv1alpha1.RolloutResult{},
			},
			expectedSucceed: 1,
			expectedFailed:  1,
		},
		{
			name: "2 configs, no override by addon",
			node: &installStrategyNode{
				desiredConfigs: addonConfigMap{
					deploymentConfigGR: {
						newConfigReference("addon", "AddonDeploymentConfig", "cfg1", "hash1"),
					},
					templateGR: {
						newConfigReference("addon", "AddonTemplate", "tpl1", "hash-tpl"),
					},
				},
				children: map[string]*addonNode{
					// cluster1: no override, desiredConfigs matches strategy -> succeed counted
					"cluster1": {
						mca: newManagedClusterAddon("test", "cluster1", nil, nil, nil),
						desiredConfigs: addonConfigMap{
							deploymentConfigGR: {
								newConfigReference("addon", "AddonDeploymentConfig", "cfg1", "hash1"),
							},
							templateGR: {
								newConfigReference("addon", "AddonTemplate", "tpl1", "hash-tpl"),
							},
						},
						status: &clustersdkv1alpha1.ClusterRolloutStatus{
							ClusterName: "cluster1",
							Status:      clustersdkv1alpha1.Succeeded,
						},
					},
					// cluster2: no override, desiredConfigs mismatches strategy -> failed NOT counted
					"cluster2": {
						mca: newManagedClusterAddon("test", "cluster2", nil, nil, nil),
						desiredConfigs: addonConfigMap{
							deploymentConfigGR: {
								newConfigReference("addon", "AddonDeploymentConfig", "cfg-old", "hash-old"),
							},
							templateGR: {
								newConfigReference("addon", "AddonTemplate", "tpl1", "hash-tpl"),
							},
						},
						status: &clustersdkv1alpha1.ClusterRolloutStatus{
							ClusterName: "cluster2",
							Status:      clustersdkv1alpha1.Failed,
						},
					},
					// cluster3: no override, desiredConfigs matches strategy -> failed counted
					"cluster3": {
						mca: newManagedClusterAddon("test", "cluster3", nil, nil, nil),
						desiredConfigs: addonConfigMap{
							deploymentConfigGR: {
								newConfigReference("addon", "AddonDeploymentConfig", "cfg1", "hash1"),
							},
							templateGR: {
								newConfigReference("addon", "AddonTemplate", "tpl1", "hash-tpl"),
							},
						},
						status: &clustersdkv1alpha1.ClusterRolloutStatus{
							ClusterName: "cluster3",
							Status:      clustersdkv1alpha1.Failed,
						},
					},
				},
				rolloutResult: clustersdkv1alpha1.RolloutResult{},
			},
			expectedSucceed: 1,
			expectedFailed:  1,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			actualSucceed := c.node.countAddonUpgradeSucceed()
			if actualSucceed != c.expectedSucceed {
				t.Errorf("countAddonUpgradeSucceed: expected %d, got %d", c.expectedSucceed, actualSucceed)
			}
			actualFailed := c.node.countAddonUpgradeFailed()
			if actualFailed != c.expectedFailed {
				t.Errorf("countAddonUpgradeFailed: expected %d, got %d", c.expectedFailed, actualFailed)
			}
		})
	}
}
