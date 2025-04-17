package helpers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
)

func TestCELSelector(t *testing.T) {
	env, err := NewEnv(nil)
	assert.NoError(t, err)
	assert.NotNil(t, env)

	tests := []struct {
		name               string
		expressions        []string
		cluster            *clusterapiv1.ManagedCluster
		expectedMatch      bool
		expectCompileError bool
	}{
		{
			name: "valid expression matches",
			expressions: []string{
				`managedCluster.metadata.labels["env"] == "prod"`,
			},
			cluster: &clusterapiv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"env": "prod"},
				},
			},
			expectedMatch: true,
		},
		{
			name: "valid expression no match",
			expressions: []string{
				`managedCluster.metadata.labels["env"] == "prod"`,
			},
			cluster: &clusterapiv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"env": "dev"},
				},
			},
			expectedMatch: false,
		},
		{
			name: "invalid expression",
			expressions: []string{
				`invalid.expression`,
			},
			cluster:            &clusterapiv1.ManagedCluster{},
			expectCompileError: true,
			expectedMatch:      false,
		},
		{
			name: "multiple expressions all match",
			expressions: []string{
				`managedCluster.metadata.labels["env"] == "prod"`,
				`managedCluster.metadata.labels["region"] == "us-east-1"`,
			},
			cluster: &clusterapiv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"env":    "prod",
						"region": "us-east-1",
					},
				},
			},
			expectedMatch: true,
		},
		{
			name: "multiple expressions one fails",
			expressions: []string{
				`managedCluster.metadata.labels["env"] == "prod"`,
				`managedCluster.metadata.labels["region"] == "us-east-1"`,
			},
			cluster: &clusterapiv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"env":    "prod",
						"region": "us-west-1",
					},
				},
			},
			expectedMatch: false,
		},
		{
			name: "nil cluster",
			expressions: []string{
				`managedCluster.metadata.labels["env"] == "prod"`,
			},
			cluster:            nil,
			expectedMatch:      false,
			expectCompileError: false,
		},
		{
			name:               "empty expressions",
			expressions:        []string{},
			cluster:            &clusterapiv1.ManagedCluster{},
			expectedMatch:      true,
			expectCompileError: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			selector := NewCELSelector(env, test.expressions)
			results := selector.Compile()

			if test.expectCompileError {
				for _, result := range results {
					assert.NotNil(t, result.Error)
				}
				return
			}

			for _, result := range results {
				assert.Nil(t, result.Error)
			}

			match := selector.Validate(test.cluster)
			assert.Equal(t, test.expectedMatch, match)
		})
	}
}
