package scheduling

import (
	"context"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	clienttesting "k8s.io/client-go/testing"

	clusterclient "github.com/open-cluster-management/api/client/cluster/clientset/versioned"
	clusterfake "github.com/open-cluster-management/api/client/cluster/clientset/versioned/fake"
	clusterlisterv1alpha1 "github.com/open-cluster-management/api/client/cluster/listers/cluster/v1alpha1"
	clusterapiv1 "github.com/open-cluster-management/api/cluster/v1"
	clusterapiv1alpha1 "github.com/open-cluster-management/api/cluster/v1alpha1"
	testinghelpers "github.com/open-cluster-management/placement/pkg/helpers/testing"
)

func TestSchedulingController_sync(t *testing.T) {
	placementNamespace := "ns1"
	placementName := "placement1"

	cases := []struct {
		name            string
		placement       *clusterapiv1alpha1.Placement
		initObjs        []runtime.Object
		scheduleResult  *scheduleResult
		validateActions func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:      "placement satisfied",
			placement: testinghelpers.NewPlacement(placementNamespace, placementName).Build(),
			scheduleResult: &scheduleResult{
				scheduled:   3,
				unscheduled: 0,
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				// check if PlacementDecision has been updated
				testinghelpers.AssertActions(t, actions, "update")
				// check if Placement has been updated
				actual := actions[0].(clienttesting.UpdateActionImpl).Object
				placement, ok := actual.(*clusterapiv1alpha1.Placement)
				if !ok {
					t.Errorf("expected Placement was updated")
				}

				if placement.Status.NumberOfSelectedClusters != int32(3) {
					t.Errorf("expecte %d cluster selected, but got %d", 3, placement.Status.NumberOfSelectedClusters)
				}
				testinghelpers.HasCondition(
					placement.Status.Conditions,
					clusterapiv1alpha1.PlacementConditionSatisfied,
					"AllDecisionsScheduled",
					metav1.ConditionTrue,
				)
			},
		},
		{
			name:      "placement unsatisfied",
			placement: testinghelpers.NewPlacement(placementNamespace, placementName).Build(),
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet("clusterset1"),
				testinghelpers.NewClusterSetBinding(placementNamespace, "clusterset1"),
				testinghelpers.NewManagedCluster("cluster1").WithLabel(clusterSetLabel, "clusterset1").Build(),
			},
			scheduleResult: &scheduleResult{
				scheduled:   3,
				unscheduled: 1,
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				// check if PlacementDecision has been updated
				testinghelpers.AssertActions(t, actions, "update")
				// check if Placement has been updated
				actual := actions[0].(clienttesting.UpdateActionImpl).Object
				placement, ok := actual.(*clusterapiv1alpha1.Placement)
				if !ok {
					t.Errorf("expected Placement was updated")
				}

				if placement.Status.NumberOfSelectedClusters != int32(3) {
					t.Errorf("expecte %d cluster selected, but got %d", 3, placement.Status.NumberOfSelectedClusters)
				}
				testinghelpers.HasCondition(
					placement.Status.Conditions,
					clusterapiv1alpha1.PlacementConditionSatisfied,
					"NotAllDecisionsScheduled",
					metav1.ConditionFalse,
				)
			},
		},
		{
			name:      "placement missing managedclustersetbindings",
			placement: testinghelpers.NewPlacement(placementNamespace, placementName).Build(),
			scheduleResult: &scheduleResult{
				scheduled:   0,
				unscheduled: 0,
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				// check if PlacementDecision has been updated
				testinghelpers.AssertActions(t, actions, "update")
				// check if Placement has been updated
				actual := actions[0].(clienttesting.UpdateActionImpl).Object
				placement, ok := actual.(*clusterapiv1alpha1.Placement)
				if !ok {
					t.Errorf("expected Placement was updated")
				}

				if placement.Status.NumberOfSelectedClusters != int32(0) {
					t.Errorf("expecte %d cluster selected, but got %d", 0, placement.Status.NumberOfSelectedClusters)
				}
				testinghelpers.HasCondition(
					placement.Status.Conditions,
					clusterapiv1alpha1.PlacementConditionSatisfied,
					"NoManagedClusterSetBindings",
					metav1.ConditionFalse,
				)
			},
		},
		{
			name: "placement status not changed",
			placement: testinghelpers.NewPlacement(placementNamespace, placementName).
				WithNumOfSelectedClusters(3).WithSatisfiedCondition(3, 0).Build(),
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet("clusterset1"),
				testinghelpers.NewClusterSetBinding(placementNamespace, "clusterset1"),
				testinghelpers.NewManagedCluster("cluster1").WithLabel(clusterSetLabel, "clusterset1").Build(),
			},
			scheduleResult: &scheduleResult{
				scheduled:   3,
				unscheduled: 0,
			},
			validateActions: testinghelpers.AssertNoActions,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.initObjs = append(c.initObjs, c.placement)
			clusterClient := clusterfake.NewSimpleClientset(c.initObjs...)
			clusterInformerFactory := testinghelpers.NewClusterInformerFactory(clusterClient, c.initObjs...)

			ctrl := schedulingController{
				clusterClient:           clusterClient,
				clusterLister:           clusterInformerFactory.Cluster().V1().ManagedClusters().Lister(),
				clusterSetLister:        clusterInformerFactory.Cluster().V1alpha1().ManagedClusterSets().Lister(),
				clusterSetBindingLister: clusterInformerFactory.Cluster().V1alpha1().ManagedClusterSetBindings().Lister(),
				placementLister:         clusterInformerFactory.Cluster().V1alpha1().Placements().Lister(),
				placementDecisionLister: clusterInformerFactory.Cluster().V1alpha1().PlacementDecisions().Lister(),
				scheduleFunc: func(
					ctx context.Context,
					placement *clusterapiv1alpha1.Placement,
					clusters []*clusterapiv1.ManagedCluster,
					clusterClient clusterclient.Interface,
					placementDecisionLister clusterlisterv1alpha1.PlacementDecisionLister,
				) (*scheduleResult, error) {
					return c.scheduleResult, nil
				},
			}

			sysCtx := testinghelpers.NewFakeSyncContext(t, c.placement.Namespace+"/"+c.placement.Name)
			syncErr := ctrl.sync(context.TODO(), sysCtx)
			if syncErr != nil {
				t.Errorf("unexpected err: %v", syncErr)
			}

			c.validateActions(t, clusterClient.Actions())
		})
	}
}

