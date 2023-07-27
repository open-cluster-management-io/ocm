package helpers

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	clusterapiv1beta1 "open-cluster-management.io/api/cluster/v1beta1"

	testinghelpers "open-cluster-management.io/ocm/pkg/placement/helpers/testing"
)

func TestMatches(t *testing.T) {
	cases := []struct {
		name            string
		clusterselector clusterapiv1beta1.ClusterSelector
		clusterlabels   map[string]string
		clusterclaims   map[string]string
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
			clusterlabels: map[string]string{"cloud": "Amazon"},
			clusterclaims: map[string]string{},
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
			clusterlabels: map[string]string{"cloud": "Google"},
			clusterclaims: map[string]string{},
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
			clusterlabels: map[string]string{},
			clusterclaims: map[string]string{"cloud": "Amazon"},
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
			clusterlabels: map[string]string{},
			clusterclaims: map[string]string{"cloud": "Google"},
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
			clusterlabels: map[string]string{"cloud": "Amazon"},
			clusterclaims: map[string]string{"region": "us-east-1"},
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
			clusterlabels: map[string]string{"region": "us-east-1"},
			clusterclaims: map[string]string{"cloud": "Amazon"},
			expectedMatch: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterSelector, err := NewClusterSelector(c.clusterselector)
			if err != nil {
				t.Errorf("unexpected err: %v", err)
			}
			result := clusterSelector.Matches(c.clusterlabels, c.clusterclaims)
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
