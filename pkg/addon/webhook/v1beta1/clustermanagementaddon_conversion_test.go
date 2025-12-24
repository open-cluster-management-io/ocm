// Copyright Contributors to the Open Cluster Management project

package v1beta1

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	clusterv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"

	internalv1alpha1 "open-cluster-management.io/ocm/pkg/addon/webhook/v1alpha1"
)

func TestClusterManagementAddOnConvertTo(t *testing.T) {
	cases := []struct {
		name     string
		src      *ClusterManagementAddOn
		expected *internalv1alpha1.ClusterManagementAddOn
	}{
		{
			name: "complete conversion with all fields",
			src: &ClusterManagementAddOn{
				ClusterManagementAddOn: addonv1beta1.ClusterManagementAddOn{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-addon",
					},
					Spec: addonv1beta1.ClusterManagementAddOnSpec{
						AddOnMeta: addonv1beta1.AddOnMeta{
							DisplayName: "Test AddOn",
							Description: "Test description",
						},
						DefaultConfigs: []addonv1beta1.AddOnConfig{
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
						InstallStrategy: addonv1beta1.InstallStrategy{
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
					Status: addonv1beta1.ClusterManagementAddOnStatus{
						DefaultConfigReferences: []addonv1beta1.DefaultConfigReference{
							{
								ConfigGroupResource: addonv1beta1.ConfigGroupResource{
									Group:    "addon.open-cluster-management.io",
									Resource: "addondeploymentconfigs",
								},
								DesiredConfig: &addonv1beta1.ConfigSpecHash{
									ConfigReferent: addonv1beta1.ConfigReferent{
										Name:      "config1",
										Namespace: "default",
									},
									SpecHash: "hash123",
								},
							},
						},
						InstallProgressions: []addonv1beta1.InstallProgression{
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
												Name:      "config1",
												Namespace: "default",
											},
											SpecHash: "hash123",
										},
									},
								},
							},
						},
					},
				},
			},
			expected: &internalv1alpha1.ClusterManagementAddOn{
				ClusterManagementAddOn: addonv1alpha1.ClusterManagementAddOn{
					TypeMeta: metav1.TypeMeta{
						Kind:       "ClusterManagementAddOn",
						APIVersion: "addon.open-cluster-management.io/v1alpha1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-addon",
					},
					Spec: addonv1alpha1.ClusterManagementAddOnSpec{
						AddOnMeta: addonv1alpha1.AddOnMeta{
							DisplayName: "Test AddOn",
							Description: "Test description",
						},
						SupportedConfigs: []addonv1alpha1.ConfigMeta{
							{
								ConfigGroupResource: addonv1alpha1.ConfigGroupResource{
									Group:    "addon.open-cluster-management.io",
									Resource: "addondeploymentconfigs",
								},
								DefaultConfig: &addonv1alpha1.ConfigReferent{
									Name:      "config1",
									Namespace: "default",
								},
							},
						},
						InstallStrategy: addonv1alpha1.InstallStrategy{
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
					Status: addonv1alpha1.ClusterManagementAddOnStatus{
						DefaultConfigReferences: []addonv1alpha1.DefaultConfigReference{
							{
								ConfigGroupResource: addonv1alpha1.ConfigGroupResource{
									Group:    "addon.open-cluster-management.io",
									Resource: "addondeploymentconfigs",
								},
								DesiredConfig: &addonv1alpha1.ConfigSpecHash{
									ConfigReferent: addonv1alpha1.ConfigReferent{
										Name:      "config1",
										Namespace: "default",
									},
									SpecHash: "hash123",
								},
							},
						},
						InstallProgressions: []addonv1alpha1.InstallProgression{
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
												Name:      "config1",
												Namespace: "default",
											},
											SpecHash: "hash123",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dst := &internalv1alpha1.ClusterManagementAddOn{}
			if err := tc.src.ConvertTo(dst); err != nil {
				t.Fatalf("ConvertTo() failed: %v", err)
			}

			if diff := cmp.Diff(tc.expected, dst); diff != "" {
				t.Errorf("ConvertTo() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestClusterManagementAddOnConvertFrom(t *testing.T) {
	cases := []struct {
		name     string
		src      *internalv1alpha1.ClusterManagementAddOn
		expected *ClusterManagementAddOn
	}{
		{
			name: "complete conversion with all fields",
			src: &internalv1alpha1.ClusterManagementAddOn{
				ClusterManagementAddOn: addonv1alpha1.ClusterManagementAddOn{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-addon",
					},
					Spec: addonv1alpha1.ClusterManagementAddOnSpec{
						AddOnMeta: addonv1alpha1.AddOnMeta{
							DisplayName: "Test AddOn",
							Description: "Test description",
						},
						SupportedConfigs: []addonv1alpha1.ConfigMeta{
							{
								ConfigGroupResource: addonv1alpha1.ConfigGroupResource{
									Group:    "addon.open-cluster-management.io",
									Resource: "addondeploymentconfigs",
								},
								DefaultConfig: &addonv1alpha1.ConfigReferent{
									Name:      "config1",
									Namespace: "default",
								},
							},
						},
						InstallStrategy: addonv1alpha1.InstallStrategy{
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
					Status: addonv1alpha1.ClusterManagementAddOnStatus{
						DefaultConfigReferences: []addonv1alpha1.DefaultConfigReference{
							{
								ConfigGroupResource: addonv1alpha1.ConfigGroupResource{
									Group:    "addon.open-cluster-management.io",
									Resource: "addondeploymentconfigs",
								},
								DesiredConfig: &addonv1alpha1.ConfigSpecHash{
									ConfigReferent: addonv1alpha1.ConfigReferent{
										Name:      "config1",
										Namespace: "default",
									},
									SpecHash: "hash123",
								},
							},
						},
						InstallProgressions: []addonv1alpha1.InstallProgression{
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
												Name:      "config1",
												Namespace: "default",
											},
											SpecHash: "hash123",
										},
									},
								},
							},
						},
					},
				},
			},
			expected: &ClusterManagementAddOn{
				ClusterManagementAddOn: addonv1beta1.ClusterManagementAddOn{
					TypeMeta: metav1.TypeMeta{
						Kind:       "ClusterManagementAddOn",
						APIVersion: "addon.open-cluster-management.io/v1beta1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-addon",
					},
					Spec: addonv1beta1.ClusterManagementAddOnSpec{
						AddOnMeta: addonv1beta1.AddOnMeta{
							DisplayName: "Test AddOn",
							Description: "Test description",
						},
						DefaultConfigs: []addonv1beta1.AddOnConfig{
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
						InstallStrategy: addonv1beta1.InstallStrategy{
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
					Status: addonv1beta1.ClusterManagementAddOnStatus{
						DefaultConfigReferences: []addonv1beta1.DefaultConfigReference{
							{
								ConfigGroupResource: addonv1beta1.ConfigGroupResource{
									Group:    "addon.open-cluster-management.io",
									Resource: "addondeploymentconfigs",
								},
								DesiredConfig: &addonv1beta1.ConfigSpecHash{
									ConfigReferent: addonv1beta1.ConfigReferent{
										Name:      "config1",
										Namespace: "default",
									},
									SpecHash: "hash123",
								},
							},
						},
						InstallProgressions: []addonv1beta1.InstallProgression{
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
												Name:      "config1",
												Namespace: "default",
											},
											SpecHash: "hash123",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dst := &ClusterManagementAddOn{}
			if err := dst.ConvertFrom(tc.src); err != nil {
				t.Fatalf("ConvertFrom() failed: %v", err)
			}

			if diff := cmp.Diff(tc.expected, dst); diff != "" {
				t.Errorf("ConvertFrom() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
