// Copyright Contributors to the Open Cluster Management project

package conversion

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	clusterv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
)

func TestConvertAddOnConfigsToV1Beta1(t *testing.T) {
	cases := []struct {
		name     string
		input    []addonv1alpha1.AddOnConfig
		expected []addonv1beta1.AddOnConfig
	}{
		{
			name:     "nil configs",
			input:    nil,
			expected: nil,
		},
		{
			name:     "empty configs",
			input:    []addonv1alpha1.AddOnConfig{},
			expected: []addonv1beta1.AddOnConfig{},
		},
		{
			name: "with namespace and name",
			input: []addonv1alpha1.AddOnConfig{
				{
					ConfigGroupResource: addonv1alpha1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					ConfigReferent: addonv1alpha1.ConfigReferent{
						Namespace: "default",
						Name:      "config1",
					},
				},
			},
			expected: []addonv1beta1.AddOnConfig{
				{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					ConfigReferent: addonv1beta1.ConfigReferent{
						Namespace: "default",
						Name:      "config1",
					},
				},
			},
		},
		{
			name: "multiple configs",
			input: []addonv1alpha1.AddOnConfig{
				{
					ConfigGroupResource: addonv1alpha1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					ConfigReferent: addonv1alpha1.ConfigReferent{
						Namespace: "default",
						Name:      "config1",
					},
				},
				{
					ConfigGroupResource: addonv1alpha1.ConfigGroupResource{
						Group:    "config.open-cluster-management.io",
						Resource: "customconfigs",
					},
					ConfigReferent: addonv1alpha1.ConfigReferent{
						Name: "config2",
					},
				},
			},
			expected: []addonv1beta1.AddOnConfig{
				{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					ConfigReferent: addonv1beta1.ConfigReferent{
						Namespace: "default",
						Name:      "config1",
					},
				},
				{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{
						Group:    "config.open-cluster-management.io",
						Resource: "customconfigs",
					},
					ConfigReferent: addonv1beta1.ConfigReferent{
						Name: "config2",
					},
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := ConvertAddOnConfigsToV1Beta1(c.input)
			if diff := cmp.Diff(c.expected, result); diff != "" {
				t.Errorf("conversion mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestConvertAddOnConfigsFromV1Beta1(t *testing.T) {
	cases := []struct {
		name     string
		input    []addonv1beta1.AddOnConfig
		expected []addonv1alpha1.AddOnConfig
	}{
		{
			name:     "nil configs",
			input:    nil,
			expected: nil,
		},
		{
			name:     "empty configs",
			input:    []addonv1beta1.AddOnConfig{},
			expected: []addonv1alpha1.AddOnConfig{},
		},
		{
			name: "with namespace and name",
			input: []addonv1beta1.AddOnConfig{
				{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					ConfigReferent: addonv1beta1.ConfigReferent{
						Namespace: "default",
						Name:      "config1",
					},
				},
			},
			expected: []addonv1alpha1.AddOnConfig{
				{
					ConfigGroupResource: addonv1alpha1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					ConfigReferent: addonv1alpha1.ConfigReferent{
						Namespace: "default",
						Name:      "config1",
					},
				},
			},
		},
		{
			name: "multiple configs",
			input: []addonv1beta1.AddOnConfig{
				{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					ConfigReferent: addonv1beta1.ConfigReferent{
						Namespace: "default",
						Name:      "config1",
					},
				},
				{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{
						Group:    "config.open-cluster-management.io",
						Resource: "customconfigs",
					},
					ConfigReferent: addonv1beta1.ConfigReferent{
						Name: "config2",
					},
				},
			},
			expected: []addonv1alpha1.AddOnConfig{
				{
					ConfigGroupResource: addonv1alpha1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					ConfigReferent: addonv1alpha1.ConfigReferent{
						Namespace: "default",
						Name:      "config1",
					},
				},
				{
					ConfigGroupResource: addonv1alpha1.ConfigGroupResource{
						Group:    "config.open-cluster-management.io",
						Resource: "customconfigs",
					},
					ConfigReferent: addonv1alpha1.ConfigReferent{
						Name: "config2",
					},
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := ConvertAddOnConfigsFromV1Beta1(c.input)
			if diff := cmp.Diff(c.expected, result); diff != "" {
				t.Errorf("conversion mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestConvertConfigMetaToAddOnConfig(t *testing.T) {
	cases := []struct {
		name     string
		input    []addonv1alpha1.ConfigMeta
		expected []addonv1beta1.AddOnConfig
	}{
		{
			name:     "empty config metas",
			input:    []addonv1alpha1.ConfigMeta{},
			expected: nil,
		},
		{
			name: "with default config",
			input: []addonv1alpha1.ConfigMeta{
				{
					ConfigGroupResource: addonv1alpha1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					DefaultConfig: &addonv1alpha1.ConfigReferent{
						Namespace: "default",
						Name:      "config1",
					},
				},
			},
			expected: []addonv1beta1.AddOnConfig{
				{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					ConfigReferent: addonv1beta1.ConfigReferent{
						Namespace: "default",
						Name:      "config1",
					},
				},
			},
		},
		{
			name: "without default config - should use PLACEHOLDER",
			input: []addonv1alpha1.ConfigMeta{
				{
					ConfigGroupResource: addonv1alpha1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					DefaultConfig: nil,
				},
			},
			expected: []addonv1beta1.AddOnConfig{
				{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					ConfigReferent: addonv1beta1.ConfigReferent{
						Name: ReservedNoDefaultConfigName,
					},
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := ConvertConfigMetaToAddOnConfig(c.input)
			if diff := cmp.Diff(c.expected, result); diff != "" {
				t.Errorf("conversion mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestConvertAddOnConfigToConfigMeta(t *testing.T) {
	cases := []struct {
		name     string
		input    []addonv1beta1.AddOnConfig
		expected []addonv1alpha1.ConfigMeta
	}{
		{
			name:     "empty addOn configs",
			input:    []addonv1beta1.AddOnConfig{},
			expected: nil,
		},
		{
			name: "with config referent",
			input: []addonv1beta1.AddOnConfig{
				{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					ConfigReferent: addonv1beta1.ConfigReferent{
						Namespace: "default",
						Name:      "config1",
					},
				},
			},
			expected: []addonv1alpha1.ConfigMeta{
				{
					ConfigGroupResource: addonv1alpha1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					DefaultConfig: &addonv1alpha1.ConfigReferent{
						Namespace: "default",
						Name:      "config1",
					},
				},
			},
		},
		{
			name: "with reserved sentinel - should convert to nil",
			input: []addonv1beta1.AddOnConfig{
				{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					ConfigReferent: addonv1beta1.ConfigReferent{
						Name: ReservedNoDefaultConfigName,
					},
				},
			},
			expected: []addonv1alpha1.ConfigMeta{
				{
					ConfigGroupResource: addonv1alpha1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					DefaultConfig: nil,
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := ConvertAddOnConfigToConfigMeta(c.input)
			if diff := cmp.Diff(c.expected, result); diff != "" {
				t.Errorf("conversion mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestConvertInstallStrategyToV1Beta1(t *testing.T) {
	cases := []struct {
		name     string
		input    addonv1alpha1.InstallStrategy
		expected addonv1beta1.InstallStrategy
	}{
		{
			name: "empty install strategy",
			input: addonv1alpha1.InstallStrategy{
				Type: addonv1alpha1.AddonInstallStrategyManual,
			},
			expected: addonv1beta1.InstallStrategy{
				Type: addonv1beta1.AddonInstallStrategyManual,
			},
		},
		{
			name: "with placements",
			input: addonv1alpha1.InstallStrategy{
				Type: addonv1alpha1.AddonInstallStrategyPlacements,
				Placements: []addonv1alpha1.PlacementStrategy{
					{
						PlacementRef: addonv1alpha1.PlacementRef{
							Name:      "placement1",
							Namespace: "default",
						},
						Configs: []addonv1alpha1.AddOnConfig{
							{
								ConfigGroupResource: addonv1alpha1.ConfigGroupResource{
									Group:    "addon.open-cluster-management.io",
									Resource: "addondeploymentconfigs",
								},
								ConfigReferent: addonv1alpha1.ConfigReferent{
									Name:      "config1",
									Namespace: "default",
								},
							},
						},
						RolloutStrategy: clusterv1alpha1.RolloutStrategy{
							Type: clusterv1alpha1.All,
						},
					},
				},
			},
			expected: addonv1beta1.InstallStrategy{
				Type: addonv1beta1.AddonInstallStrategyPlacements,
				Placements: []addonv1beta1.PlacementStrategy{
					{
						PlacementRef: addonv1beta1.PlacementRef{
							Name:      "placement1",
							Namespace: "default",
						},
						Configs: []addonv1beta1.AddOnConfig{
							{
								ConfigGroupResource: addonv1beta1.ConfigGroupResource{
									Group:    "addon.open-cluster-management.io",
									Resource: "addondeploymentconfigs",
								},
								ConfigReferent: addonv1beta1.ConfigReferent{
									Name:      "config1",
									Namespace: "default",
								},
							},
						},
						RolloutStrategy: clusterv1alpha1.RolloutStrategy{
							Type: clusterv1alpha1.All,
						},
					},
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := ConvertInstallStrategyToV1Beta1(c.input)
			if diff := cmp.Diff(c.expected, result); diff != "" {
				t.Errorf("conversion mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestConvertInstallStrategyFromV1Beta1(t *testing.T) {
	cases := []struct {
		name     string
		input    addonv1beta1.InstallStrategy
		expected addonv1alpha1.InstallStrategy
	}{
		{
			name: "empty install strategy",
			input: addonv1beta1.InstallStrategy{
				Type: addonv1beta1.AddonInstallStrategyManual,
			},
			expected: addonv1alpha1.InstallStrategy{
				Type: addonv1alpha1.AddonInstallStrategyManual,
			},
		},
		{
			name: "with placements",
			input: addonv1beta1.InstallStrategy{
				Type: addonv1beta1.AddonInstallStrategyPlacements,
				Placements: []addonv1beta1.PlacementStrategy{
					{
						PlacementRef: addonv1beta1.PlacementRef{
							Name:      "placement1",
							Namespace: "default",
						},
						Configs: []addonv1beta1.AddOnConfig{
							{
								ConfigGroupResource: addonv1beta1.ConfigGroupResource{
									Group:    "addon.open-cluster-management.io",
									Resource: "addondeploymentconfigs",
								},
								ConfigReferent: addonv1beta1.ConfigReferent{
									Name:      "config1",
									Namespace: "default",
								},
							},
						},
						RolloutStrategy: clusterv1alpha1.RolloutStrategy{
							Type: clusterv1alpha1.All,
						},
					},
				},
			},
			expected: addonv1alpha1.InstallStrategy{
				Type: addonv1alpha1.AddonInstallStrategyPlacements,
				Placements: []addonv1alpha1.PlacementStrategy{
					{
						PlacementRef: addonv1alpha1.PlacementRef{
							Name:      "placement1",
							Namespace: "default",
						},
						Configs: []addonv1alpha1.AddOnConfig{
							{
								ConfigGroupResource: addonv1alpha1.ConfigGroupResource{
									Group:    "addon.open-cluster-management.io",
									Resource: "addondeploymentconfigs",
								},
								ConfigReferent: addonv1alpha1.ConfigReferent{
									Name:      "config1",
									Namespace: "default",
								},
							},
						},
						RolloutStrategy: clusterv1alpha1.RolloutStrategy{
							Type: clusterv1alpha1.All,
						},
					},
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := ConvertInstallStrategyFromV1Beta1(c.input)
			if diff := cmp.Diff(c.expected, result); diff != "" {
				t.Errorf("conversion mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestConvertConfigSpecHashToV1Beta1(t *testing.T) {
	cases := []struct {
		name     string
		input    *addonv1alpha1.ConfigSpecHash
		expected *addonv1beta1.ConfigSpecHash
	}{
		{
			name:     "nil config spec hash",
			input:    nil,
			expected: nil,
		},
		{
			name: "with namespace and name",
			input: &addonv1alpha1.ConfigSpecHash{
				ConfigReferent: addonv1alpha1.ConfigReferent{
					Namespace: "default",
					Name:      "config1",
				},
				SpecHash: "abc123",
			},
			expected: &addonv1beta1.ConfigSpecHash{
				ConfigReferent: addonv1beta1.ConfigReferent{
					Namespace: "default",
					Name:      "config1",
				},
				SpecHash: "abc123",
			},
		},
		{
			name: "without namespace",
			input: &addonv1alpha1.ConfigSpecHash{
				ConfigReferent: addonv1alpha1.ConfigReferent{
					Name: "config1",
				},
				SpecHash: "xyz789",
			},
			expected: &addonv1beta1.ConfigSpecHash{
				ConfigReferent: addonv1beta1.ConfigReferent{
					Name: "config1",
				},
				SpecHash: "xyz789",
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := ConvertConfigSpecHashToV1Beta1(c.input)
			if diff := cmp.Diff(c.expected, result); diff != "" {
				t.Errorf("conversion mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestConvertConfigSpecHashFromV1Beta1(t *testing.T) {
	cases := []struct {
		name     string
		input    *addonv1beta1.ConfigSpecHash
		expected *addonv1alpha1.ConfigSpecHash
	}{
		{
			name:     "nil config spec hash",
			input:    nil,
			expected: nil,
		},
		{
			name: "with namespace and name",
			input: &addonv1beta1.ConfigSpecHash{
				ConfigReferent: addonv1beta1.ConfigReferent{
					Namespace: "default",
					Name:      "config1",
				},
				SpecHash: "abc123",
			},
			expected: &addonv1alpha1.ConfigSpecHash{
				ConfigReferent: addonv1alpha1.ConfigReferent{
					Namespace: "default",
					Name:      "config1",
				},
				SpecHash: "abc123",
			},
		},
		{
			name: "without namespace",
			input: &addonv1beta1.ConfigSpecHash{
				ConfigReferent: addonv1beta1.ConfigReferent{
					Name: "config1",
				},
				SpecHash: "xyz789",
			},
			expected: &addonv1alpha1.ConfigSpecHash{
				ConfigReferent: addonv1alpha1.ConfigReferent{
					Name: "config1",
				},
				SpecHash: "xyz789",
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := ConvertConfigSpecHashFromV1Beta1(c.input)
			if diff := cmp.Diff(c.expected, result); diff != "" {
				t.Errorf("conversion mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestConvertDefaultConfigReferencesToV1Beta1(t *testing.T) {
	cases := []struct {
		name     string
		input    []addonv1alpha1.DefaultConfigReference
		expected []addonv1beta1.DefaultConfigReference
	}{
		{
			name:     "empty default config references",
			input:    []addonv1alpha1.DefaultConfigReference{},
			expected: nil,
		},
		{
			name: "with desired config",
			input: []addonv1alpha1.DefaultConfigReference{
				{
					ConfigGroupResource: addonv1alpha1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					DesiredConfig: &addonv1alpha1.ConfigSpecHash{
						ConfigReferent: addonv1alpha1.ConfigReferent{
							Namespace: "default",
							Name:      "config1",
						},
						SpecHash: "hash123",
					},
				},
			},
			expected: []addonv1beta1.DefaultConfigReference{
				{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					DesiredConfig: &addonv1beta1.ConfigSpecHash{
						ConfigReferent: addonv1beta1.ConfigReferent{
							Namespace: "default",
							Name:      "config1",
						},
						SpecHash: "hash123",
					},
				},
			},
		},
		{
			name: "with nil desired config",
			input: []addonv1alpha1.DefaultConfigReference{
				{
					ConfigGroupResource: addonv1alpha1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					DesiredConfig: nil,
				},
			},
			expected: []addonv1beta1.DefaultConfigReference{
				{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					DesiredConfig: nil,
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := ConvertDefaultConfigReferencesToV1Beta1(c.input)
			if diff := cmp.Diff(c.expected, result); diff != "" {
				t.Errorf("conversion mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestConvertDefaultConfigReferencesFromV1Beta1(t *testing.T) {
	cases := []struct {
		name     string
		input    []addonv1beta1.DefaultConfigReference
		expected []addonv1alpha1.DefaultConfigReference
	}{
		{
			name:     "empty default config references",
			input:    []addonv1beta1.DefaultConfigReference{},
			expected: nil,
		},
		{
			name: "with desired config",
			input: []addonv1beta1.DefaultConfigReference{
				{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					DesiredConfig: &addonv1beta1.ConfigSpecHash{
						ConfigReferent: addonv1beta1.ConfigReferent{
							Namespace: "default",
							Name:      "config1",
						},
						SpecHash: "hash123",
					},
				},
			},
			expected: []addonv1alpha1.DefaultConfigReference{
				{
					ConfigGroupResource: addonv1alpha1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					DesiredConfig: &addonv1alpha1.ConfigSpecHash{
						ConfigReferent: addonv1alpha1.ConfigReferent{
							Namespace: "default",
							Name:      "config1",
						},
						SpecHash: "hash123",
					},
				},
			},
		},
		{
			name: "with nil desired config",
			input: []addonv1beta1.DefaultConfigReference{
				{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					DesiredConfig: nil,
				},
			},
			expected: []addonv1alpha1.DefaultConfigReference{
				{
					ConfigGroupResource: addonv1alpha1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					DesiredConfig: nil,
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := ConvertDefaultConfigReferencesFromV1Beta1(c.input)
			if diff := cmp.Diff(c.expected, result); diff != "" {
				t.Errorf("conversion mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestConvertInstallConfigReferencesToV1Beta1(t *testing.T) {
	cases := []struct {
		name     string
		input    []addonv1alpha1.InstallConfigReference
		expected []addonv1beta1.InstallConfigReference
	}{
		{
			name:     "empty install config references",
			input:    []addonv1alpha1.InstallConfigReference{},
			expected: nil,
		},
		{
			name: "with all config fields",
			input: []addonv1alpha1.InstallConfigReference{
				{
					ConfigGroupResource: addonv1alpha1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					DesiredConfig: &addonv1alpha1.ConfigSpecHash{
						ConfigReferent: addonv1alpha1.ConfigReferent{
							Namespace: "default",
							Name:      "config1",
						},
						SpecHash: "desired-hash",
					},
					LastAppliedConfig: &addonv1alpha1.ConfigSpecHash{
						ConfigReferent: addonv1alpha1.ConfigReferent{
							Namespace: "default",
							Name:      "config1",
						},
						SpecHash: "applied-hash",
					},
					LastKnownGoodConfig: &addonv1alpha1.ConfigSpecHash{
						ConfigReferent: addonv1alpha1.ConfigReferent{
							Namespace: "default",
							Name:      "config0",
						},
						SpecHash: "good-hash",
					},
				},
			},
			expected: []addonv1beta1.InstallConfigReference{
				{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					DesiredConfig: &addonv1beta1.ConfigSpecHash{
						ConfigReferent: addonv1beta1.ConfigReferent{
							Namespace: "default",
							Name:      "config1",
						},
						SpecHash: "desired-hash",
					},
					LastAppliedConfig: &addonv1beta1.ConfigSpecHash{
						ConfigReferent: addonv1beta1.ConfigReferent{
							Namespace: "default",
							Name:      "config1",
						},
						SpecHash: "applied-hash",
					},
					LastKnownGoodConfig: &addonv1beta1.ConfigSpecHash{
						ConfigReferent: addonv1beta1.ConfigReferent{
							Namespace: "default",
							Name:      "config0",
						},
						SpecHash: "good-hash",
					},
				},
			},
		},
		{
			name: "with nil config fields",
			input: []addonv1alpha1.InstallConfigReference{
				{
					ConfigGroupResource: addonv1alpha1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					DesiredConfig:       nil,
					LastAppliedConfig:   nil,
					LastKnownGoodConfig: nil,
				},
			},
			expected: []addonv1beta1.InstallConfigReference{
				{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					DesiredConfig:       nil,
					LastAppliedConfig:   nil,
					LastKnownGoodConfig: nil,
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := ConvertInstallConfigReferencesToV1Beta1(c.input)
			if diff := cmp.Diff(c.expected, result); diff != "" {
				t.Errorf("conversion mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestConvertInstallConfigReferencesFromV1Beta1(t *testing.T) {
	cases := []struct {
		name     string
		input    []addonv1beta1.InstallConfigReference
		expected []addonv1alpha1.InstallConfigReference
	}{
		{
			name:     "empty install config references",
			input:    []addonv1beta1.InstallConfigReference{},
			expected: nil,
		},
		{
			name: "with all config fields",
			input: []addonv1beta1.InstallConfigReference{
				{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					DesiredConfig: &addonv1beta1.ConfigSpecHash{
						ConfigReferent: addonv1beta1.ConfigReferent{
							Namespace: "default",
							Name:      "config1",
						},
						SpecHash: "desired-hash",
					},
					LastAppliedConfig: &addonv1beta1.ConfigSpecHash{
						ConfigReferent: addonv1beta1.ConfigReferent{
							Namespace: "default",
							Name:      "config1",
						},
						SpecHash: "applied-hash",
					},
					LastKnownGoodConfig: &addonv1beta1.ConfigSpecHash{
						ConfigReferent: addonv1beta1.ConfigReferent{
							Namespace: "default",
							Name:      "config0",
						},
						SpecHash: "good-hash",
					},
				},
			},
			expected: []addonv1alpha1.InstallConfigReference{
				{
					ConfigGroupResource: addonv1alpha1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					DesiredConfig: &addonv1alpha1.ConfigSpecHash{
						ConfigReferent: addonv1alpha1.ConfigReferent{
							Namespace: "default",
							Name:      "config1",
						},
						SpecHash: "desired-hash",
					},
					LastAppliedConfig: &addonv1alpha1.ConfigSpecHash{
						ConfigReferent: addonv1alpha1.ConfigReferent{
							Namespace: "default",
							Name:      "config1",
						},
						SpecHash: "applied-hash",
					},
					LastKnownGoodConfig: &addonv1alpha1.ConfigSpecHash{
						ConfigReferent: addonv1alpha1.ConfigReferent{
							Namespace: "default",
							Name:      "config0",
						},
						SpecHash: "good-hash",
					},
				},
			},
		},
		{
			name: "with nil config fields",
			input: []addonv1beta1.InstallConfigReference{
				{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					DesiredConfig:       nil,
					LastAppliedConfig:   nil,
					LastKnownGoodConfig: nil,
				},
			},
			expected: []addonv1alpha1.InstallConfigReference{
				{
					ConfigGroupResource: addonv1alpha1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					DesiredConfig:       nil,
					LastAppliedConfig:   nil,
					LastKnownGoodConfig: nil,
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := ConvertInstallConfigReferencesFromV1Beta1(c.input)
			if diff := cmp.Diff(c.expected, result); diff != "" {
				t.Errorf("conversion mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestConvertInstallProgressionsToV1Beta1(t *testing.T) {
	cases := []struct {
		name     string
		input    []addonv1alpha1.InstallProgression
		expected []addonv1beta1.InstallProgression
	}{
		{
			name:     "empty install progressions",
			input:    []addonv1alpha1.InstallProgression{},
			expected: nil,
		},
		{
			name: "with placement ref and config references",
			input: []addonv1alpha1.InstallProgression{
				{
					PlacementRef: addonv1alpha1.PlacementRef{
						Name:      "placement1",
						Namespace: "default",
					},
					ConfigReferences: []addonv1alpha1.InstallConfigReference{
						{
							ConfigGroupResource: addonv1alpha1.ConfigGroupResource{
								Group:    "addon.open-cluster-management.io",
								Resource: "addondeploymentconfigs",
							},
							DesiredConfig: &addonv1alpha1.ConfigSpecHash{
								ConfigReferent: addonv1alpha1.ConfigReferent{
									Namespace: "default",
									Name:      "config1",
								},
								SpecHash: "hash123",
							},
						},
					},
				},
			},
			expected: []addonv1beta1.InstallProgression{
				{
					PlacementRef: addonv1beta1.PlacementRef{
						Name:      "placement1",
						Namespace: "default",
					},
					ConfigReferences: []addonv1beta1.InstallConfigReference{
						{
							ConfigGroupResource: addonv1beta1.ConfigGroupResource{
								Group:    "addon.open-cluster-management.io",
								Resource: "addondeploymentconfigs",
							},
							DesiredConfig: &addonv1beta1.ConfigSpecHash{
								ConfigReferent: addonv1beta1.ConfigReferent{
									Namespace: "default",
									Name:      "config1",
								},
								SpecHash: "hash123",
							},
						},
					},
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := ConvertInstallProgressionsToV1Beta1(c.input)
			if diff := cmp.Diff(c.expected, result); diff != "" {
				t.Errorf("conversion mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestConvertInstallProgressionsFromV1Beta1(t *testing.T) {
	cases := []struct {
		name     string
		input    []addonv1beta1.InstallProgression
		expected []addonv1alpha1.InstallProgression
	}{
		{
			name:     "empty install progressions",
			input:    []addonv1beta1.InstallProgression{},
			expected: nil,
		},
		{
			name: "with placement ref and config references",
			input: []addonv1beta1.InstallProgression{
				{
					PlacementRef: addonv1beta1.PlacementRef{
						Name:      "placement1",
						Namespace: "default",
					},
					ConfigReferences: []addonv1beta1.InstallConfigReference{
						{
							ConfigGroupResource: addonv1beta1.ConfigGroupResource{
								Group:    "addon.open-cluster-management.io",
								Resource: "addondeploymentconfigs",
							},
							DesiredConfig: &addonv1beta1.ConfigSpecHash{
								ConfigReferent: addonv1beta1.ConfigReferent{
									Namespace: "default",
									Name:      "config1",
								},
								SpecHash: "hash123",
							},
						},
					},
				},
			},
			expected: []addonv1alpha1.InstallProgression{
				{
					PlacementRef: addonv1alpha1.PlacementRef{
						Name:      "placement1",
						Namespace: "default",
					},
					ConfigReferences: []addonv1alpha1.InstallConfigReference{
						{
							ConfigGroupResource: addonv1alpha1.ConfigGroupResource{
								Group:    "addon.open-cluster-management.io",
								Resource: "addondeploymentconfigs",
							},
							DesiredConfig: &addonv1alpha1.ConfigSpecHash{
								ConfigReferent: addonv1alpha1.ConfigReferent{
									Namespace: "default",
									Name:      "config1",
								},
								SpecHash: "hash123",
							},
						},
					},
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := ConvertInstallProgressionsFromV1Beta1(c.input)
			if diff := cmp.Diff(c.expected, result); diff != "" {
				t.Errorf("conversion mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
