package helpers

import (
	"errors"
	"strings"
	"testing"

	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	clusterapiv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	testinghelpers "open-cluster-management.io/ocm/pkg/placement/helpers/testing"
)

func TestCelEvaluate(t *testing.T) {
	cases := []struct {
		name            string
		clusterselector clusterapiv1beta1.ClusterSelector
		cluster         *clusterapiv1.ManagedCluster
		expectedMatch   bool
		expectedErr     error
	}{
		{
			name: "match with label",
			clusterselector: clusterapiv1beta1.ClusterSelector{
				CelSelector: clusterapiv1beta1.ClusterCelSelector{
					CelExpressions: []string{`managedCluster.metadata.name.score("aaa","bbb") == 3`},
				},
			},
			cluster:       testinghelpers.NewManagedCluster("test").WithLabel("version", "1.14.3").Build(),
			expectedMatch: true,
		},
		{
			name: "not match with label",
			clusterselector: clusterapiv1beta1.ClusterSelector{
				CelSelector: clusterapiv1beta1.ClusterCelSelector{
					CelExpressions: []string{`managedCluster.metadata.labels["version"].matches('^1\\.(14|15)\\.\\d+$')`},
				},
			},
			cluster:       testinghelpers.NewManagedCluster("test").WithLabel("version", "1.16.3").Build(),
			expectedMatch: false,
		},
		{
			name: "invalid labels expression",
			clusterselector: clusterapiv1beta1.ClusterSelector{
				CelSelector: clusterapiv1beta1.ClusterCelSelector{
					CelExpressions: []string{`managedCluster.metadata.labels["version"].matchess('^1\\.(14|15)\\.\\d+$')`},
				},
			},
			cluster:       testinghelpers.NewManagedCluster("test").WithLabel("version", "1.14.3").Build(),
			expectedMatch: false,
			expectedErr:   errors.New("undeclared reference to 'matchess'"),
		},
		{
			name: "match with claim",
			clusterselector: clusterapiv1beta1.ClusterSelector{
				CelSelector: clusterapiv1beta1.ClusterCelSelector{
					CelExpressions: []string{`managedCluster.status.clusterClaims.exists(c, c.name == "version" && c.value.matches('^1\\.(14|15)\\.\\d+$'))`},
				},
			},
			cluster:       testinghelpers.NewManagedCluster("test").WithClaim("version", "1.14.3").Build(),
			expectedMatch: true,
		},
		{
			name: "not match with claim",
			clusterselector: clusterapiv1beta1.ClusterSelector{
				CelSelector: clusterapiv1beta1.ClusterCelSelector{
					CelExpressions: []string{`managedCluster.status.clusterClaims.exists(c, c.name == "version" && c.value.matches('^1\\.(14|15)\\.\\d+$'))`},
				},
			},
			cluster:       testinghelpers.NewManagedCluster("test").WithClaim("version", "1.16.3").Build(),
			expectedMatch: false,
		},
		{
			name: "invalid claims expression",
			clusterselector: clusterapiv1beta1.ClusterSelector{
				CelSelector: clusterapiv1beta1.ClusterCelSelector{
					CelExpressions: []string{`managedCluster.status.clusterClaims.exists(c, c.name == "version" && c.value.matchessssss('^1\\.(14|15)\\.\\d+$'))`},
				},
			},
			cluster:       testinghelpers.NewManagedCluster("test").WithClaim("version", "1.14.3").Build(),
			expectedMatch: false,
			expectedErr:   errors.New("undeclared reference to 'matchessssss'"),
		},
		{
			name: "match with both label and claim",
			clusterselector: clusterapiv1beta1.ClusterSelector{
				CelSelector: clusterapiv1beta1.ClusterCelSelector{
					CelExpressions: []string{
						`managedCluster.metadata.labels["cloud"] == "Amazon"`,
						`managedCluster.status.clusterClaims.exists(c, c.name == "region" && c.value == "us-east-1")`,
					},
				},
			},
			cluster:       testinghelpers.NewManagedCluster("test").WithLabel("cloud", "Amazon").WithClaim("region", "us-east-1").Build(),
			expectedMatch: true,
		},
		{
			name: "not match with both label and claim",
			clusterselector: clusterapiv1beta1.ClusterSelector{
				CelSelector: clusterapiv1beta1.ClusterCelSelector{
					CelExpressions: []string{
						`managedCluster.metadata.labels["cloud"] == "Amazon"`,
						`managedCluster.status.clusterClaims.exists(c, c.name == "region" && c.value == "us-east-1"`,
					},
				},
			},
			cluster:       testinghelpers.NewManagedCluster("test").WithLabel("region", "us-east-1").WithClaim("cloud", "Amazon").Build(),
			expectedMatch: false,
			expectedErr:   errors.New("no such key: cloud"),
		},
		{
			name: "match with k8s version",
			clusterselector: clusterapiv1beta1.ClusterSelector{
				CelSelector: clusterapiv1beta1.ClusterCelSelector{
					CelExpressions: []string{`managedCluster.status.version.kubernetes == "v1.30.6"`},
				},
			},
			cluster:       testinghelpers.NewManagedCluster("test").Withk8sVersion("v1.30.6").Build(),
			expectedMatch: true,
		},
		{
			name: "not match with k8s version",
			clusterselector: clusterapiv1beta1.ClusterSelector{
				CelSelector: clusterapiv1beta1.ClusterCelSelector{
					CelExpressions: []string{`managedCluster.status.version.kubernetes != "v1.30.6"`},
				},
			},
			cluster:       testinghelpers.NewManagedCluster("test").Withk8sVersion("v1.30.6").Build(),
			expectedMatch: false,
		},
		{
			name: "match with score",
			clusterselector: clusterapiv1beta1.ClusterSelector{
				CelSelector: clusterapiv1beta1.ClusterCelSelector{
					CelExpressions: []string{`managedCluster.score("aaa","bbb") == 3`},
				},
			},
			cluster:       testinghelpers.NewManagedCluster("test").WithLabel("version", "1.14.3").Build(),
			expectedMatch: true,
		},
		{
			name: "not match with score",
			clusterselector: clusterapiv1beta1.ClusterSelector{
				CelSelector: clusterapiv1beta1.ClusterCelSelector{
					CelExpressions: []string{`managedCluster.score("aaa","bbb") > 3`},
				},
			},
			cluster:       testinghelpers.NewManagedCluster("test").WithLabel("version", "1.14.3").Build(),
			expectedMatch: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			evaluator, err := NewEvaluator()
			if err != nil {
				t.Errorf("unexpected err: %v", err)
			}
			result, err := evaluator.Evaluate(c.cluster, c.clusterselector.CelSelector.CelExpressions)
			if c.expectedMatch != result {
				t.Errorf("expected match to be %v but get : %v", c.expectedMatch, result)
			}
			if err != nil && !strings.Contains(err.Error(), c.expectedErr.Error()) {
				t.Errorf("unexpected err %v", err)
			}
		})
	}
}
