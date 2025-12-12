// Copyright Contributors to the Open Cluster Management project

package conversion

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1beta1 "open-cluster-management.io/api/addon/v1beta1"
)

func TestConvertRegistrationsToV1Beta1(t *testing.T) {
	cases := []struct {
		name     string
		input    []addonv1alpha1.RegistrationConfig
		expected []addonv1beta1.RegistrationConfig
	}{
		{
			name:     "nil registrations - should preserve nil",
			input:    nil,
			expected: nil,
		},
		{
			name:     "empty registrations - should return nil",
			input:    []addonv1alpha1.RegistrationConfig{},
			expected: nil,
		},
		{
			name: "KubeClient registration",
			input: []addonv1alpha1.RegistrationConfig{
				{
					SignerName: "kubernetes.io/kube-apiserver-client",
					Subject: addonv1alpha1.Subject{
						User:              "system:addon:test",
						Groups:            []string{"system:authenticated"},
						OrganizationUnits: []string{"acme-org"},
					},
				},
			},
			expected: []addonv1beta1.RegistrationConfig{
				{
					Type: addonv1beta1.KubeClient,
					KubeClient: &addonv1beta1.KubeClientConfig{
						Subject: addonv1beta1.KubeClientSubject{
							BaseSubject: addonv1beta1.BaseSubject{
								User:   "system:addon:test",
								Groups: []string{"system:authenticated"},
							},
						},
					},
				},
			},
		},
		{
			name: "CSR registration with custom signer",
			input: []addonv1alpha1.RegistrationConfig{
				{
					SignerName: "example.com/custom-signer",
					Subject: addonv1alpha1.Subject{
						User:              "system:addon:custom",
						Groups:            []string{"system:authenticated"},
						OrganizationUnits: []string{"custom-org"},
					},
				},
			},
			expected: []addonv1beta1.RegistrationConfig{
				{
					Type: addonv1beta1.CSR,
					CSR: &addonv1beta1.CSRConfig{
						SignerName: "example.com/custom-signer",
						Subject: addonv1beta1.Subject{
							BaseSubject: addonv1beta1.BaseSubject{
								User:   "system:addon:custom",
								Groups: []string{"system:authenticated"},
							},
							OrganizationUnits: []string{"custom-org"},
						},
					},
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := ConvertRegistrationsToV1Beta1(c.input)
			if diff := cmp.Diff(c.expected, result); diff != "" {
				t.Errorf("conversion mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestConvertRegistrationsFromV1Beta1(t *testing.T) {
	cases := []struct {
		name     string
		input    []addonv1beta1.RegistrationConfig
		expected []addonv1alpha1.RegistrationConfig
	}{
		{
			name:     "nil registrations - should preserve nil",
			input:    nil,
			expected: nil,
		},
		{
			name:     "empty registrations - should return nil",
			input:    []addonv1beta1.RegistrationConfig{},
			expected: nil,
		},
		{
			name: "KubeClient registration",
			input: []addonv1beta1.RegistrationConfig{
				{
					Type: addonv1beta1.KubeClient,
					KubeClient: &addonv1beta1.KubeClientConfig{
						Subject: addonv1beta1.KubeClientSubject{
							BaseSubject: addonv1beta1.BaseSubject{
								User:   "system:addon:test",
								Groups: []string{"system:authenticated"},
							},
						},
					},
				},
			},
			expected: []addonv1alpha1.RegistrationConfig{
				{
					SignerName: "kubernetes.io/kube-apiserver-client",
					Subject: addonv1alpha1.Subject{
						User:   "system:addon:test",
						Groups: []string{"system:authenticated"},
					},
				},
			},
		},
		{
			name: "CSR registration with custom signer",
			input: []addonv1beta1.RegistrationConfig{
				{
					Type: addonv1beta1.CSR,
					CSR: &addonv1beta1.CSRConfig{
						SignerName: "example.com/custom-signer",
						Subject: addonv1beta1.Subject{
							BaseSubject: addonv1beta1.BaseSubject{
								User:   "system:addon:custom",
								Groups: []string{"system:authenticated"},
							},
							OrganizationUnits: []string{"custom-org"},
						},
					},
				},
			},
			expected: []addonv1alpha1.RegistrationConfig{
				{
					SignerName: "example.com/custom-signer",
					Subject: addonv1alpha1.Subject{
						User:              "system:addon:custom",
						Groups:            []string{"system:authenticated"},
						OrganizationUnits: []string{"custom-org"},
					},
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := ConvertRegistrationsFromV1Beta1(c.input)
			if diff := cmp.Diff(c.expected, result); diff != "" {
				t.Errorf("conversion mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestConvertConfigReferencesToV1Beta1(t *testing.T) {
	cases := []struct {
		name     string
		input    []addonv1alpha1.ConfigReference
		expected []addonv1beta1.ConfigReference
	}{
		{
			name:     "nil config references - should preserve nil",
			input:    nil,
			expected: nil,
		},
		{
			name:     "empty config references - should return nil (not empty slice)",
			input:    []addonv1alpha1.ConfigReference{},
			expected: nil,
		},
		{
			name: "with config references",
			input: []addonv1alpha1.ConfigReference{
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
			expected: []addonv1beta1.ConfigReference{
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
		{
			name: "with all config fields",
			input: []addonv1alpha1.ConfigReference{
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
						SpecHash: "hash-desired",
					},
					LastAppliedConfig: &addonv1alpha1.ConfigSpecHash{
						ConfigReferent: addonv1alpha1.ConfigReferent{
							Name:      "config1",
							Namespace: "default",
						},
						SpecHash: "hash-applied",
					},
					LastObservedGeneration: 3,
				},
			},
			expected: []addonv1beta1.ConfigReference{
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
						SpecHash: "hash-desired",
					},
					LastAppliedConfig: &addonv1beta1.ConfigSpecHash{
						ConfigReferent: addonv1beta1.ConfigReferent{
							Name:      "config1",
							Namespace: "default",
						},
						SpecHash: "hash-applied",
					},
					LastObservedGeneration: 3,
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := ConvertConfigReferencesToV1Beta1(c.input)
			if diff := cmp.Diff(c.expected, result); diff != "" {
				t.Errorf("conversion mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestConvertConfigReferencesFromV1Beta1(t *testing.T) {
	cases := []struct {
		name     string
		input    []addonv1beta1.ConfigReference
		expected []addonv1alpha1.ConfigReference
	}{
		{
			name:     "nil config references - should preserve nil",
			input:    nil,
			expected: nil,
		},
		{
			name:     "empty config references - should return nil (not empty slice)",
			input:    []addonv1beta1.ConfigReference{},
			expected: nil,
		},
		{
			name: "with config references",
			input: []addonv1beta1.ConfigReference{
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
			expected: []addonv1alpha1.ConfigReference{
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
		},
		{
			name: "with desired and last applied config",
			input: []addonv1beta1.ConfigReference{
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
						SpecHash: "hash-desired",
					},
					LastAppliedConfig: &addonv1beta1.ConfigSpecHash{
						ConfigReferent: addonv1beta1.ConfigReferent{
							Name:      "config1",
							Namespace: "default",
						},
						SpecHash: "hash-applied",
					},
					LastObservedGeneration: 5,
				},
			},
			expected: []addonv1alpha1.ConfigReference{
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
						SpecHash: "hash-desired",
					},
					LastAppliedConfig: &addonv1alpha1.ConfigSpecHash{
						ConfigReferent: addonv1alpha1.ConfigReferent{
							Name:      "config1",
							Namespace: "default",
						},
						SpecHash: "hash-applied",
					},
					LastObservedGeneration: 5,
				},
			},
		},
		{
			name: "with nil desired config - should not extract ConfigReferent",
			input: []addonv1beta1.ConfigReference{
				{
					ConfigGroupResource: addonv1beta1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					DesiredConfig: nil,
				},
			},
			expected: []addonv1alpha1.ConfigReference{
				{
					ConfigGroupResource: addonv1alpha1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					ConfigReferent: addonv1alpha1.ConfigReferent{
						Name:      "",
						Namespace: "",
					},
					DesiredConfig: nil,
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := ConvertConfigReferencesFromV1Beta1(c.input)
			if diff := cmp.Diff(c.expected, result); diff != "" {
				t.Errorf("conversion mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestConvertRelatedObjectsToV1Beta1(t *testing.T) {
	cases := []struct {
		name     string
		input    []addonv1alpha1.ObjectReference
		expected []addonv1beta1.ObjectReference
	}{
		{
			name:     "nil related objects - should preserve nil",
			input:    nil,
			expected: nil,
		},
		{
			name:     "empty related objects",
			input:    []addonv1alpha1.ObjectReference{},
			expected: []addonv1beta1.ObjectReference{},
		},
		{
			name: "with related objects",
			input: []addonv1alpha1.ObjectReference{
				{
					Group:     "apps",
					Resource:  "deployments",
					Namespace: "default",
					Name:      "my-deployment",
				},
				{
					Group:    "",
					Resource: "configmaps",
					Name:     "my-config",
				},
			},
			expected: []addonv1beta1.ObjectReference{
				{
					Group:     "apps",
					Resource:  "deployments",
					Namespace: "default",
					Name:      "my-deployment",
				},
				{
					Group:    "",
					Resource: "configmaps",
					Name:     "my-config",
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := ConvertRelatedObjectsToV1Beta1(c.input)
			if diff := cmp.Diff(c.expected, result); diff != "" {
				t.Errorf("conversion mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestConvertRelatedObjectsFromV1Beta1(t *testing.T) {
	cases := []struct {
		name     string
		input    []addonv1beta1.ObjectReference
		expected []addonv1alpha1.ObjectReference
	}{
		{
			name:     "nil related objects - should preserve nil",
			input:    nil,
			expected: nil,
		},
		{
			name:     "empty related objects",
			input:    []addonv1beta1.ObjectReference{},
			expected: []addonv1alpha1.ObjectReference{},
		},
		{
			name: "with related objects",
			input: []addonv1beta1.ObjectReference{
				{
					Group:     "apps",
					Resource:  "deployments",
					Namespace: "default",
					Name:      "my-deployment",
				},
				{
					Group:    "",
					Resource: "services",
					Name:     "my-service",
				},
			},
			expected: []addonv1alpha1.ObjectReference{
				{
					Group:     "apps",
					Resource:  "deployments",
					Namespace: "default",
					Name:      "my-deployment",
				},
				{
					Group:    "",
					Resource: "services",
					Name:     "my-service",
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := ConvertRelatedObjectsFromV1Beta1(c.input)
			if diff := cmp.Diff(c.expected, result); diff != "" {
				t.Errorf("conversion mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestConvertSupportedConfigsToV1Beta1(t *testing.T) {
	cases := []struct {
		name     string
		input    []addonv1alpha1.ConfigGroupResource
		expected []addonv1beta1.ConfigGroupResource
	}{
		{
			name:     "nil supported configs - should preserve nil",
			input:    nil,
			expected: nil,
		},
		{
			name:     "empty supported configs",
			input:    []addonv1alpha1.ConfigGroupResource{},
			expected: []addonv1beta1.ConfigGroupResource{},
		},
		{
			name: "with supported configs",
			input: []addonv1alpha1.ConfigGroupResource{
				{
					Group:    "addon.open-cluster-management.io",
					Resource: "addondeploymentconfigs",
				},
				{
					Group:    "config.open-cluster-management.io",
					Resource: "customconfigs",
				},
			},
			expected: []addonv1beta1.ConfigGroupResource{
				{
					Group:    "addon.open-cluster-management.io",
					Resource: "addondeploymentconfigs",
				},
				{
					Group:    "config.open-cluster-management.io",
					Resource: "customconfigs",
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := ConvertSupportedConfigsToV1Beta1(c.input)
			if diff := cmp.Diff(c.expected, result); diff != "" {
				t.Errorf("conversion mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestConvertSupportedConfigsFromV1Beta1(t *testing.T) {
	cases := []struct {
		name     string
		input    []addonv1beta1.ConfigGroupResource
		expected []addonv1alpha1.ConfigGroupResource
	}{
		{
			name:     "nil supported configs - should preserve nil",
			input:    nil,
			expected: nil,
		},
		{
			name:     "empty supported configs",
			input:    []addonv1beta1.ConfigGroupResource{},
			expected: []addonv1alpha1.ConfigGroupResource{},
		},
		{
			name: "with supported configs",
			input: []addonv1beta1.ConfigGroupResource{
				{
					Group:    "addon.open-cluster-management.io",
					Resource: "addondeploymentconfigs",
				},
				{
					Group:    "config.open-cluster-management.io",
					Resource: "customconfigs",
				},
			},
			expected: []addonv1alpha1.ConfigGroupResource{
				{
					Group:    "addon.open-cluster-management.io",
					Resource: "addondeploymentconfigs",
				},
				{
					Group:    "config.open-cluster-management.io",
					Resource: "customconfigs",
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := ConvertSupportedConfigsFromV1Beta1(c.input)
			if diff := cmp.Diff(c.expected, result); diff != "" {
				t.Errorf("conversion mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