func TestGetAvailableClusters(t *testing.T) {
	placementNamespace := "ns1"
	placementName := "placement1"

	cases := []struct {
		name                 string
		placement            *clusterapiv1alpha1.Placement
		initObjs             []runtime.Object
		expectedClusterNames []string
	}{
		{
			name:      "no bound clusterset",
			placement: testinghelpers.NewPlacement(placementNamespace, placementName).Build(),
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet("clusterset1"),
				testinghelpers.NewClusterSetBinding(placementNamespace, "clusterset1"),
			},
		},
		{
			name:      "select all clusters from bound clustersets",
			placement: testinghelpers.NewPlacement(placementNamespace, placementName).Build(),
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet("clusterset1"),
				testinghelpers.NewClusterSet("clusterset2"),
				testinghelpers.NewClusterSetBinding(placementNamespace, "clusterset1"),
				testinghelpers.NewClusterSetBinding(placementNamespace, "clusterset2"),
				testinghelpers.NewManagedCluster("cluster1").WithLabel(clusterSetLabel, "clusterset1").Build(),
				testinghelpers.NewManagedCluster("cluster2").WithLabel(clusterSetLabel, "clusterset2").Build(),
			},
			expectedClusterNames: []string{"cluster1", "cluster2"},
		},
		{
			name: "select clusters from a bound clusterset",
			placement: testinghelpers.NewPlacement(placementNamespace, placementName).
				WithClusterSets("clusterset1").Build(),
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet("clusterset1"),
				testinghelpers.NewClusterSet("clusterset2"),
				testinghelpers.NewClusterSetBinding(placementNamespace, "clusterset1"),
				testinghelpers.NewClusterSetBinding(placementNamespace, "clusterset2"),
				testinghelpers.NewManagedCluster("cluster1").WithLabel(clusterSetLabel, "clusterset1").Build(),
				testinghelpers.NewManagedCluster("cluster2").WithLabel(clusterSetLabel, "clusterset2").Build(),
			},
			expectedClusterNames: []string{"cluster1"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.initObjs = append(c.initObjs, c.placement)
			clusterClient := clusterfake.NewSimpleClientset(c.initObjs...)
			clusterInformerFactory := testinghelpers.NewClusterInformerFactory(clusterClient, c.initObjs...)

			ctrl := &schedulingController{
				clusterLister:           clusterInformerFactory.Cluster().V1().ManagedClusters().Lister(),
				clusterSetLister:        clusterInformerFactory.Cluster().V1alpha1().ManagedClusterSets().Lister(),
				clusterSetBindingLister: clusterInformerFactory.Cluster().V1alpha1().ManagedClusterSetBindings().Lister(),
			}
			bindings, err := ctrl.getManagedClusterSetBindings(c.placement)
			if err != nil {
				t.Errorf("unexpected err: %v", err)
			}

			clusters, err := ctrl.getAvailableClusters(c.placement, bindings)
			if err != nil {
				t.Errorf("unexpected err: %v", err)
			}

			expectedClusterNames := sets.NewString(c.expectedClusterNames...)
			if len(clusters) != expectedClusterNames.Len() {
				t.Errorf("expected %d clusters but got %d", expectedClusterNames.Len(), len(clusters))
			}
			for _, cluster := range clusters {
				expectedClusterNames.Delete(cluster.Name)
			}
			if expectedClusterNames.Len() > 0 {
				t.Errorf("expected clusters not selected: %s", strings.Join(expectedClusterNames.List(), ","))
			}
		})
	}
}
