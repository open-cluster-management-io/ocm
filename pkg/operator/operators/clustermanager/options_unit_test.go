package clustermanager

import (
	"testing"

	"open-cluster-management.io/ocm/pkg/operator/helpers"
)

func TestImagePullSecretNameFromOptions(t *testing.T) {
	cases := []struct {
		name     string
		options  Options
		expected string
	}{
		{
			name:     "default when unset",
			options:  Options{},
			expected: helpers.ImagePullSecret,
		},
		{
			name:     "custom secret name",
			options:  Options{ImagePullSecretName: "my-registry-secret"},
			expected: "my-registry-secret",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := imagePullSecretNameFromOptions(&c.options); got != c.expected {
				t.Fatalf("expected %q, got %q", c.expected, got)
			}
		})
	}
}
