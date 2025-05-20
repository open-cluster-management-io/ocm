package helpers

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	clusterapiv1beta1 "open-cluster-management.io/api/cluster/v1beta1"

	testinghelpers "open-cluster-management.io/ocm/pkg/placement/helpers/testing"
)

func TestMatches(t *testing.T) {
	env, err := NewEnv(nil)
	if err != nil {
		t.Fatalf("failed to create CEL environment: %v", err)
	}

	cases := []struct {
		name            string
		clusterselector clusterapiv1beta1.ClusterSelector
		cluster         *clusterapiv1.ManagedCluster
		expectedMatch   bool
	}{
		{
			name: "match with label",
			clusterselector: clusterapiv1beta1.ClusterSelector{
				LabelSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{
						"cloud": "Amazon",
					},
				},
			},
			cluster:       testinghelpers.NewManagedCluster("test").WithLabel("cloud", "Amazon").Build(),
			expectedMatch: true,
		},
		{
			name: "not match with label",
			clusterselector: clusterapiv1beta1.ClusterSelector{
				LabelSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{
						"cloud": "Amazon",
					},
				},
			},
			cluster:       testinghelpers.NewManagedCluster("test").WithLabel("cloud", "Google").Build(),
			expectedMatch: false,
		},
		{
			name: "match with claim",
			clusterselector: clusterapiv1beta1.ClusterSelector{
				ClaimSelector: clusterapiv1beta1.ClusterClaimSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      "cloud",
							Operator: metav1.LabelSelectorOpIn,
							Values:   []string{"Amazon"},
						},
					},
				},
			},
			cluster:       testinghelpers.NewManagedCluster("test").WithClaim("cloud", "Amazon").Build(),
			expectedMatch: true,
		},
		{
			name: "not match with claim",
			clusterselector: clusterapiv1beta1.ClusterSelector{
				ClaimSelector: clusterapiv1beta1.ClusterClaimSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      "cloud",
							Operator: metav1.LabelSelectorOpIn,
							Values:   []string{"Amazon"},
						},
					},
				},
			},
			cluster:       testinghelpers.NewManagedCluster("test").WithClaim("cloud", "Google").Build(),
			expectedMatch: false,
		},
		{
			name: "match with both label and claim",
			clusterselector: clusterapiv1beta1.ClusterSelector{
				LabelSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{
						"cloud": "Amazon",
					},
				},
				ClaimSelector: clusterapiv1beta1.ClusterClaimSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      "region",
							Operator: metav1.LabelSelectorOpIn,
							Values:   []string{"us-east-1"},
						},
					},
				},
			},
			cluster:       testinghelpers.NewManagedCluster("test").WithLabel("cloud", "Amazon").WithClaim("region", "us-east-1").Build(),
			expectedMatch: true,
		},
		{
			name: "not match with both label and claim",
			clusterselector: clusterapiv1beta1.ClusterSelector{
				LabelSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{
						"cloud": "Amazon",
					},
				},
				ClaimSelector: clusterapiv1beta1.ClusterClaimSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      "region",
							Operator: metav1.LabelSelectorOpIn,
							Values:   []string{"us-east-1"},
						},
					},
				},
			},
			cluster:       testinghelpers.NewManagedCluster("test").WithLabel("region", "us-east-1").WithClaim("cloud", "Amazon").Build(),
			expectedMatch: false,
		},
		{
			name: "match with CEL expression",
			clusterselector: clusterapiv1beta1.ClusterSelector{
				CelSelector: clusterapiv1beta1.ClusterCelSelector{
					CelExpressions: []string{
						`managedCluster.metadata.labels["env"] == "prod"`,
					},
				},
			},
			cluster:       testinghelpers.NewManagedCluster("test").WithLabel("env", "prod").Build(),
			expectedMatch: true,
		},
		{
			name: "not match with CEL expression",
			clusterselector: clusterapiv1beta1.ClusterSelector{
				CelSelector: clusterapiv1beta1.ClusterCelSelector{
					CelExpressions: []string{
						`managedCluster.metadata.labels["env"] == "prod"`,
					},
				},
			},
			cluster:       testinghelpers.NewManagedCluster("test").WithLabel("env", "dev").Build(),
			expectedMatch: false,
		},
		{
			name: "match with CEL expression and label",
			clusterselector: clusterapiv1beta1.ClusterSelector{
				LabelSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{
						"cloud": "Amazon",
					},
				},
				CelSelector: clusterapiv1beta1.ClusterCelSelector{
					CelExpressions: []string{
						`managedCluster.metadata.labels["env"] == "prod"`,
					},
				},
			},
			cluster:       testinghelpers.NewManagedCluster("test").WithLabel("cloud", "Amazon").WithLabel("env", "prod").Build(),
			expectedMatch: true,
		},
		{
			name: "not match with CEL expression and label",
			clusterselector: clusterapiv1beta1.ClusterSelector{
				LabelSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{
						"cloud": "Amazon",
					},
				},
				CelSelector: clusterapiv1beta1.ClusterCelSelector{
					CelExpressions: []string{
						`managedCluster.metadata.labels["env"] == "prod"`,
					},
				},
			},
			cluster:       testinghelpers.NewManagedCluster("test").WithLabel("cloud", "Amazon").WithLabel("env", "dev").Build(),
			expectedMatch: false,
		},
		{
			name: "invalid CEL expression",
			clusterselector: clusterapiv1beta1.ClusterSelector{
				CelSelector: clusterapiv1beta1.ClusterCelSelector{
					CelExpressions: []string{
						`invalid.expression`,
					},
				},
			},
			cluster:       testinghelpers.NewManagedCluster("test").Build(),
			expectedMatch: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterSelector, err := NewClusterSelector(c.clusterselector, env, nil)
			if err != nil {
				t.Errorf("unexpected err: %v", err)
			}
			clusterSelector.Compile()
			result := clusterSelector.Matches(context.TODO(), c.cluster)
			if c.expectedMatch != result {
				t.Errorf("expected match to be %v but get : %v", c.expectedMatch, result)
			}
		})
	}
}

func TestGetClusterClaims(t *testing.T) {
	cases := []struct {
		name     string
		cluster  *clusterapiv1.ManagedCluster
		expected map[string]string
	}{
		{
			name:     "convert cluster claim",
			cluster:  testinghelpers.NewManagedCluster("cluster1").WithClaim("cloud", "Amazon").Build(),
			expected: map[string]string{"cloud": "Amazon"},
		},
		{
			name:     "convert empty cluster claim",
			cluster:  testinghelpers.NewManagedCluster("cluster1").Build(),
			expected: map[string]string{},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			actual := GetClusterClaims(c.cluster)
			if len(actual) != len(c.expected) {
				t.Errorf("expected %v but get %v", c.expected, actual)
			}
		})
	}
}
