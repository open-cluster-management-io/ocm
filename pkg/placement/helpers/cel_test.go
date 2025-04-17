package helpers

import (
	"context"
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
		expectedCost       int64
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
			expectedCost:  5,
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
			expectedCost:  5,
		},
		{
			name: "invalid expression",
			expressions: []string{
				`invalid.expression`,
			},
			cluster:            &clusterapiv1.ManagedCluster{},
			expectCompileError: true,
			expectedMatch:      false,
			expectedCost:       5,
		},
		{
			name: "multiple expressions all match",
			expressions: []string{
				`managedCluster.metadata.labels["env"] == "prod"`,
				`semver(managedCluster.metadata.labels["version"]).isLessThan(semver("1.31.0"))`,
			},
			cluster: &clusterapiv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"env":     "prod",
						"version": "1.30.0",
					},
				},
			},
			expectedMatch: true,
			expectedCost:  12,
		},
		{
			name: "multiple expressions one fails",
			expressions: []string{
				`managedCluster.metadata.labels["env"] == "prod"`,
				`semver(managedCluster.metadata.labels["version"]).isGreaterThan(semver("1.31.0"))`,
			},
			cluster: &clusterapiv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"env":     "prod",
						"version": "1.30.0",
					},
				},
			},
			expectedMatch: false,
			expectedCost:  12,
		},
		{
			name: "multiple expressions running out of cost budget",
			expressions: []string{
				`managedCluster.metadata.labels["env"] == "prod"`,
				`semver(managedCluster.metadata.labels["version"]).isLessThan(semver("1.31.0"))`,
				`semver(managedCluster.metadata.labels["version"]).isLessThan(semver("1.31.0"))`,
			},
			cluster: &clusterapiv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"env":     "prod",
						"version": "1.30.0",
					},
				},
			},
			expectedMatch: false,
			expectedCost:  16,
		},
		{
			name: "nil cluster",
			expressions: []string{
				`managedCluster.metadata.labels["env"] == "prod"`,
			},
			cluster:            nil,
			expectedMatch:      false,
			expectCompileError: false,
			expectedCost:       3,
		},
		{
			name:               "empty expressions",
			expressions:        []string{},
			cluster:            &clusterapiv1.ManagedCluster{},
			expectedMatch:      true,
			expectCompileError: false,
			expectedCost:       0,
		},
	}

	globalCostBudget = 15
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			selector := NewCELSelector(env, test.expressions, nil)
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

			match, cost := selector.Validate(context.TODO(), test.cluster)
			assert.Equal(t, test.expectedMatch, match)
			assert.Equal(t, test.expectedCost, cost)
		})
	}
}
