package addonconfiguration

import (
	"reflect"
	"testing"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"

	"open-cluster-management.io/addon-framework/pkg/addonmanager/addontesting"
)

type placementStrategy struct {
	configs  []addonv1alpha1.AddOnConfig
	clusters []string
}

func TestConfigurationGraph(t *testing.T) {
	cases := []struct {
		name                   string
		defaultConfigs         []addonv1alpha1.ConfigMeta
		defaultConfigReference []addonv1alpha1.DefaultConfigReference
		addons                 []*addonv1alpha1.ManagedClusterAddOn
		placementStrategies    []placementStrategy
		installProgressions    []addonv1alpha1.InstallProgression
		expected               []*addonNode
	}{
		{
			name:     "no output",
			expected: nil,
		},
		{
			name: "default config only",
			defaultConfigs: []addonv1alpha1.ConfigMeta{
				{ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Bar"}},
				{ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
					DefaultConfig: &addonv1alpha1.ConfigReferent{Name: "test"}},
			},
			defaultConfigReference: []addonv1alpha1.DefaultConfigReference{
				newDefaultConfigReference("core", "Foo", "test", "<core-foo-test-hash>"),
			},
			addons: []*addonv1alpha1.ManagedClusterAddOn{
				addontesting.NewAddon("test", "cluster1"),
				addontesting.NewAddon("test", "cluster2"),
			},
			expected: []*addonNode{
				{
					desiredConfigs: map[addonv1alpha1.ConfigGroupResource]addonv1alpha1.ConfigReference{
						{Group: "core", Resource: "Foo"}: {
							ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
							ConfigReferent:      addonv1alpha1.ConfigReferent{Name: "test"},
							DesiredConfig: &addonv1alpha1.ConfigSpecHash{
								ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test"},
								SpecHash:       "<core-foo-test-hash>",
							},
						},
					},
					mca: addontesting.NewAddon("test", "cluster1"),
				},
				{
					desiredConfigs: map[addonv1alpha1.ConfigGroupResource]addonv1alpha1.ConfigReference{
						{Group: "core", Resource: "Foo"}: {
							ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
							ConfigReferent:      addonv1alpha1.ConfigReferent{Name: "test"},
							DesiredConfig: &addonv1alpha1.ConfigSpecHash{
								ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test"},
								SpecHash:       "<core-foo-test-hash>",
							},
						},
					},
					mca: addontesting.NewAddon("test", "cluster2"),
				},
			},
		},
		{
			name: "with placement strategy",
			defaultConfigs: []addonv1alpha1.ConfigMeta{
				{ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Bar"},
					DefaultConfig: &addonv1alpha1.ConfigReferent{Name: "test"}},
				{ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
					DefaultConfig: &addonv1alpha1.ConfigReferent{Name: "test"}},
			},
			defaultConfigReference: []addonv1alpha1.DefaultConfigReference{
				newDefaultConfigReference("core", "Bar", "test", "<core-bar-test-hash>"),
				newDefaultConfigReference("core", "Foo", "test", "<core-foo-test-hash>"),
			},
			addons: []*addonv1alpha1.ManagedClusterAddOn{
				addontesting.NewAddon("test", "cluster1"),
				addontesting.NewAddon("test", "cluster2"),
				addontesting.NewAddon("test", "cluster3"),
			},
			placementStrategies: []placementStrategy{
				{configs: []addonv1alpha1.AddOnConfig{
					{ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Bar"},
						ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test1"}},
				}, clusters: []string{"cluster1"}},
				{configs: []addonv1alpha1.AddOnConfig{
					{ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Bar"},
						ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test2"}},
					{ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test2"}},
				}, clusters: []string{"cluster2"}},
			},
			installProgressions: []addonv1alpha1.InstallProgression{
				{
					ConfigReferences: []addonv1alpha1.InstallConfigReference{
						newInstallConfigReference("core", "Bar", "test1", "<core-bar-test1-hash>"),
					},
				},
				{
					ConfigReferences: []addonv1alpha1.InstallConfigReference{
						newInstallConfigReference("core", "Bar", "test2", "<core-bar-test2-hash>"),
						newInstallConfigReference("core", "Foo", "test2", "<core-foo-test2-hash>"),
					},
				},
			},
			expected: []*addonNode{
				{
					desiredConfigs: map[addonv1alpha1.ConfigGroupResource]addonv1alpha1.ConfigReference{
						{Group: "core", Resource: "Bar"}: {
							ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Bar"},
							ConfigReferent:      addonv1alpha1.ConfigReferent{Name: "test1"},
							DesiredConfig: &addonv1alpha1.ConfigSpecHash{
								ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test1"},
								SpecHash:       "<core-bar-test1-hash>",
							},
						},
						{Group: "core", Resource: "Foo"}: {
							ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
							ConfigReferent:      addonv1alpha1.ConfigReferent{Name: "test"},
							DesiredConfig: &addonv1alpha1.ConfigSpecHash{
								ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test"},
								SpecHash:       "<core-foo-test-hash>",
							},
						},
					},
					mca: addontesting.NewAddon("test", "cluster1"),
				},
				{
					desiredConfigs: map[addonv1alpha1.ConfigGroupResource]addonv1alpha1.ConfigReference{
						{Group: "core", Resource: "Bar"}: {
							ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Bar"},
							ConfigReferent:      addonv1alpha1.ConfigReferent{Name: "test2"},
							DesiredConfig: &addonv1alpha1.ConfigSpecHash{
								ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test2"},
								SpecHash:       "<core-bar-test2-hash>",
							},
						},
						{Group: "core", Resource: "Foo"}: {
							ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
							ConfigReferent:      addonv1alpha1.ConfigReferent{Name: "test2"},
							DesiredConfig: &addonv1alpha1.ConfigSpecHash{
								ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test2"},
								SpecHash:       "<core-foo-test2-hash>",
							},
						},
					},
					mca: addontesting.NewAddon("test", "cluster2"),
				},
				{
					desiredConfigs: map[addonv1alpha1.ConfigGroupResource]addonv1alpha1.ConfigReference{
						{Group: "core", Resource: "Bar"}: {
							ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Bar"},
							ConfigReferent:      addonv1alpha1.ConfigReferent{Name: "test"},
							DesiredConfig: &addonv1alpha1.ConfigSpecHash{
								ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test"},
								SpecHash:       "<core-bar-test-hash>",
							},
						},
						{Group: "core", Resource: "Foo"}: {
							ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
							ConfigReferent:      addonv1alpha1.ConfigReferent{Name: "test"},
							DesiredConfig: &addonv1alpha1.ConfigSpecHash{
								ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test"},
								SpecHash:       "<core-foo-test-hash>",
							},
						},
					},
					mca: addontesting.NewAddon("test", "cluster3"),
				},
			},
		},
		{
			name: "mca override",
			defaultConfigs: []addonv1alpha1.ConfigMeta{
				{ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Bar"},
					DefaultConfig: &addonv1alpha1.ConfigReferent{Name: "test"}},
				{ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
					DefaultConfig: &addonv1alpha1.ConfigReferent{Name: "test"}},
			},
			defaultConfigReference: []addonv1alpha1.DefaultConfigReference{
				newDefaultConfigReference("core", "Bar", "test", "<core-bar-test-hash>"),
				newDefaultConfigReference("core", "Foo", "test", "<core-foo-test-hash>"),
			},
			addons: []*addonv1alpha1.ManagedClusterAddOn{
				newManagedClusterAddon("test", "cluster1", []addonv1alpha1.AddOnConfig{
					{ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Bar"},
						ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test1"}},
				}, nil),
				addontesting.NewAddon("test", "cluster2"),
				addontesting.NewAddon("test", "cluster3"),
			},
			placementStrategies: []placementStrategy{
				{configs: []addonv1alpha1.AddOnConfig{
					{ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test1"}},
				}, clusters: []string{"cluster1"}},
				{configs: []addonv1alpha1.AddOnConfig{
					{ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Bar"},
						ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test2"}},
					{ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
						ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test2"}},
				}, clusters: []string{"cluster2"}},
			},
			installProgressions: []addonv1alpha1.InstallProgression{
				{
					ConfigReferences: []addonv1alpha1.InstallConfigReference{
						newInstallConfigReference("core", "Foo", "test1", "<core-foo-test1-hash>"),
					},
				},
				{
					ConfigReferences: []addonv1alpha1.InstallConfigReference{
						newInstallConfigReference("core", "Bar", "test2", "<core-bar-test2-hash>"),
						newInstallConfigReference("core", "Foo", "test2", "<core-foo-test2-hash>"),
					},
				},
			},
			expected: []*addonNode{
				{
					desiredConfigs: map[addonv1alpha1.ConfigGroupResource]addonv1alpha1.ConfigReference{
						{Group: "core", Resource: "Bar"}: {
							ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Bar"},
							ConfigReferent:      addonv1alpha1.ConfigReferent{Name: "test1"},
							DesiredConfig: &addonv1alpha1.ConfigSpecHash{
								ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test1"},
								SpecHash:       "",
							},
						},
						{Group: "core", Resource: "Foo"}: {
							ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
							ConfigReferent:      addonv1alpha1.ConfigReferent{Name: "test1"},
							DesiredConfig: &addonv1alpha1.ConfigSpecHash{
								ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test1"},
								SpecHash:       "<core-foo-test1-hash>",
							},
						},
					},
					mca: newManagedClusterAddon("test", "cluster1", []addonv1alpha1.AddOnConfig{
						{ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Bar"},
							ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test1"}},
					}, nil),
				},
				{
					desiredConfigs: map[addonv1alpha1.ConfigGroupResource]addonv1alpha1.ConfigReference{
						{Group: "core", Resource: "Bar"}: {
							ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Bar"},
							ConfigReferent:      addonv1alpha1.ConfigReferent{Name: "test2"},
							DesiredConfig: &addonv1alpha1.ConfigSpecHash{
								ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test2"},
								SpecHash:       "<core-bar-test2-hash>",
							},
						},
						{Group: "core", Resource: "Foo"}: {
							ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
							ConfigReferent:      addonv1alpha1.ConfigReferent{Name: "test2"},
							DesiredConfig: &addonv1alpha1.ConfigSpecHash{
								ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test2"},
								SpecHash:       "<core-foo-test2-hash>",
							},
						},
					},
					mca: addontesting.NewAddon("test", "cluster2"),
				},
				{
					desiredConfigs: map[addonv1alpha1.ConfigGroupResource]addonv1alpha1.ConfigReference{
						{Group: "core", Resource: "Bar"}: {
							ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Bar"},
							ConfigReferent:      addonv1alpha1.ConfigReferent{Name: "test"},
							DesiredConfig: &addonv1alpha1.ConfigSpecHash{
								ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test"},
								SpecHash:       "<core-bar-test-hash>",
							},
						},
						{Group: "core", Resource: "Foo"}: {
							ConfigGroupResource: addonv1alpha1.ConfigGroupResource{Group: "core", Resource: "Foo"},
							ConfigReferent:      addonv1alpha1.ConfigReferent{Name: "test"},
							DesiredConfig: &addonv1alpha1.ConfigSpecHash{
								ConfigReferent: addonv1alpha1.ConfigReferent{Name: "test"},
								SpecHash:       "<core-foo-test-hash>",
							},
						},
					},
					mca: addontesting.NewAddon("test", "cluster3"),
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			graph := newGraph(c.defaultConfigs, c.defaultConfigReference)
			for _, addon := range c.addons {
				graph.addAddonNode(addon)
			}
			for i, strategy := range c.placementStrategies {
				graph.addPlacementNode(c.installProgressions[i].ConfigReferences, strategy.clusters)
			}

			actual := graph.addonToUpdate()
			if len(actual) != len(c.expected) {
				t.Errorf("output length is not correct, expected %v, got %v", len(c.expected), len(actual))
			}

			for _, ev := range c.expected {
				compared := false
				for _, v := range actual {
					if v == nil || ev == nil {
						t.Errorf("addonNode should not be nil")
					}
					if ev.mca != nil && v.mca != nil && ev.mca.Namespace == v.mca.Namespace {
						if !reflect.DeepEqual(v, ev) {
							t.Errorf("output is not correct, cluster %s, expected %v, got %v", v.mca.Namespace, ev, v)
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

func newInstallConfigReference(group, resource, name, hash string) addonv1alpha1.InstallConfigReference {
	return addonv1alpha1.InstallConfigReference{
		ConfigGroupResource: addonv1alpha1.ConfigGroupResource{
			Group:    group,
			Resource: resource,
		},
		DesiredConfig: &addonv1alpha1.ConfigSpecHash{
			ConfigReferent: addonv1alpha1.ConfigReferent{Name: name},
			SpecHash:       hash,
		},
	}
}

func newDefaultConfigReference(group, resource, name, hash string) addonv1alpha1.DefaultConfigReference {
	return addonv1alpha1.DefaultConfigReference{
		ConfigGroupResource: addonv1alpha1.ConfigGroupResource{
			Group:    group,
			Resource: resource,
		},
		DesiredConfig: &addonv1alpha1.ConfigSpecHash{
			ConfigReferent: addonv1alpha1.ConfigReferent{Name: name},
			SpecHash:       hash,
		},
	}
}
