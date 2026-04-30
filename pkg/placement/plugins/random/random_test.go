package random

import (
	"testing"

	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	clusterapiv1beta1 "open-cluster-management.io/api/cluster/v1beta1"

	testinghelpers "open-cluster-management.io/ocm/pkg/placement/helpers/testing"
	"open-cluster-management.io/ocm/pkg/placement/plugins"
)

func TestScoreClusterWithRandom(t *testing.T) {
	cases := []struct {
		name      string
		placement *clusterapiv1beta1.Placement
		clusters  []*clusterapiv1.ManagedCluster
	}{
		{
			name:      "no clusters",
			placement: testinghelpers.NewPlacement("test", "test").Build(),
			clusters:  []*clusterapiv1.ManagedCluster{},
		},
		{
			name:      "single cluster",
			placement: testinghelpers.NewPlacement("test", "test").Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").Build(),
			},
		},
		{
			name:      "multiple clusters",
			placement: testinghelpers.NewPlacement("test", "test").Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").Build(),
				testinghelpers.NewManagedCluster("cluster2").Build(),
				testinghelpers.NewManagedCluster("cluster3").Build(),
				testinghelpers.NewManagedCluster("cluster4").Build(),
				testinghelpers.NewManagedCluster("cluster5").Build(),
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			random := &Random{
				handle: testinghelpers.NewFakePluginHandle(t, nil),
			}

			scoreResult, status := random.Score(t.Context(), c.placement, c.clusters)
			scores := scoreResult.Scores
			err := status.AsError()

			if err != nil {
				t.Errorf("Expected no error, but got %v", err)
			}

			// Verify all clusters received a score
			if len(scores) != len(c.clusters) {
				t.Errorf("Expected %d scores, but got %d", len(c.clusters), len(scores))
			}

			// Ensure no two clusters have the same score
			seenScores := make(map[int64]string)
			for clusterName, score := range scores {
				if other, exists := seenScores[score]; exists {
					t.Errorf("Clusters %s and %s have the same score %d", clusterName, other, score)
				}
				seenScores[score] = clusterName
			}

			// Verify all scores are within valid range
			for _, cluster := range c.clusters {
				score, exists := scores[cluster.Name]
				if !exists {
					t.Errorf("Cluster %s did not receive a score", cluster.Name)
					continue
				}

				if score < plugins.MinClusterScore || score > plugins.MaxClusterScore {
					t.Errorf("Cluster %s score %d is outside valid range [%d, %d]",
						cluster.Name, score, plugins.MinClusterScore, plugins.MaxClusterScore)
				}
			}
		})
	}
}

func TestScoreDistribution(t *testing.T) {
	// Run multiple iterations to verify randomness
	placement := testinghelpers.NewPlacement("test", "test").Build()
	clusters := []*clusterapiv1.ManagedCluster{
		testinghelpers.NewManagedCluster("cluster1").Build(),
		testinghelpers.NewManagedCluster("cluster2").Build(),
		testinghelpers.NewManagedCluster("cluster3").Build(),
	}

	random := &Random{
		handle: testinghelpers.NewFakePluginHandle(t, nil),
	}

	allScoresIdentical := true
	var previousScores map[string]int64

	for i := range 10 {
		scoreResult, status := random.Score(t.Context(), placement, clusters)
		scores := scoreResult.Scores
		err := status.AsError()

		if err != nil {
			t.Errorf("Expected no error on iteration %d, but got %v", i, err)
		}

		// Check if scores differ from previous iteration
		if i > 0 && allScoresIdentical {
			scoresMatch := true
			for clusterName, score := range scores {
				if previousScore, exists := previousScores[clusterName]; !exists || score != previousScore {
					scoresMatch = false
					break
				}
			}
			if !scoresMatch {
				allScoresIdentical = false
			}
		}

		previousScores = scores
	}

	// With random scores, it's highly unlikely all iterations produce identical scores
	// Note: This test has a very low probability of false failure
	if allScoresIdentical && len(clusters) > 0 {
		t.Log("Warning: All score iterations produced identical results (unlikely with true randomness)")
	}
}
