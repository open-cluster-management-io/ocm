package scheduling

import (
	"context"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	clienttesting "k8s.io/client-go/testing"

	clusterfake "github.com/open-cluster-management/api/client/cluster/clientset/versioned/fake"
	clusterapiv1 "github.com/open-cluster-management/api/cluster/v1"
	clusterapiv1alpha1 "github.com/open-cluster-management/api/cluster/v1alpha1"
	testinghelpers "github.com/open-cluster-management/placement/pkg/helpers/testing"
)

func TestSchedule(t *testing.T) {
	clusterSetName := "clusterSets"
	placementNamespace := "ns1"
	placementName := "placement1"
	placementDecisionName := "placement1-decision1"

	cases := []struct {
		name            string
		placement       *clusterapiv1alpha1.Placement
		initObjs        []runtime.Object
		clusters        []*clusterapiv1.ManagedCluster
		scheduleResult  scheduleResult
		validateActions func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:      "new placement satisfied",
			placement: testinghelpers.NewPlacement(placementNamespace, placementName).Build(),
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet(clusterSetName),
				testinghelpers.NewClusterSetBinding(placementNamespace, clusterSetName),
			},
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithLabel(clusterSetLabel, clusterSetName).Build(),
			},
			scheduleResult: scheduleResult{
				scheduled: 1,
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				// check if PlacementDecision has been created
				testinghelpers.AssertActions(t, actions, "create", "update")
				actual := actions[1].(clienttesting.UpdateActionImpl).Object
				placementDecision, ok := actual.(*clusterapiv1alpha1.PlacementDecision)
				if !ok {
					t.Errorf("expected PlacementDecision was updated")
				}
				assertClustersSelected(t, placementDecision.Status.Decisions, "cluster1")
			},
		},
		{
			name:      "new placement unsatisfied",
			placement: testinghelpers.NewPlacement(placementNamespace, placementName).WithNOC(3).Build(),
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet(clusterSetName),
				testinghelpers.NewClusterSetBinding(placementNamespace, clusterSetName),
			},
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithLabel(clusterSetLabel, clusterSetName).Build(),
			},
			scheduleResult: scheduleResult{
				scheduled:   1,
				unscheduled: 2,
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				// check if PlacementDecision has been updated
				testinghelpers.AssertActions(t, actions, "create", "update")
				actual := actions[1].(clienttesting.UpdateActionImpl).Object
				placementDecision, ok := actual.(*clusterapiv1alpha1.PlacementDecision)
				if !ok {
					t.Errorf("expected PlacementDecision was updated")
				}
				assertClustersSelected(t, placementDecision.Status.Decisions, "cluster1")
			},
		},
		{
			name:      "placement with all decisions scheduled",
			placement: testinghelpers.NewPlacement(placementNamespace, placementName).WithNOC(2).Build(),
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet(clusterSetName),
				testinghelpers.NewClusterSetBinding(placementNamespace, clusterSetName),
				testinghelpers.NewPlacementDecision(placementNamespace, placementDecisionName).
					WithLabel(placementLabel, placementName).
					WithDecisions("cluster1", "cluster2").Build(),
			},
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithLabel(clusterSetLabel, clusterSetName).Build(),
				testinghelpers.NewManagedCluster("cluster2").WithLabel(clusterSetLabel, clusterSetName).Build(),
			},
			scheduleResult: scheduleResult{
				scheduled: 2,
			},
			validateActions: testinghelpers.AssertNoActions,
		},
		{
			name:      "placement with part of decisions scheduled",
			placement: testinghelpers.NewPlacement(placementNamespace, placementName).WithNOC(4).Build(),
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet(clusterSetName),
				testinghelpers.NewClusterSetBinding(placementNamespace, clusterSetName),
				testinghelpers.NewPlacementDecision(placementNamespace, placementDecisionName).
					WithLabel(placementLabel, placementName).WithDecisions("cluster1").Build(),
			},
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithLabel(clusterSetLabel, clusterSetName).Build(),
				testinghelpers.NewManagedCluster("cluster2").WithLabel(clusterSetLabel, clusterSetName).Build(),
			},
			scheduleResult: scheduleResult{
				scheduled:   2,
				unscheduled: 2,
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				// check if PlacementDecision has been updated
				testinghelpers.AssertActions(t, actions, "update")
				actual := actions[0].(clienttesting.UpdateActionImpl).Object
				placementDecision, ok := actual.(*clusterapiv1alpha1.PlacementDecision)
				if !ok {
					t.Errorf("expected PlacementDecision was updated")
				}
				assertClustersSelected(t, placementDecision.Status.Decisions, "cluster1", "cluster2")
			},
		},
		{
			name:      "placement without more feasible cluster available",
			placement: testinghelpers.NewPlacement(placementNamespace, placementName).WithNOC(4).Build(),
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet(clusterSetName),
				testinghelpers.NewClusterSetBinding(placementNamespace, clusterSetName),
				testinghelpers.NewPlacementDecision(placementNamespace, placementDecisionName).
					WithLabel(placementLabel, placementName).WithDecisions("cluster1").Build(),
			},
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithLabel(clusterSetLabel, clusterSetName).Build(),
			},
			scheduleResult: scheduleResult{
				scheduled:   1,
				unscheduled: 3,
			},
			validateActions: testinghelpers.AssertNoActions,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.initObjs = append(c.initObjs, c.placement)
			clusterClient := clusterfake.NewSimpleClientset(c.initObjs...)
			clusterInformerFactory := testinghelpers.NewClusterInformerFactory(clusterClient, c.initObjs...)
			result, err := schedule(
				context.TODO(),
				c.placement,
				c.clusters,
				clusterClient,
				clusterInformerFactory.Cluster().V1alpha1().PlacementDecisions().Lister(),
			)
			if err != nil {
				t.Errorf("unexpected err: %v", err)
			}
			if result.scheduled != c.scheduleResult.scheduled {
				t.Errorf("expected %d scheduled, but got %d", c.scheduleResult.scheduled, result.scheduled)
			}
			if result.unscheduled != c.scheduleResult.unscheduled {
				t.Errorf("expected %d unscheduled, but got %d", c.scheduleResult.unscheduled, result.unscheduled)
			}
			c.validateActions(t, clusterClient.Actions())
		})
	}
}

func assertClustersSelected(t *testing.T, decisons []clusterapiv1alpha1.ClusterDecision, clusterNames ...string) {
	names := sets.NewString(clusterNames...)
	for _, decision := range decisons {
		if names.Has(decision.ClusterName) {
			names.Delete(decision.ClusterName)
		}
	}

	if names.Len() != 0 {
		t.Errorf("expected clusters selected: %s", strings.Join(names.UnsortedList(), ","))
	}
}
