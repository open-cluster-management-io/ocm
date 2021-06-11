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
				feasibleClusters:     3,
				scheduledDecisions:   3,
				unscheduledDecisions: 0,
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
				feasibleClusters:     3,
				scheduledDecisions:   3,
				unscheduledDecisions: 1,
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
				feasibleClusters:     0,
				scheduledDecisions:   0,
				unscheduledDecisions: 0,
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
				feasibleClusters:     3,
				scheduledDecisions:   3,
				unscheduledDecisions: 0,
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

func TestGetValidManagedClusterSetBindings(t *testing.T) {
	placementNamespace := "ns1"
	cases := []struct {
		name                           string
		initObjs                       []runtime.Object
		expectedClusterSetBindingNames []string
	}{
		{
			name: "no bound clusterset",
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet("clusterset1"),
			},
		},
		{
			name: "invalid binding",
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSetBinding(placementNamespace, "clusterset1"),
			},
		},
		{
			name: "valid binding",
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet("clusterset1"),
				testinghelpers.NewClusterSetBinding(placementNamespace, "clusterset1"),
			},
			expectedClusterSetBindingNames: []string{"clusterset1"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterClient := clusterfake.NewSimpleClientset(c.initObjs...)
			clusterInformerFactory := testinghelpers.NewClusterInformerFactory(clusterClient, c.initObjs...)

			ctrl := &schedulingController{
				clusterSetLister:        clusterInformerFactory.Cluster().V1alpha1().ManagedClusterSets().Lister(),
				clusterSetBindingLister: clusterInformerFactory.Cluster().V1alpha1().ManagedClusterSetBindings().Lister(),
			}
			bindings, err := ctrl.getValidManagedClusterSetBindings(placementNamespace)
			if err != nil {
				t.Errorf("unexpected err: %v", err)
			}

			expectedBindingNames := sets.NewString(c.expectedClusterSetBindingNames...)
			if len(bindings) != expectedBindingNames.Len() {
				t.Errorf("expected %d bindings but got %d", expectedBindingNames.Len(), len(bindings))
			}
			for _, binding := range bindings {
				expectedBindingNames.Delete(binding.Name)
			}
			if expectedBindingNames.Len() > 0 {
				t.Errorf("expected bindings: %s", strings.Join(expectedBindingNames.List(), ","))
			}
		})
	}
}

func TestGetAvailableClusters(t *testing.T) {
	placementNamespace := "ns1"

	cases := []struct {
		name                 string
		clusterSetNames      []string
		initObjs             []runtime.Object
		expectedClusterNames []string
	}{
		{
			name: "no clusterset",
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet("clusterset1"),
				testinghelpers.NewClusterSetBinding(placementNamespace, "clusterset1"),
			},
		},
		{
			name:            "select clusters from clustersets",
			clusterSetNames: []string{"clusterset1", "clusterset2"},
			initObjs: []runtime.Object{
				testinghelpers.NewManagedCluster("cluster1").WithLabel(clusterSetLabel, "clusterset1").Build(),
				testinghelpers.NewManagedCluster("cluster2").WithLabel(clusterSetLabel, "clusterset2").Build(),
			},
			expectedClusterNames: []string{"cluster1", "cluster2"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterClient := clusterfake.NewSimpleClientset(c.initObjs...)
			clusterInformerFactory := testinghelpers.NewClusterInformerFactory(clusterClient, c.initObjs...)

			ctrl := &schedulingController{
				clusterLister: clusterInformerFactory.Cluster().V1().ManagedClusters().Lister(),
			}

			clusters, err := ctrl.getAvailableClusters(c.clusterSetNames)
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

func TestNewSatisfiedCondition(t *testing.T) {
	cases := []struct {
		name                      string
		clusterSetsInSpec         []string
		eligibleClusterSets       []string
		numOfBindings             int
		numOfAvailableClusters    int
		numOfFeasibleClusters     int
		numOfUnscheduledDecisions int
		expectedStatus            metav1.ConditionStatus
		expectedReason            string
	}{
		{
			name:                      "NoManagedClusterSetBindings",
			numOfBindings:             0,
			numOfUnscheduledDecisions: 5,
			expectedStatus:            metav1.ConditionFalse,
			expectedReason:            "NoManagedClusterSetBindings",
		},
		{
			name:                      "NoIntersection",
			clusterSetsInSpec:         []string{"clusterset1"},
			numOfBindings:             1,
			numOfAvailableClusters:    0,
			numOfFeasibleClusters:     0,
			numOfUnscheduledDecisions: 0,
			expectedStatus:            metav1.ConditionFalse,
			expectedReason:            "NoIntersection",
		},
		{
			name:                      "AllManagedClusterSetsEmpty",
			eligibleClusterSets:       []string{"clusterset1"},
			numOfBindings:             1,
			numOfAvailableClusters:    0,
			numOfFeasibleClusters:     0,
			numOfUnscheduledDecisions: 0,
			expectedStatus:            metav1.ConditionFalse,
			expectedReason:            "AllManagedClusterSetsEmpty",
		},
		{
			name:                      "NoManagedClusterMatched",
			eligibleClusterSets:       []string{"clusterset1"},
			numOfBindings:             1,
			numOfAvailableClusters:    1,
			numOfFeasibleClusters:     0,
			numOfUnscheduledDecisions: 0,
			expectedStatus:            metav1.ConditionFalse,
			expectedReason:            "NoManagedClusterMatched",
		},
		{
			name:                      "AllDecisionsScheduled",
			eligibleClusterSets:       []string{"clusterset1"},
			numOfBindings:             1,
			numOfAvailableClusters:    1,
			numOfFeasibleClusters:     1,
			numOfUnscheduledDecisions: 0,
			expectedStatus:            metav1.ConditionTrue,
			expectedReason:            "AllDecisionsScheduled",
		},
		{
			name:                      "NotAllDecisionsScheduled",
			eligibleClusterSets:       []string{"clusterset1"},
			numOfBindings:             1,
			numOfAvailableClusters:    1,
			numOfFeasibleClusters:     1,
			numOfUnscheduledDecisions: 1,
			expectedStatus:            metav1.ConditionFalse,
			expectedReason:            "NotAllDecisionsScheduled",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			condition := newSatisfiedCondition(
				c.clusterSetsInSpec,
				c.eligibleClusterSets,
				c.numOfBindings,
				c.numOfAvailableClusters,
				c.numOfFeasibleClusters,
				c.numOfUnscheduledDecisions,
			)

			if condition.Status != c.expectedStatus {
				t.Errorf("expected status %q but got %q", c.expectedStatus, condition.Status)
			}
			if condition.Reason != c.expectedReason {
				t.Errorf("expected reason %q but got %q", c.expectedReason, condition.Reason)
			}
		})
	}
}
