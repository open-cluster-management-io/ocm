package addon

import (
	"context"
	"errors"
	"testing"
	"time"

	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/runtime"
	testingclock "k8s.io/utils/clock/testing"
	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	clusterapivbeta1 "open-cluster-management.io/api/cluster/v1beta1"
	testinghelpers "open-cluster-management.io/placement/pkg/helpers/testing"
)

var fakeTime = time.Date(2022, time.January, 01, 0, 0, 0, 0, time.UTC)
var expiredTime = fakeTime.Add(-30 * time.Second)

func TestScoreClusterWithAddOn(t *testing.T) {
	cases := []struct {
		name                string
		placement           *clusterapivbeta1.Placement
		clusters            []*clusterapiv1.ManagedCluster
		existingAddOnScores []runtime.Object
		expectedScores      map[string]int64
		expectedErr         error
	}{
		{
			name:      "no addon scores",
			placement: testinghelpers.NewPlacement("test", "test").WithScoreCoordinateAddOn("test", "score1", 1).Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").Build(),
				testinghelpers.NewManagedCluster("cluster2").Build(),
				testinghelpers.NewManagedCluster("cluster3").Build(),
			},
			existingAddOnScores: []runtime.Object{},
			expectedScores:      map[string]int64{"cluster1": 0, "cluster2": 0, "cluster3": 0},
		},
		{
			name:      "part of addon scores generated",
			placement: testinghelpers.NewPlacement("test", "test").WithScoreCoordinateAddOn("test", "score1", 1).Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").Build(),
				testinghelpers.NewManagedCluster("cluster2").Build(),
				testinghelpers.NewManagedCluster("cluster3").Build(),
			},
			existingAddOnScores: []runtime.Object{
				testinghelpers.NewAddOnPlacementScore("cluster1", "test").WithScore("score1", 30).Build(),
			},
			expectedScores: map[string]int64{"cluster1": 30, "cluster2": 0, "cluster3": 0},
		},
		{
			name:      "part of addon scores expired",
			placement: testinghelpers.NewPlacement("test", "test").WithScoreCoordinateAddOn("test", "score1", 1).Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").Build(),
				testinghelpers.NewManagedCluster("cluster2").Build(),
				testinghelpers.NewManagedCluster("cluster3").Build(),
			},
			existingAddOnScores: []runtime.Object{
				testinghelpers.NewAddOnPlacementScore("cluster1", "test").WithScore("score1", 30).WithValidUntil(expiredTime).Build(),
				testinghelpers.NewAddOnPlacementScore("cluster2", "test").WithScore("score1", 40).Build(),
				testinghelpers.NewAddOnPlacementScore("cluster3", "test").WithScore("score1", 50).Build(),
			},
			expectedScores: map[string]int64{"cluster1": 0, "cluster2": 40, "cluster3": 50},
			expectedErr:    errors.New("AddOnPlacementScores cluster1/test expired"),
		},
		{
			name:      "all the addon scores generated",
			placement: testinghelpers.NewPlacement("test", "test").WithScoreCoordinateAddOn("test", "score1", 1).Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").Build(),
				testinghelpers.NewManagedCluster("cluster2").Build(),
				testinghelpers.NewManagedCluster("cluster3").Build(),
			},
			existingAddOnScores: []runtime.Object{
				testinghelpers.NewAddOnPlacementScore("cluster1", "test").WithScore("score1", 30).Build(),
				testinghelpers.NewAddOnPlacementScore("cluster2", "test").WithScore("score1", 40).Build(),
				testinghelpers.NewAddOnPlacementScore("cluster3", "test").WithScore("score1", 50).Build(),
			},
			expectedScores: map[string]int64{"cluster1": 30, "cluster2": 40, "cluster3": 50},
		},
	}

	AddOnClock = testingclock.NewFakeClock(fakeTime)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			addon := &AddOn{
				handle:          testinghelpers.NewFakePluginHandle(t, nil, c.existingAddOnScores...),
				prioritizerName: "AddOn/test/score1",
				resourceName:    "test",
				scoreName:       "score1",
			}

			scoreResult, status := addon.Score(context.TODO(), c.placement, c.clusters)
			scores := scoreResult.Scores
			err := status.AsError()
			if err != nil && err.Error() != c.expectedErr.Error() {
				t.Errorf("expect err %v but get %v", c.expectedErr, err)
			}

			if !apiequality.Semantic.DeepEqual(scores, c.expectedScores) {
				t.Errorf("Expect score %v, but got %v", c.expectedScores, scores)
			}
		})
	}
}
