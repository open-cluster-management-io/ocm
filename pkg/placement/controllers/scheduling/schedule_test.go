package scheduling

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	clusterapiv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	clusterlisterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	clusterapiv1beta2 "open-cluster-management.io/api/cluster/v1beta2"

	"open-cluster-management.io/ocm/pkg/placement/controllers/framework"
	testinghelpers "open-cluster-management.io/ocm/pkg/placement/helpers/testing"
)

func TestSchedule(t *testing.T) {
	clusterSetName := "clusterSets"

	cases := []struct {
		name                 string
		placement            *clusterlisterv1beta1.Placement
		initObjs             []runtime.Object
		clusters             []*clusterapiv1.ManagedCluster
		expectedFilterResult []FilterResult
		expectedScoreResult  []PrioritizerResult
		expectedDecisions    []*clusterapiv1.ManagedCluster
		expectedUnScheduled  int
		expectedStatus       framework.Status
	}{
		{
			name:      "new placement satisfied",
			placement: testinghelpers.NewPlacement(placementNamespace, placementName).Build(),
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet(clusterSetName).Build(),
				testinghelpers.NewClusterSetBinding(placementNamespace, clusterSetName),
			},
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithLabel(
					clusterapiv1beta2.ClusterSetLabel, clusterSetName).Build(),
			},
			expectedDecisions: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithLabel(
					clusterapiv1beta2.ClusterSetLabel, clusterSetName).Build(),
			},
			expectedFilterResult: []FilterResult{
				{
					Name:             "Predicate",
					FilteredClusters: []string{"cluster1"},
				},
				{
					Name:             "Predicate,TaintToleration",
					FilteredClusters: []string{"cluster1"},
				},
			},
			expectedScoreResult: []PrioritizerResult{
				{
					Name:   "Balance",
					Weight: 1,
					Scores: PrioritizerScore{"cluster1": 100},
				},
				{
					Name:   "Steady",
					Weight: 1,
					Scores: PrioritizerScore{"cluster1": 0},
				},
			},
			expectedUnScheduled: 0,
			expectedStatus:      *framework.NewStatus("", framework.Success, ""),
		},
		{
			name:      "new placement unsatisfied",
			placement: testinghelpers.NewPlacement(placementNamespace, placementName).WithNOC(3).Build(),
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet(clusterSetName).Build(),
				testinghelpers.NewClusterSetBinding(placementNamespace, clusterSetName),
			},
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithLabel(clusterapiv1beta2.ClusterSetLabel, clusterSetName).Build(),
			},
			expectedDecisions: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithLabel(clusterapiv1beta2.ClusterSetLabel, clusterSetName).Build(),
			},
			expectedFilterResult: []FilterResult{
				{
					Name:             "Predicate",
					FilteredClusters: []string{"cluster1"},
				},
				{
					Name:             "Predicate,TaintToleration",
					FilteredClusters: []string{"cluster1"},
				},
			},
			expectedScoreResult: []PrioritizerResult{
				{
					Name:   "Balance",
					Weight: 1,
					Scores: PrioritizerScore{"cluster1": 100},
				},
				{
					Name:   "Steady",
					Weight: 1,
					Scores: PrioritizerScore{"cluster1": 0},
				},
			},
			expectedUnScheduled: 2,
			expectedStatus:      *framework.NewStatus("", framework.Success, ""),
		},
		{
			name: "new placement misconfigured",
			placement: testinghelpers.NewPlacement(placementNamespace, placementName).WithNOC(3).AddToleration(
				&clusterapiv1beta1.Toleration{
					Value:    "value1",
					Operator: clusterapiv1beta1.TolerationOpEqual,
				}).Build(),
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet(clusterSetName).Build(),
				testinghelpers.NewClusterSetBinding(placementNamespace, clusterSetName),
			},
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithLabel(clusterapiv1beta2.ClusterSetLabel, clusterSetName).Build(),
			},
			expectedDecisions: []*clusterapiv1.ManagedCluster{},
			expectedFilterResult: []FilterResult{
				{
					Name:             "Predicate",
					FilteredClusters: []string{"cluster1"},
				},
			},
			expectedScoreResult: []PrioritizerResult{},
			expectedUnScheduled: 0,
			expectedStatus: *framework.NewStatus(
				"TaintToleration",
				framework.Misconfigured,
				"If the key is empty, operator must be Exists.",
			),
		},
		{
			name:      "placement with all decisions scheduled",
			placement: testinghelpers.NewPlacement(placementNamespace, placementName).WithNOC(2).Build(),
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet(clusterSetName).Build(),
				testinghelpers.NewClusterSetBinding(placementNamespace, clusterSetName),
				testinghelpers.NewPlacementDecision(placementNamespace, testinghelpers.PlacementDecisionName(placementName, 1)).
					WithLabel(clusterapiv1beta1.PlacementLabel, placementName).
					WithDecisions("cluster1", "cluster2").Build(),
			},
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithLabel(clusterapiv1beta2.ClusterSetLabel, clusterSetName).Build(),
				testinghelpers.NewManagedCluster("cluster2").WithLabel(clusterapiv1beta2.ClusterSetLabel, clusterSetName).Build(),
				testinghelpers.NewManagedCluster("cluster3").WithLabel(clusterapiv1beta2.ClusterSetLabel, clusterSetName).Build(),
			},
			expectedDecisions: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithLabel(clusterapiv1beta2.ClusterSetLabel, clusterSetName).Build(),
				testinghelpers.NewManagedCluster("cluster2").WithLabel(clusterapiv1beta2.ClusterSetLabel, clusterSetName).Build(),
			},
			expectedFilterResult: []FilterResult{
				{
					Name:             "Predicate",
					FilteredClusters: []string{"cluster1", "cluster2", "cluster3"},
				},
				{
					Name:             "Predicate,TaintToleration",
					FilteredClusters: []string{"cluster1", "cluster2", "cluster3"},
				},
			},
			expectedScoreResult: []PrioritizerResult{
				{
					Name:   "Balance",
					Weight: 1,
					Scores: PrioritizerScore{"cluster1": 100, "cluster2": 100, "cluster3": 100},
				},
				{
					Name:   "Steady",
					Weight: 1,
					Scores: PrioritizerScore{"cluster1": 100, "cluster2": 100, "cluster3": 0},
				},
			},
			expectedUnScheduled: 0,
			expectedStatus:      *framework.NewStatus("", framework.Success, ""),
		},
		{
			name:      "placement with empty Prioritizer Policy",
			placement: testinghelpers.NewPlacement(placementNamespace, placementName).WithPrioritizerPolicy("").Build(),
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet(clusterSetName).Build(),
				testinghelpers.NewClusterSetBinding(placementNamespace, clusterSetName),
			},
			expectedDecisions: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithLabel(clusterapiv1beta2.ClusterSetLabel, clusterSetName).Build(),
			},
			expectedFilterResult: []FilterResult{
				{
					Name:             "Predicate",
					FilteredClusters: []string{"cluster1"},
				},
				{
					Name:             "Predicate,TaintToleration",
					FilteredClusters: []string{"cluster1"},
				},
			},
			expectedScoreResult: []PrioritizerResult{
				{
					Name:   "Balance",
					Weight: 1,
					Scores: PrioritizerScore{"cluster1": 100},
				},
				{
					Name:   "Steady",
					Weight: 1,
					Scores: PrioritizerScore{"cluster1": 0},
				},
			},
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithLabel(clusterapiv1beta2.ClusterSetLabel, clusterSetName).Build(),
			},
			expectedUnScheduled: 0,
			expectedStatus:      *framework.NewStatus("", framework.Success, ""),
		},
		{
			name: "placement with taint and toleration",
			placement: testinghelpers.NewPlacement(placementNamespace, placementName).WithNOC(3).AddToleration(
				&clusterapiv1beta1.Toleration{
					Key:      "key1",
					Value:    "value1",
					Operator: clusterapiv1beta1.TolerationOpEqual,
				}).Build(),
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet(clusterSetName).Build(),
				testinghelpers.NewClusterSetBinding(placementNamespace, clusterSetName),
				testinghelpers.NewAddOnPlacementScore("cluster1", "demo").WithScore("demo", 30).Build(),
				testinghelpers.NewAddOnPlacementScore("cluster2", "demo").WithScore("demo", 40).Build(),
				testinghelpers.NewAddOnPlacementScore("cluster3", "demo").WithScore("demo", 50).Build(),
			},
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithLabel(clusterapiv1beta2.ClusterSetLabel, clusterSetName).WithTaint(
					&clusterapiv1.Taint{
						Key:       "key1",
						Value:     "value1",
						Effect:    clusterapiv1.TaintEffectNoSelect,
						TimeAdded: metav1.Time{},
					}).Build(),
				testinghelpers.NewManagedCluster("cluster2").WithLabel(clusterapiv1beta2.ClusterSetLabel, clusterSetName).WithTaint(
					&clusterapiv1.Taint{
						Key:       "key2",
						Value:     "value2",
						Effect:    clusterapiv1.TaintEffectNoSelect,
						TimeAdded: metav1.Time{},
					}).Build(),
				testinghelpers.NewManagedCluster("cluster3").WithLabel(clusterapiv1beta2.ClusterSetLabel, clusterSetName).Build(),
			},
			expectedDecisions: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithLabel(clusterapiv1beta2.ClusterSetLabel, clusterSetName).WithTaint(
					&clusterapiv1.Taint{
						Key:       "key1",
						Value:     "value1",
						Effect:    clusterapiv1.TaintEffectNoSelect,
						TimeAdded: metav1.Time{},
					}).Build(),
				testinghelpers.NewManagedCluster("cluster3").WithLabel(clusterapiv1beta2.ClusterSetLabel, clusterSetName).Build(),
			},
			expectedFilterResult: []FilterResult{
				{
					Name:             "Predicate",
					FilteredClusters: []string{"cluster1", "cluster2", "cluster3"},
				},
				{
					Name:             "Predicate,TaintToleration",
					FilteredClusters: []string{"cluster1", "cluster3"},
				},
			},
			expectedScoreResult: []PrioritizerResult{
				{
					Name:   "Balance",
					Weight: 1,
					Scores: PrioritizerScore{"cluster1": 100, "cluster3": 100},
				},
				{
					Name:   "Steady",
					Weight: 1,
					Scores: PrioritizerScore{"cluster1": 0, "cluster3": 0},
				},
			},
			expectedUnScheduled: 1,
			expectedStatus:      *framework.NewStatus("", framework.Success, ""),
		},
		{
			name: "placement with additive Prioritizer Policy",
			placement: testinghelpers.NewPlacement(placementNamespace, placementName).
				WithNOC(2).
				WithPrioritizerPolicy("Additive").
				WithPrioritizerConfig("Balance", 3).
				WithPrioritizerConfig("ResourceAllocatableMemory", 1).
				WithScoreCoordinateAddOn("demo", "demo", 1).Build(),
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet(clusterSetName).Build(),
				testinghelpers.NewClusterSetBinding(placementNamespace, clusterSetName),
				testinghelpers.NewAddOnPlacementScore("cluster1", "demo").WithScore("demo", 30).Build(),
				testinghelpers.NewAddOnPlacementScore("cluster2", "demo").WithScore("demo", 40).Build(),
				testinghelpers.NewAddOnPlacementScore("cluster3", "demo").WithScore("demo", 50).Build(),
			},
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").
					WithLabel(clusterapiv1beta2.ClusterSetLabel, clusterSetName).
					WithResource(clusterapiv1.ResourceMemory, "100", "100").Build(),
				testinghelpers.NewManagedCluster("cluster2").
					WithLabel(clusterapiv1beta2.ClusterSetLabel, clusterSetName).
					WithResource(clusterapiv1.ResourceMemory, "50", "100").Build(),
				testinghelpers.NewManagedCluster("cluster3").
					WithLabel(clusterapiv1beta2.ClusterSetLabel, clusterSetName).
					WithResource(clusterapiv1.ResourceMemory, "0", "100").Build(),
			},
			expectedDecisions: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").
					WithLabel(clusterapiv1beta2.ClusterSetLabel, clusterSetName).
					WithResource(clusterapiv1.ResourceMemory, "100", "100").Build(),
				testinghelpers.NewManagedCluster("cluster2").
					WithLabel(clusterapiv1beta2.ClusterSetLabel, clusterSetName).
					WithResource(clusterapiv1.ResourceMemory, "50", "100").Build(),
			},
			expectedFilterResult: []FilterResult{
				{
					Name:             "Predicate",
					FilteredClusters: []string{"cluster1", "cluster2", "cluster3"},
				},
				{
					Name:             "Predicate,TaintToleration",
					FilteredClusters: []string{"cluster1", "cluster2", "cluster3"},
				},
			},
			expectedScoreResult: []PrioritizerResult{
				{
					Name:   "Balance",
					Weight: 3,
					Scores: PrioritizerScore{"cluster1": 100, "cluster2": 100, "cluster3": 100},
				},
				{
					Name:   "Steady",
					Weight: 1,
					Scores: PrioritizerScore{"cluster1": 0, "cluster2": 0, "cluster3": 0},
				},
				{
					Name:   "ResourceAllocatableMemory",
					Weight: 1,
					Scores: PrioritizerScore{"cluster1": 100, "cluster2": 0, "cluster3": -100},
				},
				{
					Name:   "AddOn/demo/demo",
					Weight: 1,
					Scores: PrioritizerScore{"cluster1": 30, "cluster2": 40, "cluster3": 50},
				},
			},
			expectedUnScheduled: 0,
			expectedStatus:      *framework.NewStatus("", framework.Success, ""),
		},
		{
			name: "placement with exact Prioritizer Policy",
			placement: testinghelpers.NewPlacement(placementNamespace, placementName).
				WithNOC(2).WithPrioritizerPolicy("Exact").
				WithPrioritizerConfig("Balance", 3).
				WithPrioritizerConfig("ResourceAllocatableMemory", 1).Build(),
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet(clusterSetName).Build(),
				testinghelpers.NewClusterSetBinding(placementNamespace, clusterSetName),
			},
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").
					WithLabel(clusterapiv1beta2.ClusterSetLabel, clusterSetName).
					WithResource(clusterapiv1.ResourceMemory, "100", "100").Build(),
				testinghelpers.NewManagedCluster("cluster2").
					WithLabel(clusterapiv1beta2.ClusterSetLabel, clusterSetName).
					WithResource(clusterapiv1.ResourceMemory, "50", "100").Build(),
				testinghelpers.NewManagedCluster("cluster3").
					WithLabel(clusterapiv1beta2.ClusterSetLabel, clusterSetName).
					WithResource(clusterapiv1.ResourceMemory, "0", "100").Build(),
			},
			expectedDecisions: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").
					WithLabel(clusterapiv1beta2.ClusterSetLabel, clusterSetName).
					WithResource(clusterapiv1.ResourceMemory, "100", "100").Build(),
				testinghelpers.NewManagedCluster("cluster2").
					WithLabel(clusterapiv1beta2.ClusterSetLabel, clusterSetName).
					WithResource(clusterapiv1.ResourceMemory, "50", "100").Build(),
			},
			expectedFilterResult: []FilterResult{
				{
					Name:             "Predicate",
					FilteredClusters: []string{"cluster1", "cluster2", "cluster3"},
				},
				{
					Name:             "Predicate,TaintToleration",
					FilteredClusters: []string{"cluster1", "cluster2", "cluster3"},
				},
			},
			expectedScoreResult: []PrioritizerResult{
				{
					Name:   "Balance",
					Weight: 3,
					Scores: PrioritizerScore{"cluster1": 100, "cluster2": 100, "cluster3": 100},
				},
				{
					Name:   "ResourceAllocatableMemory",
					Weight: 1,
					Scores: PrioritizerScore{"cluster1": 100, "cluster2": 0, "cluster3": -100},
				},
			},
			expectedUnScheduled: 0,
			expectedStatus:      *framework.NewStatus("", framework.Success, ""),
		},
		{
			name:      "placement with part of decisions scheduled",
			placement: testinghelpers.NewPlacement(placementNamespace, placementName).WithNOC(4).Build(),
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet(clusterSetName).Build(),
				testinghelpers.NewClusterSetBinding(placementNamespace, clusterSetName),
				testinghelpers.NewPlacementDecision(placementNamespace, testinghelpers.PlacementDecisionName(placementName, 1)).
					WithLabel(clusterapiv1beta1.PlacementLabel, placementName).
					WithDecisions("cluster1").Build(),
			},
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").
					WithLabel(clusterapiv1beta2.ClusterSetLabel, clusterSetName).Build(),
				testinghelpers.NewManagedCluster("cluster2").
					WithLabel(clusterapiv1beta2.ClusterSetLabel, clusterSetName).Build(),
			},
			expectedDecisions: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").
					WithLabel(clusterapiv1beta2.ClusterSetLabel, clusterSetName).Build(),
				testinghelpers.NewManagedCluster("cluster2").
					WithLabel(clusterapiv1beta2.ClusterSetLabel, clusterSetName).Build(),
			},
			expectedFilterResult: []FilterResult{
				{
					Name:             "Predicate",
					FilteredClusters: []string{"cluster1", "cluster2"},
				},
				{
					Name:             "Predicate,TaintToleration",
					FilteredClusters: []string{"cluster1", "cluster2"},
				},
			},
			expectedScoreResult: []PrioritizerResult{
				{
					Name:   "Balance",
					Weight: 1,
					Scores: PrioritizerScore{"cluster1": 100, "cluster2": 100},
				},
				{
					Name:   "Steady",
					Weight: 1,
					Scores: PrioritizerScore{"cluster1": 100, "cluster2": 0},
				},
			},
			expectedUnScheduled: 2,
			expectedStatus:      *framework.NewStatus("", framework.Success, ""),
		},
		{
			name:      "schedule to cluster with least decisions",
			placement: testinghelpers.NewPlacement(placementNamespace, placementName).WithNOC(1).Build(),
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet(clusterSetName).Build(),
				testinghelpers.NewClusterSetBinding(placementNamespace, clusterSetName),
				testinghelpers.NewPlacementDecision(placementNamespace, testinghelpers.PlacementDecisionName("others", 1)).
					WithDecisions("cluster1", "cluster2").Build(),
			},
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").
					WithLabel(clusterapiv1beta2.ClusterSetLabel, clusterSetName).Build(),
				testinghelpers.NewManagedCluster("cluster2").
					WithLabel(clusterapiv1beta2.ClusterSetLabel, clusterSetName).Build(),
				testinghelpers.NewManagedCluster("cluster3").
					WithLabel(clusterapiv1beta2.ClusterSetLabel, clusterSetName).Build(),
			},
			expectedDecisions: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster3").
					WithLabel(clusterapiv1beta2.ClusterSetLabel, clusterSetName).Build(),
			},
			expectedFilterResult: []FilterResult{
				{
					Name:             "Predicate",
					FilteredClusters: []string{"cluster1", "cluster2", "cluster3"},
				},
				{
					Name:             "Predicate,TaintToleration",
					FilteredClusters: []string{"cluster3", "cluster1", "cluster2"},
				},
			},
			expectedScoreResult: []PrioritizerResult{
				{
					Name:   "Balance",
					Weight: 1,
					Scores: PrioritizerScore{"cluster1": -100, "cluster2": -100, "cluster3": 100},
				},
				{
					Name:   "Steady",
					Weight: 1,
					Scores: PrioritizerScore{"cluster1": 0, "cluster2": 0, "cluster3": 0},
				},
			},
			expectedUnScheduled: 0,
			expectedStatus:      *framework.NewStatus("", framework.Success, ""),
		},
		{
			name:      "do not schedule to other cluster even with least decisions",
			placement: testinghelpers.NewPlacement(placementNamespace, placementName).WithNOC(1).Build(),
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet(clusterSetName).Build(),
				testinghelpers.NewClusterSetBinding(placementNamespace, clusterSetName),
				testinghelpers.NewPlacementDecision(placementNamespace, testinghelpers.PlacementDecisionName("others", 1)).
					WithDecisions("cluster2", "cluster3").Build(),
				testinghelpers.NewPlacementDecision(placementNamespace, testinghelpers.PlacementDecisionName("others", 2)).
					WithDecisions("cluster1", "cluster2").Build(),
				testinghelpers.NewPlacementDecision(placementNamespace, testinghelpers.PlacementDecisionName(placementName, 1)).
					WithLabel(clusterapiv1beta1.PlacementLabel, placementName).
					WithDecisions("cluster3").Build(),
			},
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").
					WithLabel(clusterapiv1beta2.ClusterSetLabel, clusterSetName).Build(),
				testinghelpers.NewManagedCluster("cluster2").
					WithLabel(clusterapiv1beta2.ClusterSetLabel, clusterSetName).Build(),
				testinghelpers.NewManagedCluster("cluster3").
					WithLabel(clusterapiv1beta2.ClusterSetLabel, clusterSetName).Build(),
			},
			expectedDecisions: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster3").
					WithLabel(clusterapiv1beta2.ClusterSetLabel, clusterSetName).Build(),
			},
			expectedFilterResult: []FilterResult{
				{
					Name:             "Predicate",
					FilteredClusters: []string{"cluster1", "cluster2", "cluster3"},
				},
				{
					Name:             "Predicate,TaintToleration",
					FilteredClusters: []string{"cluster3", "cluster1", "cluster2"},
				},
			},
			expectedScoreResult: []PrioritizerResult{
				{
					Name:   "Balance",
					Weight: 1,
					Scores: PrioritizerScore{"cluster1": 0, "cluster2": -100, "cluster3": 0},
				},
				{
					Name:   "Steady",
					Weight: 1,
					Scores: PrioritizerScore{"cluster1": 0, "cluster2": 0, "cluster3": 100},
				},
			},
			expectedUnScheduled: 0,
			expectedStatus:      *framework.NewStatus("", framework.Success, ""),
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.initObjs = append(c.initObjs, c.placement)
			clusterClient := clusterfake.NewSimpleClientset(c.initObjs...)
			s := NewPluginScheduler(testinghelpers.NewFakePluginHandle(t, clusterClient, c.initObjs...))
			result, status := s.Schedule(
				context.TODO(),
				c.placement,
				c.clusters,
			)
			// TODO
			if status.Message() != c.expectedStatus.Message() && status.Code() != c.expectedStatus.Code() {
				t.Errorf("unexpected err: %v", status.AsError())
			}
			if len(c.expectedDecisions) != 0 && !reflect.DeepEqual(result.Decisions(), c.expectedDecisions) {
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

			prioritizerResult := result.PrioritizerResults()
			sort.SliceStable(prioritizerResult, func(i, j int) bool {
				return prioritizerResult[i].Name < prioritizerResult[j].Name
			})
			expectedScoreResult := c.expectedScoreResult
			sort.SliceStable(expectedScoreResult, func(i, j int) bool {
				return expectedScoreResult[i].Name < expectedScoreResult[j].Name
			})

			actual, _ = json.Marshal(prioritizerResult)
			expected, _ = json.Marshal(c.expectedScoreResult)
			if !reflect.DeepEqual(actual, expected) {
				t.Errorf("expected score results %v, but got %v", string(expected), string(actual))
			}
		})
	}
}

func TestFilterResults(t *testing.T) {

}
