package templateagent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
)

func TestGetAddOnTemplateSpecHash(t *testing.T) {
	cases := []struct {
		name        string
		at          *addonapiv1alpha1.AddOnTemplate
		expected    string
		expectedErr string
	}{
		{
			name:        "addon template is nil",
			at:          nil,
			expected:    "",
			expectedErr: "addon template is nil",
		},
		{
			name: "addon template spec is nil",
			at: &addonapiv1alpha1.AddOnTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test1",
				},
				Spec: addonapiv1alpha1.AddOnTemplateSpec{},
			},
			expected: "3c2de0761c7df5d6a864280a8aa44d1f4d91eecbbcd2ce5af3efb4b76d91d3cd",
		},
		{
			name: "addon template spec is not nil",
			at: &addonapiv1alpha1.AddOnTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test1",
				},
				Spec: addonapiv1alpha1.AddOnTemplateSpec{
					AddonName: "test1",
				},
			},
			expected: "b59db5673238c791a826f0a8f8710f3d6e49cd4c7a5ee73e186c91412f26fc85",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			hash, err := GetAddOnTemplateSpecHash(c.at)
			if c.expectedErr != "" {
				assert.Equal(t, c.expectedErr, err.Error(), "should be equal")
			} else {
				assert.Nil(t, err, "should be nil")
			}
			assert.Equal(t, c.expected, hash, "should be equal")
		})
	}
}
