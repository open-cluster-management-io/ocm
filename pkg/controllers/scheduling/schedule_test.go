package scheduling

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"

	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	clusterapiv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
	testinghelpers "open-cluster-management.io/placement/pkg/helpers/testing"
)

func TestSchedule(t *testing.T) {
	clusterSetName := "clusterSets"
	placementNamespace := "ns1"
	placementName := "placement1"

	cases := []struct {
		name                 string
		placement            *clusterapiv1alpha1.Placement
		initObjs             []runtime.Object
		clusters             []*clusterapiv1.ManagedCluster
		decisions            []runtime.Object
		expectedFilterResult []FilterResult
		expectedScoreResult  []PriorizeResult
		expectedDecisions    []clusterapiv1alpha1.ClusterDecision
		expectedUnScheduled  int
	}{
		{
			name:      "new placement satisfied",
			placement: testinghelpers.NewPlacement(placementNamespace, placementName).Build(),
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet(clusterSetName),
				testinghelpers.NewClusterSetBinding(placementNamespace, clusterSetName),
			},
			decisions: []runtime.Object{},
			expectedDecisions: []clusterapiv1alpha1.ClusterDecision{
				{ClusterName: "cluster1"},
			},
			expectedFilterResult: []FilterResult{
				{
					Name:             "predicate",
					FilteredClusters: []string{"cluster1"},
				},
			},
			expectedScoreResult: []PriorizeResult{
				{
					Name:   "balance",
					Scores: PrioritizeSore{"cluster1": 100},
				},
				{
					Name:   "steady",
					Scores: PrioritizeSore{"cluster1": 0},
				},
			},
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithLabel(clusterSetLabel, clusterSetName).Build(),
			},
			expectedUnScheduled: 0,
		},
		{
			name:      "new placement unsatisfied",
			placement: testinghelpers.NewPlacement(placementNamespace, placementName).WithNOC(3).Build(),
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet(clusterSetName),
				testinghelpers.NewClusterSetBinding(placementNamespace, clusterSetName),
			},
			decisions: []runtime.Object{},
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithLabel(clusterSetLabel, clusterSetName).Build(),
			},
			expectedDecisions: []clusterapiv1alpha1.ClusterDecision{
				{ClusterName: "cluster1"},
			},
			expectedFilterResult: []FilterResult{
				{
					Name:             "predicate",
					FilteredClusters: []string{"cluster1"},
				},
			},
			expectedScoreResult: []PriorizeResult{
				{
					Name:   "balance",
					Scores: PrioritizeSore{"cluster1": 100},
				},
				{
					Name:   "steady",
					Scores: PrioritizeSore{"cluster1": 0},
				},
			},
			expectedUnScheduled: 2,
		},
		{
			name:      "placement with all decisions scheduled",
			placement: testinghelpers.NewPlacement(placementNamespace, placementName).WithNOC(2).Build(),
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet(clusterSetName),
				testinghelpers.NewClusterSetBinding(placementNamespace, clusterSetName),
				testinghelpers.NewPlacementDecision(placementNamespace, placementDecisionName(placementName, 1)).
					WithLabel(placementLabel, placementName).
					WithDecisions("cluster1", "cluster2").Build(),
			},
			decisions: []runtime.Object{
				testinghelpers.NewPlacementDecision(placementNamespace, placementDecisionName(placementName, 1)).
					WithLabel(placementLabel, placementName).
					WithDecisions("cluster1", "cluster2").Build(),
			},
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithLabel(clusterSetLabel, clusterSetName).Build(),
				testinghelpers.NewManagedCluster("cluster2").WithLabel(clusterSetLabel, clusterSetName).Build(),
				testinghelpers.NewManagedCluster("cluster3").WithLabel(clusterSetLabel, clusterSetName).Build(),
			},
			expectedDecisions: []clusterapiv1alpha1.ClusterDecision{
				{ClusterName: "cluster1"},
				{ClusterName: "cluster2"},
			},
			expectedFilterResult: []FilterResult{
				{
					Name:             "predicate",
					FilteredClusters: []string{"cluster1", "cluster2", "cluster3"},
				},
			},
			expectedScoreResult: []PriorizeResult{
				{
					Name:   "balance",
					Scores: PrioritizeSore{"cluster1": 100, "cluster2": 100, "cluster3": 100},
				},
				{
					Name:   "steady",
					Scores: PrioritizeSore{"cluster1": 100, "cluster2": 100, "cluster3": 0},
				},
			},
			expectedUnScheduled: 0,
		},
		{
			name:      "placement with part of decisions scheduled",
			placement: testinghelpers.NewPlacement(placementNamespace, placementName).WithNOC(4).Build(),
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet(clusterSetName),
				testinghelpers.NewClusterSetBinding(placementNamespace, clusterSetName),
				testinghelpers.NewPlacementDecision(placementNamespace, placementDecisionName(placementName, 1)).
					WithLabel(placementLabel, placementName).
					WithDecisions("cluster1").Build(),
			},
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithLabel(clusterSetLabel, clusterSetName).Build(),
				testinghelpers.NewManagedCluster("cluster2").WithLabel(clusterSetLabel, clusterSetName).Build(),
			},
			decisions: []runtime.Object{
				testinghelpers.NewPlacementDecision(placementNamespace, placementDecisionName(placementName, 1)).
					WithLabel(placementLabel, placementName).
					WithDecisions("cluster1").Build(),
			},
			expectedDecisions: []clusterapiv1alpha1.ClusterDecision{
				{ClusterName: "cluster1"},
				{ClusterName: "cluster2"},
			},
			expectedFilterResult: []FilterResult{
				{
					Name:             "predicate",
					FilteredClusters: []string{"cluster1", "cluster2"},
				},
			},
			expectedScoreResult: []PriorizeResult{
				{
					Name:   "balance",
					Scores: PrioritizeSore{"cluster1": 100, "cluster2": 100},
				},
				{
					Name:   "steady",
					Scores: PrioritizeSore{"cluster1": 100, "cluster2": 0},
				},
			},
			expectedUnScheduled: 2,
		},
		{
			name:      "schedule to cluster with least decisions",
			placement: testinghelpers.NewPlacement(placementNamespace, placementName).WithNOC(1).Build(),
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet(clusterSetName),
				testinghelpers.NewClusterSetBinding(placementNamespace, clusterSetName),
				testinghelpers.NewPlacementDecision(placementNamespace, placementDecisionName("others", 1)).
					WithDecisions("cluster1", "cluster2").Build(),
			},
			decisions: []runtime.Object{
				testinghelpers.NewPlacementDecision(placementNamespace, placementDecisionName("others", 1)).
					WithDecisions("cluster1", "cluster2").Build(),
			},
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithLabel(clusterSetLabel, clusterSetName).Build(),
				testinghelpers.NewManagedCluster("cluster2").WithLabel(clusterSetLabel, clusterSetName).Build(),
				testinghelpers.NewManagedCluster("cluster3").WithLabel(clusterSetLabel, clusterSetName).Build(),
			},
			expectedDecisions: []clusterapiv1alpha1.ClusterDecision{
				{ClusterName: "cluster3"},
			},
			expectedFilterResult: []FilterResult{
				{
					Name:             "predicate",
					FilteredClusters: []string{"cluster3", "cluster1", "cluster2"},
				},
			},
			expectedScoreResult: []PriorizeResult{
				{
					Name:   "balance",
					Scores: PrioritizeSore{"cluster1": -100, "cluster2": -100, "cluster3": 100},
				},
				{
					Name:   "steady",
					Scores: PrioritizeSore{"cluster1": 0, "cluster2": 0, "cluster3": 0},
				},
			},
			expectedUnScheduled: 0,
		},
		{
			name:      "do not schedule to other cluster even with least decisions",
			placement: testinghelpers.NewPlacement(placementNamespace, placementName).WithNOC(1).Build(),
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet(clusterSetName),
				testinghelpers.NewClusterSetBinding(placementNamespace, clusterSetName),
				testinghelpers.NewPlacementDecision(placementNamespace, placementDecisionName("others", 1)).
					WithDecisions("cluster3", "cluster2").Build(),
				testinghelpers.NewPlacementDecision(placementNamespace, placementDecisionName("others", 2)).
					WithDecisions("cluster2", "cluster1").Build(),
				testinghelpers.NewPlacementDecision(placementNamespace, placementDecisionName(placementName, 1)).
					WithLabel(placementLabel, placementName).
					WithDecisions("cluster3").Build(),
			},
			decisions: []runtime.Object{
				testinghelpers.NewPlacementDecision(placementNamespace, placementDecisionName("others", 1)).
					WithDecisions("cluster3", "cluster2").Build(),
				testinghelpers.NewPlacementDecision(placementNamespace, placementDecisionName("others", 2)).
					WithDecisions("cluster2", "cluster1").Build(),
				testinghelpers.NewPlacementDecision(placementNamespace, placementDecisionName(placementName, 1)).
					WithLabel(placementLabel, placementName).
					WithDecisions("cluster3").Build(),
			},
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithLabel(clusterSetLabel, clusterSetName).Build(),
				testinghelpers.NewManagedCluster("cluster2").WithLabel(clusterSetLabel, clusterSetName).Build(),
				testinghelpers.NewManagedCluster("cluster3").WithLabel(clusterSetLabel, clusterSetName).Build(),
			},
			expectedDecisions: []clusterapiv1alpha1.ClusterDecision{
				{ClusterName: "cluster3"},
			},
			expectedFilterResult: []FilterResult{
				{
					Name:             "predicate",
					FilteredClusters: []string{"cluster3", "cluster1", "cluster2"},
				},
			},
			expectedScoreResult: []PriorizeResult{
				{
					Name:   "balance",
					Scores: PrioritizeSore{"cluster1": 0, "cluster2": -100, "cluster3": 0},
				},
				{
					Name:   "steady",
					Scores: PrioritizeSore{"cluster1": 0, "cluster2": 0, "cluster3": 100},
				},
			},
			expectedUnScheduled: 0,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.initObjs = append(c.initObjs, c.placement)
			clusterClient := clusterfake.NewSimpleClientset(c.initObjs...)
			s := NewPluginScheduler(testinghelpers.NewFakePluginHandle(t, clusterClient, c.initObjs...))
			result, err := s.Schedule(
				context.TODO(),
				c.placement,
				c.clusters,
			)
			if err != nil {
				t.Errorf("unexpected err: %v", err)
			}
			if !reflect.DeepEqual(result.Decisions(), c.expectedDecisions) {
				t.Errorf("expected %v scheduled, but got %v", c.expectedDecisions, result.Decisions())
			}
			if result.NumOfUnscheduled() != c.expectedUnScheduled {
				t.Errorf("expected %d unscheduled, but got %d", c.expectedUnScheduled, result.NumOfUnscheduled())
			}

			actual, _ := json.Marshal(result.FilterResults())
			expected, _ := json.Marshal(c.expectedFilterResult)
			if !reflect.DeepEqual(actual, expected) {
				t.Errorf("expected filter results %v, but got %v", string(expected), string(actual))
			}

			actual, _ = json.Marshal(result.PriorizeResults())
			expected, _ = json.Marshal(c.expectedScoreResult)
			if !reflect.DeepEqual(actual, expected) {
				t.Errorf("expected score results %v, but got %v", string(expected), string(actual))
			}
		})
	}
}

func placementDecisionName(placementName string, index int) string {
	return fmt.Sprintf("%s-decision-%d", placementName, index)
}

func TestFilterResults(t *testing.T) {

}
