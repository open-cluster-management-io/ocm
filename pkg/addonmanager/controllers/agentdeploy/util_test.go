package agentdeploy

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	workapiv1 "open-cluster-management.io/api/work/v1"
)

func TestConfigsToAnnotations(t *testing.T) {
	cases := []struct {
		name              string
		configReference   []addonapiv1alpha1.ConfigReference
		expectAnnotations map[string]string
	}{
		{
			name: "generate annotaions",
			configReference: []addonapiv1alpha1.ConfigReference{
				{
					ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					DesiredConfig: &addonapiv1alpha1.ConfigSpecHash{
						ConfigReferent: addonapiv1alpha1.ConfigReferent{
							Name:      "test",
							Namespace: "open-cluster-management",
						},
						SpecHash: "hash1",
					},
				},
				{
					ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
						Resource: "addonhubconfigs",
					},
					DesiredConfig: &addonapiv1alpha1.ConfigSpecHash{
						ConfigReferent: addonapiv1alpha1.ConfigReferent{
							Name: "test",
						},
						SpecHash: "hash2",
					},
				},
			},
			expectAnnotations: map[string]string{
				workapiv1.ManifestConfigSpecHashAnnotationKey: `{"addondeploymentconfigs.addon.open-cluster-management.io/open-cluster-management/test":"hash1","addonhubconfigs//test":"hash2"}`},
		},
		{
			name:              "generate annotaions without configReference",
			configReference:   []addonapiv1alpha1.ConfigReference{},
			expectAnnotations: nil,
		},
		{
			name: "generate annotaions without DesiredConfig",
			configReference: []addonapiv1alpha1.ConfigReference{
				{
					ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
				},
			},
			expectAnnotations: map[string]string{
				workapiv1.ManifestConfigSpecHashAnnotationKey: `{}`},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			annotations, err := configsToAnnotations(c.configReference)
			assert.NoError(t, err)
			if !reflect.DeepEqual(annotations, c.expectAnnotations) {
				t.Fatalf("Expected annotations to be equal but got %v (expected) and %v (actual)", c.expectAnnotations, annotations)
			}
		})
	}
}
