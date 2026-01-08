// Copyright Contributors to the Open Cluster Management project

package v1beta1

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1beta1 "open-cluster-management.io/api/addon/v1beta1"

	internalv1alpha1 "open-cluster-management.io/ocm/pkg/addon/webhook/v1alpha1"
)

func TestManagedClusterAddOnConvertTo(t *testing.T) {
	cases := []struct {
		name     string
		src      *ManagedClusterAddOn
		expected *internalv1alpha1.ManagedClusterAddOn
	}{
		{
			name: "complete conversion with all fields",
			src: &ManagedClusterAddOn{
				ManagedClusterAddOn: addonv1beta1.ManagedClusterAddOn{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-addon",
						Namespace: "cluster1",
						Annotations: map[string]string{
							"addon.open-cluster-management.io/v1alpha1-install-namespace": "test-namespace",
						},
					},
					Spec: addonv1beta1.ManagedClusterAddOnSpec{
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
					},
					Status: addonv1beta1.ManagedClusterAddOnStatus{
						Namespace: "open-cluster-management-agent-addon",
						Registrations: []addonv1beta1.RegistrationConfig{
							{
								Type: addonv1beta1.KubeClient,
								KubeClient: &addonv1beta1.KubeClientConfig{
									Subject: addonv1beta1.KubeClientSubject{
										BaseSubject: addonv1beta1.BaseSubject{
											User: "system:addon:test",
										},
									},
								},
							},
						},
						ConfigReferences: []addonv1beta1.ConfigReference{
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
						SupportedConfigs: []addonv1beta1.ConfigGroupResource{
							{
								Group:    "addon.open-cluster-management.io",
								Resource: "addondeploymentconfigs",
							},
						},
						RelatedObjects: []addonv1beta1.ObjectReference{
							{
								Group:    "apps",
								Resource: "deployments",
								Name:     "test-deployment",
							},
						},
						AddOnMeta: addonv1beta1.AddOnMeta{
							DisplayName: "Test AddOn",
							Description: "Test description",
						},
						HealthCheck: addonv1beta1.HealthCheck{
							Mode: addonv1beta1.HealthCheckModeCustomized,
						},
					},
				},
			},
			expected: &internalv1alpha1.ManagedClusterAddOn{
				ManagedClusterAddOn: addonv1alpha1.ManagedClusterAddOn{
					TypeMeta: metav1.TypeMeta{
						Kind:       "ManagedClusterAddOn",
						APIVersion: "addon.open-cluster-management.io/v1alpha1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-addon",
						Namespace: "cluster1",
						// The internal annotation should be removed after conversion to v1alpha1
						Annotations: map[string]string{},
					},
					Spec: addonv1alpha1.ManagedClusterAddOnSpec{
						InstallNamespace: "test-namespace",
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
					},
					Status: addonv1alpha1.ManagedClusterAddOnStatus{
						Namespace: "open-cluster-management-agent-addon",
						Registrations: []addonv1alpha1.RegistrationConfig{
							{
								SignerName: "kubernetes.io/kube-apiserver-client",
								Subject: addonv1alpha1.Subject{
									User: "system:addon:test",
								},
							},
						},
						ConfigReferences: []addonv1alpha1.ConfigReference{
							{
								ConfigGroupResource: addonv1alpha1.ConfigGroupResource{
									Group:    "addon.open-cluster-management.io",
									Resource: "addondeploymentconfigs",
								},
								ConfigReferent: addonv1alpha1.ConfigReferent{
									Name:      "config1",
									Namespace: "default",
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
						SupportedConfigs: []addonv1alpha1.ConfigGroupResource{
							{
								Group:    "addon.open-cluster-management.io",
								Resource: "addondeploymentconfigs",
							},
						},
						RelatedObjects: []addonv1alpha1.ObjectReference{
							{
								Group:    "apps",
								Resource: "deployments",
								Name:     "test-deployment",
							},
						},
						AddOnMeta: addonv1alpha1.AddOnMeta{
							DisplayName: "Test AddOn",
							Description: "Test description",
						},
						HealthCheck: addonv1alpha1.HealthCheck{
							Mode: addonv1alpha1.HealthCheckModeCustomized,
						},
					},
				},
			},
		},
		{
			name: "conversion removes internal annotation but preserves user annotations",
			src: &ManagedClusterAddOn{
				ManagedClusterAddOn: addonv1beta1.ManagedClusterAddOn{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-addon",
						Namespace: "cluster1",
						Annotations: map[string]string{
							"addon.open-cluster-management.io/v1alpha1-install-namespace": "test-namespace",
							"abc.def":         "hahaha",
							"user.annotation": "should-be-preserved",
						},
					},
					Spec: addonv1beta1.ManagedClusterAddOnSpec{},
				},
			},
			expected: &internalv1alpha1.ManagedClusterAddOn{
				ManagedClusterAddOn: addonv1alpha1.ManagedClusterAddOn{
					TypeMeta: metav1.TypeMeta{
						Kind:       "ManagedClusterAddOn",
						APIVersion: "addon.open-cluster-management.io/v1alpha1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-addon",
						Namespace: "cluster1",
						// Internal annotation removed, user annotations preserved
						Annotations: map[string]string{
							"abc.def":         "hahaha",
							"user.annotation": "should-be-preserved",
						},
					},
					Spec: addonv1alpha1.ManagedClusterAddOnSpec{
						InstallNamespace: "test-namespace",
					},
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dst := &internalv1alpha1.ManagedClusterAddOn{}
			if err := tc.src.ConvertTo(dst); err != nil {
				t.Fatalf("ConvertTo() failed: %v", err)
			}

			if diff := cmp.Diff(tc.expected, dst); diff != "" {
				t.Errorf("ConvertTo() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestManagedClusterAddOnConvertFrom(t *testing.T) {
	cases := []struct {
		name     string
		src      *internalv1alpha1.ManagedClusterAddOn
		expected *ManagedClusterAddOn
	}{
		{
			name: "complete conversion with all fields",
			src: &internalv1alpha1.ManagedClusterAddOn{
				ManagedClusterAddOn: addonv1alpha1.ManagedClusterAddOn{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-addon",
						Namespace: "cluster1",
					},
					Spec: addonv1alpha1.ManagedClusterAddOnSpec{
						InstallNamespace: "test-namespace",
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
					},
					Status: addonv1alpha1.ManagedClusterAddOnStatus{
						Namespace: "open-cluster-management-agent-addon",
						Registrations: []addonv1alpha1.RegistrationConfig{
							{
								SignerName: "kubernetes.io/kube-apiserver-client",
								Subject: addonv1alpha1.Subject{
									User: "system:addon:test",
								},
							},
						},
						ConfigReferences: []addonv1alpha1.ConfigReference{
							{
								ConfigGroupResource: addonv1alpha1.ConfigGroupResource{
									Group:    "addon.open-cluster-management.io",
									Resource: "addondeploymentconfigs",
								},
								ConfigReferent: addonv1alpha1.ConfigReferent{
									Name:      "config1",
									Namespace: "default",
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
						SupportedConfigs: []addonv1alpha1.ConfigGroupResource{
							{
								Group:    "addon.open-cluster-management.io",
								Resource: "addondeploymentconfigs",
							},
						},
						RelatedObjects: []addonv1alpha1.ObjectReference{
							{
								Group:    "apps",
								Resource: "deployments",
								Name:     "test-deployment",
							},
						},
						AddOnMeta: addonv1alpha1.AddOnMeta{
							DisplayName: "Test AddOn",
							Description: "Test description",
						},
						HealthCheck: addonv1alpha1.HealthCheck{
							Mode: addonv1alpha1.HealthCheckModeCustomized,
						},
					},
				},
			},
			expected: &ManagedClusterAddOn{
				ManagedClusterAddOn: addonv1beta1.ManagedClusterAddOn{
					TypeMeta: metav1.TypeMeta{
						Kind:       "ManagedClusterAddOn",
						APIVersion: "addon.open-cluster-management.io/v1beta1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-addon",
						Namespace: "cluster1",
						Annotations: map[string]string{
							"addon.open-cluster-management.io/v1alpha1-install-namespace": "test-namespace",
						},
					},
					Spec: addonv1beta1.ManagedClusterAddOnSpec{
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
					},
					Status: addonv1beta1.ManagedClusterAddOnStatus{
						Namespace: "open-cluster-management-agent-addon",
						Registrations: []addonv1beta1.RegistrationConfig{
							{
								Type: addonv1beta1.KubeClient,
								KubeClient: &addonv1beta1.KubeClientConfig{
									Subject: addonv1beta1.KubeClientSubject{
										BaseSubject: addonv1beta1.BaseSubject{
											User: "system:addon:test",
										},
									},
								},
							},
						},
						ConfigReferences: []addonv1beta1.ConfigReference{
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
						SupportedConfigs: []addonv1beta1.ConfigGroupResource{
							{
								Group:    "addon.open-cluster-management.io",
								Resource: "addondeploymentconfigs",
							},
						},
						RelatedObjects: []addonv1beta1.ObjectReference{
							{
								Group:    "apps",
								Resource: "deployments",
								Name:     "test-deployment",
							},
						},
						AddOnMeta: addonv1beta1.AddOnMeta{
							DisplayName: "Test AddOn",
							Description: "Test description",
						},
						HealthCheck: addonv1beta1.HealthCheck{
							Mode: addonv1beta1.HealthCheckModeCustomized,
						},
					},
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dst := &ManagedClusterAddOn{}
			if err := dst.ConvertFrom(tc.src); err != nil {
				t.Fatalf("ConvertFrom() failed: %v", err)
			}

			if diff := cmp.Diff(tc.expected, dst); diff != "" {
				t.Errorf("ConvertFrom() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
