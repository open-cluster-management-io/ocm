package balance

import (
	"context"
	"testing"

	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/runtime"
	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	clusterapiv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	testinghelpers "open-cluster-management.io/placement/pkg/helpers/testing"
)

func TestScoreClusterWithSteady(t *testing.T) {
	cases := []struct {
		name              string
		placement         *clusterapiv1beta1.Placement
		clusters          []*clusterapiv1.ManagedCluster
		existingDecisions []runtime.Object
		expectedScores    map[string]int64
	}{
		{
			name:      "no decisions",
			placement: testinghelpers.NewPlacement("test", "test").Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").Build(),
				testinghelpers.NewManagedCluster("cluster2").Build(),
				testinghelpers.NewManagedCluster("cluster3").Build(),
			},
			existingDecisions: []runtime.Object{},
			expectedScores:    map[string]int64{"cluster1": 100, "cluster2": 100, "cluster3": 100},
		},
		{
			name:      "one decision belongs to current placement",
			placement: testinghelpers.NewPlacement("test", "test").Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").Build(),
				testinghelpers.NewManagedCluster("cluster2").Build(),
				testinghelpers.NewManagedCluster("cluster3").Build(),
			},
			existingDecisions: []runtime.Object{
				testinghelpers.NewPlacementDecision("test", "test1").WithLabel(placementLabel, "test").WithDecisions("cluster1").Build(),
			},
			expectedScores: map[string]int64{"cluster1": 100, "cluster2": 100, "cluster3": 100},
		},
		{
			name:      "one decision not belong to current placement",
			placement: testinghelpers.NewPlacement("test", "test").Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").Build(),
				testinghelpers.NewManagedCluster("cluster2").Build(),
				testinghelpers.NewManagedCluster("cluster3").Build(),
			},
			existingDecisions: []runtime.Object{
				testinghelpers.NewPlacementDecision("test", "test1").WithLabel(placementLabel, "test1").WithDecisions("cluster1").Build(),
			},
			expectedScores: map[string]int64{"cluster1": -100, "cluster2": 100, "cluster3": 100},
		},
		{
			name:      "multiple decisions",
			placement: testinghelpers.NewPlacement("test", "test").Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").Build(),
				testinghelpers.NewManagedCluster("cluster2").Build(),
				testinghelpers.NewManagedCluster("cluster3").Build(),
			},
			existingDecisions: []runtime.Object{
				testinghelpers.NewPlacementDecision("test", "test1").WithLabel(placementLabel, "test1").WithDecisions("cluster1", "cluster2").Build(),
				testinghelpers.NewPlacementDecision("test", "test2").WithLabel(placementLabel, "test2").WithDecisions("cluster1", "cluster3").Build(),
			},
			expectedScores: map[string]int64{"cluster1": -100, "cluster2": 0, "cluster3": 0},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			steady := &Balance{
				handle: testinghelpers.NewFakePluginHandle(t, nil, c.existingDecisions...),
			}

			scoreResult, status := steady.Score(context.TODO(), c.placement, c.clusters)
			scores := scoreResult.Scores
			err := status.AsError()

			if err != nil {
				t.Errorf("Expect no error, but got %v", err)
			}

			if !apiequality.Semantic.DeepEqual(scores, c.expectedScores) {
				t.Errorf("Expect score %v, but got %v", c.expectedScores, scores)
			}
		})
	}
}
