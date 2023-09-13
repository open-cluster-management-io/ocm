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
			expected: "aa3e489402ac2e99c4aef0ddc8cc2fdf1d3b6c34c7b8d040dc3fae5db60478cb",
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
			expected: "00730aa8aa1826c9a3cfd8d6858b45e1e16bcdade5cc57070ea8089c6764285e",
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
