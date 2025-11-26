package scheduling

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/sets"
	clienttesting "k8s.io/client-go/testing"
	kevents "k8s.io/client-go/tools/events"
	"k8s.io/utils/clock"

	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	clusterapiv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	clusterapiv1beta2 "open-cluster-management.io/api/cluster/v1beta2"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	"open-cluster-management.io/ocm/pkg/placement/controllers/framework"
	"open-cluster-management.io/ocm/pkg/placement/controllers/metrics"
	testinghelpers "open-cluster-management.io/ocm/pkg/placement/helpers/testing"
	"open-cluster-management.io/ocm/test/integration/util"
)

type testScheduler struct {
	result ScheduleResult
}

const (
	placementNamespace = "ns1"
	placementName      = "placement1"
)

func (s *testScheduler) Schedule(ctx context.Context,
	placement *clusterapiv1beta1.Placement,
	clusters []*clusterapiv1.ManagedCluster,
) (ScheduleResult, *framework.Status) {
	return s.result, nil
}

func TestSchedulingController_sync(t *testing.T) {
	cases := []struct {
		name            string
		placement       *clusterapiv1beta1.Placement
		initObjs        []runtime.Object
		scheduleResult  *scheduleResult
		validateActions func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:      "placement satisfied",
			placement: testinghelpers.NewPlacement(placementNamespace, placementName).Build(),
			scheduleResult: &scheduleResult{
				feasibleClusters: []*clusterapiv1.ManagedCluster{
					testinghelpers.NewManagedCluster("cluster1").Build(),
					testinghelpers.NewManagedCluster("cluster2").Build(),
					testinghelpers.NewManagedCluster("cluster3").Build(),
				},
				scheduledDecisions: []*clusterapiv1.ManagedCluster{
					testinghelpers.NewManagedCluster("cluster1").Build(),
					testinghelpers.NewManagedCluster("cluster2").Build(),
					testinghelpers.NewManagedCluster("cluster3").Build(),
				},
				unscheduledDecisions: 0,
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				// check if PlacementDecision has been updated
				testingcommon.AssertActions(t, actions, "create", "patch")
				// check if Placement has been updated
				placement := &clusterapiv1beta1.Placement{}
				patchData := actions[1].(clienttesting.PatchActionImpl).Patch
				err := json.Unmarshal(patchData, placement)
				if err != nil {
					t.Fatal(err)
				}

				if placement.Status.NumberOfSelectedClusters != int32(3) {
					t.Errorf("expecte %d cluster selected, but got %d", 3, placement.Status.NumberOfSelectedClusters)
				}
				expectDecisionGroups := []clusterapiv1beta1.DecisionGroupStatus{
					{
						DecisionGroupIndex: 0,
						DecisionGroupName:  "",
						Decisions:          []string{testinghelpers.PlacementDecisionName(placementName, 1)},
						ClustersCount:      3,
					},
				}
				if !reflect.DeepEqual(placement.Status.DecisionGroups, expectDecisionGroups) {
					t.Errorf("expect %v cluster decision gorups, but got %v", expectDecisionGroups, placement.Status.DecisionGroups)
				}

				util.HasCondition(
					placement.Status.Conditions,
					clusterapiv1beta1.PlacementConditionSatisfied,
					"AllDecisionsScheduled",
					metav1.ConditionTrue,
				)
			},
		},
		{
			name:      "placement unsatisfied",
			placement: testinghelpers.NewPlacement(placementNamespace, placementName).Build(),
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet("clusterset1").Build(),
				testinghelpers.NewClusterSetBinding(placementNamespace, "clusterset1"),
				testinghelpers.NewManagedCluster("cluster1").WithLabel(
					clusterapiv1beta2.ClusterSetLabel, "clusterset1").Build(),
			},
			scheduleResult: &scheduleResult{
				feasibleClusters: []*clusterapiv1.ManagedCluster{
					testinghelpers.NewManagedCluster("cluster1").Build(),
					testinghelpers.NewManagedCluster("cluster2").Build(),
					testinghelpers.NewManagedCluster("cluster3").Build(),
				},
				scheduledDecisions: []*clusterapiv1.ManagedCluster{
					testinghelpers.NewManagedCluster("cluster1").Build(),
					testinghelpers.NewManagedCluster("cluster2").Build(),
					testinghelpers.NewManagedCluster("cluster3").Build(),
				},
				unscheduledDecisions: 1,
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				// check if PlacementDecision has been updated
				testingcommon.AssertActions(t, actions, "create", "patch")
				// check if Placement has been updated
				placement := &clusterapiv1beta1.Placement{}
				patchData := actions[1].(clienttesting.PatchActionImpl).Patch
				err := json.Unmarshal(patchData, placement)
				if err != nil {
					t.Fatal(err)
				}

				if placement.Status.NumberOfSelectedClusters != int32(3) {
					t.Errorf("expecte %d cluster selected, but got %d", 3, placement.Status.NumberOfSelectedClusters)
				}
				if len(placement.Status.DecisionGroups) != 1 || placement.Status.DecisionGroups[0].ClustersCount != 3 {
					t.Errorf("expecte %d cluster decision gorups, but got %d", 1, len(placement.Status.DecisionGroups))
				}
				util.HasCondition(
					placement.Status.Conditions,
					clusterapiv1beta1.PlacementConditionSatisfied,
					"NotAllDecisionsScheduled",
					metav1.ConditionFalse,
				)
			},
		},
		{
			name: "placement with canary group strategy",
			placement: testinghelpers.NewPlacement(placementNamespace, placementName).WithGroupStrategy(clusterapiv1beta1.GroupStrategy{
				ClustersPerDecisionGroup: intstr.FromInt(1),
				DecisionGroups: []clusterapiv1beta1.DecisionGroup{
					{
						GroupName: "canary",
						ClusterSelector: clusterapiv1beta1.GroupClusterSelector{
							LabelSelector: metav1.LabelSelector{MatchLabels: map[string]string{"cloud": "Azure"}},
						},
					},
				}}).Build(),
			scheduleResult: &scheduleResult{
				feasibleClusters: []*clusterapiv1.ManagedCluster{
					testinghelpers.NewManagedCluster("cluster1").Build(),
					testinghelpers.NewManagedCluster("cluster2").Build(),
					testinghelpers.NewManagedCluster("cluster3").Build(),
					testinghelpers.NewManagedCluster("cluster4").Build(),
				},
				scheduledDecisions: []*clusterapiv1.ManagedCluster{
					testinghelpers.NewManagedCluster("cluster1").WithLabel("cloud", "Amazon").Build(),
					testinghelpers.NewManagedCluster("cluster2").WithLabel("cloud", "Amazon").Build(),
					testinghelpers.NewManagedCluster("cluster3").WithLabel("cloud", "Azure").Build(),
					testinghelpers.NewManagedCluster("cluster4").WithLabel("cloud", "Azure").Build(),
				},
				unscheduledDecisions: 0,
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "create", "create", "create", "create", "patch")
				// check if Placement has been updated
				placement := &clusterapiv1beta1.Placement{}
				patchData := actions[4].(clienttesting.PatchActionImpl).Patch
				err := json.Unmarshal(patchData, placement)
				if err != nil {
					t.Fatal(err)
				}

				if placement.Status.NumberOfSelectedClusters != int32(4) {
					t.Errorf("expecte %d cluster selected, but got %d", 4, placement.Status.NumberOfSelectedClusters)
				}

				expectDecisionGroups := []clusterapiv1beta1.DecisionGroupStatus{
					{
						DecisionGroupIndex: 0,
						DecisionGroupName:  "canary",
						Decisions:          []string{testinghelpers.PlacementDecisionName(placementName, 1)},
						ClustersCount:      1,
					},
					{
						DecisionGroupIndex: 1,
						DecisionGroupName:  "canary",
						Decisions:          []string{testinghelpers.PlacementDecisionName(placementName, 2)},
						ClustersCount:      1,
					},
					{
						DecisionGroupIndex: 2,
						DecisionGroupName:  "",
						Decisions:          []string{testinghelpers.PlacementDecisionName(placementName, 3)},
						ClustersCount:      1,
					},
					{
						DecisionGroupIndex: 3,
						DecisionGroupName:  "",
						Decisions:          []string{testinghelpers.PlacementDecisionName(placementName, 4)},
						ClustersCount:      1,
					},
				}
				if !reflect.DeepEqual(placement.Status.DecisionGroups, expectDecisionGroups) {
					t.Errorf("expect %v cluster decision gorups, but got %v", expectDecisionGroups, placement.Status.DecisionGroups)
				}

				util.HasCondition(
					placement.Status.Conditions,
					clusterapiv1beta1.PlacementConditionSatisfied,
					"AllDecisionsScheduled",
					metav1.ConditionTrue,
				)
			},
		},
		{
			name: "placement with multiple group strategy",
			placement: testinghelpers.NewPlacement(placementNamespace, placementName).WithGroupStrategy(clusterapiv1beta1.GroupStrategy{
				DecisionGroups: []clusterapiv1beta1.DecisionGroup{
					{
						GroupName: "group1",
						ClusterSelector: clusterapiv1beta1.GroupClusterSelector{
							LabelSelector: metav1.LabelSelector{MatchLabels: map[string]string{"cloud": "Amazon"}},
						},
					},
					{
						GroupName: "group2",
						ClusterSelector: clusterapiv1beta1.GroupClusterSelector{
							LabelSelector: metav1.LabelSelector{MatchLabels: map[string]string{"cloud": "Azure"}},
						},
					},
				}}).Build(),
			scheduleResult: &scheduleResult{
				feasibleClusters: []*clusterapiv1.ManagedCluster{
					testinghelpers.NewManagedCluster("cluster1").Build(),
					testinghelpers.NewManagedCluster("cluster2").Build(),
					testinghelpers.NewManagedCluster("cluster3").Build(),
				},
				scheduledDecisions: []*clusterapiv1.ManagedCluster{
					testinghelpers.NewManagedCluster("cluster1").WithLabel("cloud", "Amazon").Build(),
					testinghelpers.NewManagedCluster("cluster2").WithLabel("cloud", "Amazon").Build(),
					testinghelpers.NewManagedCluster("cluster3").WithLabel("cloud", "Azure").Build(),
				},
				unscheduledDecisions: 0,
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "create", "create", "patch")
				// check if Placement has been updated
				placement := &clusterapiv1beta1.Placement{}
				patchData := actions[2].(clienttesting.PatchActionImpl).Patch
				err := json.Unmarshal(patchData, placement)
				if err != nil {
					t.Fatal(err)
				}

				if placement.Status.NumberOfSelectedClusters != int32(3) {
					t.Errorf("expecte %d cluster selected, but got %d", 3, placement.Status.NumberOfSelectedClusters)
				}

				expectDecisionGroups := []clusterapiv1beta1.DecisionGroupStatus{
					{
						DecisionGroupIndex: 0,
						DecisionGroupName:  "group1",
						Decisions:          []string{testinghelpers.PlacementDecisionName(placementName, 1)},
						ClustersCount:      2,
					},
					{
						DecisionGroupIndex: 1,
						DecisionGroupName:  "group2",
						Decisions:          []string{testinghelpers.PlacementDecisionName(placementName, 2)},
						ClustersCount:      1,
					},
				}
				if !reflect.DeepEqual(placement.Status.DecisionGroups, expectDecisionGroups) {
					t.Errorf("expect %v cluster decision gorups, but got %v", expectDecisionGroups, placement.Status.DecisionGroups)
				}

				util.HasCondition(
					placement.Status.Conditions,
					clusterapiv1beta1.PlacementConditionSatisfied,
					"AllDecisionsScheduled",
					metav1.ConditionTrue,
				)
			},
		},
		{
			name: "placement with only cluster per decision group",
			placement: testinghelpers.NewPlacement(placementNamespace, placementName).WithGroupStrategy(clusterapiv1beta1.GroupStrategy{
				ClustersPerDecisionGroup: intstr.FromString("25%"),
			}).Build(),
			scheduleResult: &scheduleResult{
				feasibleClusters: []*clusterapiv1.ManagedCluster{
					testinghelpers.NewManagedCluster("cluster1").Build(),
					testinghelpers.NewManagedCluster("cluster2").Build(),
					testinghelpers.NewManagedCluster("cluster3").Build(),
				},
				scheduledDecisions: []*clusterapiv1.ManagedCluster{
					testinghelpers.NewManagedCluster("cluster1").WithLabel("cloud", "Amazon").Build(),
					testinghelpers.NewManagedCluster("cluster2").WithLabel("cloud", "Amazon").Build(),
					testinghelpers.NewManagedCluster("cluster3").WithLabel("cloud", "Azure").Build(),
				},
				unscheduledDecisions: 0,
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "create", "create", "create", "patch")
				// check if Placement has been updated
				placement := &clusterapiv1beta1.Placement{}
				patchData := actions[3].(clienttesting.PatchActionImpl).Patch
				err := json.Unmarshal(patchData, placement)
				if err != nil {
					t.Fatal(err)
				}

				if placement.Status.NumberOfSelectedClusters != int32(3) {
					t.Errorf("expecte %d cluster selected, but got %d", 3, placement.Status.NumberOfSelectedClusters)
				}

				expectDecisionGroups := []clusterapiv1beta1.DecisionGroupStatus{
					{
						DecisionGroupIndex: 0,
						DecisionGroupName:  "",
						Decisions:          []string{testinghelpers.PlacementDecisionName(placementName, 1)},
						ClustersCount:      1,
					},
					{
						DecisionGroupIndex: 1,
						DecisionGroupName:  "",
						Decisions:          []string{testinghelpers.PlacementDecisionName(placementName, 2)},
						ClustersCount:      1,
					},
					{
						DecisionGroupIndex: 2,
						DecisionGroupName:  "",
						Decisions:          []string{testinghelpers.PlacementDecisionName(placementName, 3)},
						ClustersCount:      1,
					},
				}
				if !reflect.DeepEqual(placement.Status.DecisionGroups, expectDecisionGroups) {
					t.Errorf("expect %v cluster decision gorups, but got %v", expectDecisionGroups, placement.Status.DecisionGroups)
				}

				util.HasCondition(
					placement.Status.Conditions,
					clusterapiv1beta1.PlacementConditionSatisfied,
					"AllDecisionsScheduled",
					metav1.ConditionTrue,
				)
			},
		},
		{
			name:      "placement missing managedclustersetbindings",
			placement: testinghelpers.NewPlacement(placementNamespace, placementName).Build(),
			scheduleResult: &scheduleResult{
				feasibleClusters:     []*clusterapiv1.ManagedCluster{},
				scheduledDecisions:   []*clusterapiv1.ManagedCluster{},
				unscheduledDecisions: 0,
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				// check if PlacementDecision has been updated
				testingcommon.AssertActions(t, actions, "create", "patch")
				// check if empty PlacementDecision has been created
				actual := actions[0].(clienttesting.CreateActionImpl).Object
				placementDecision, ok := actual.(*clusterapiv1beta1.PlacementDecision)
				if !ok {
					t.Errorf("expected PlacementDecision was created")
				}

				if len(placementDecision.Status.Decisions) != 0 {
					t.Errorf("expecte %d cluster selected, but got %d", 0, len(placementDecision.Status.Decisions))
				}
				// check if Placement has been updated
				placement := &clusterapiv1beta1.Placement{}
				patchData := actions[1].(clienttesting.PatchActionImpl).Patch
				err := json.Unmarshal(patchData, placement)
				if err != nil {
					t.Fatal(err)
				}

				if placement.Status.NumberOfSelectedClusters != int32(0) {
					t.Errorf("expecte %d cluster selected, but got %d", 0, placement.Status.NumberOfSelectedClusters)
				}
				if len(placement.Status.DecisionGroups) != 1 || placement.Status.DecisionGroups[0].ClustersCount != 0 {
					t.Errorf("expecte %d cluster decision gorups, but got %d", 1, len(placement.Status.DecisionGroups))
				}
				util.HasCondition(
					placement.Status.Conditions,
					clusterapiv1beta1.PlacementConditionSatisfied,
					"NoManagedClusterSetBindings",
					metav1.ConditionFalse,
				)
			},
		},
		{
			name:      "placement all managedclustersets empty ",
			placement: testinghelpers.NewPlacement(placementNamespace, placementName).Build(),
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet("clusterset1").Build(),
				testinghelpers.NewClusterSetBinding(placementNamespace, "clusterset1"),
			},
			scheduleResult: &scheduleResult{
				feasibleClusters:     []*clusterapiv1.ManagedCluster{},
				scheduledDecisions:   []*clusterapiv1.ManagedCluster{},
				unscheduledDecisions: 0,
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				// check if PlacementDecision has been updated
				testingcommon.AssertActions(t, actions, "create", "patch")
				// check if empty PlacementDecision has been created
				actual := actions[0].(clienttesting.CreateActionImpl).Object
				placementDecision, ok := actual.(*clusterapiv1beta1.PlacementDecision)
				if !ok {
					t.Errorf("expected PlacementDecision was created")
				}

				if len(placementDecision.Status.Decisions) != 0 {
					t.Errorf("expecte %d cluster selected, but got %d", 0, len(placementDecision.Status.Decisions))
				}
				// check if Placement has been updated
				placement := &clusterapiv1beta1.Placement{}
				patchData := actions[1].(clienttesting.PatchActionImpl).Patch
				err := json.Unmarshal(patchData, placement)
				if err != nil {
					t.Fatal(err)
				}

				if placement.Status.NumberOfSelectedClusters != int32(0) {
					t.Errorf("expecte %d cluster selected, but got %d", 0, placement.Status.NumberOfSelectedClusters)
				}
				if len(placement.Status.DecisionGroups) != 1 || placement.Status.DecisionGroups[0].ClustersCount != 0 {
					t.Errorf("expecte %d cluster decision gorups, but got %d", 1, len(placement.Status.DecisionGroups))
				}
				util.HasCondition(
					placement.Status.Conditions,
					clusterapiv1beta1.PlacementConditionSatisfied,
					"AllManagedClusterSetsEmpty",
					metav1.ConditionFalse,
				)
			},
		},
		{
			name:      "placement no cluster matches",
			placement: testinghelpers.NewPlacement(placementNamespace, placementName).Build(),
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet("clusterset1").Build(),
				testinghelpers.NewClusterSetBinding(placementNamespace, "clusterset1"),
				testinghelpers.NewManagedCluster("cluster1").WithLabel(clusterapiv1beta2.ClusterSetLabel, "clusterset1").Build(),
			},
			scheduleResult: &scheduleResult{
				feasibleClusters: []*clusterapiv1.ManagedCluster{
					testinghelpers.NewManagedCluster("cluster1").Build(),
				},
				scheduledDecisions:   []*clusterapiv1.ManagedCluster{},
				unscheduledDecisions: 0,
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				// check if PlacementDecision has been updated
				testingcommon.AssertActions(t, actions, "create", "patch")
				// check if empty PlacementDecision has been created
				actual := actions[0].(clienttesting.CreateActionImpl).Object
				placementDecision, ok := actual.(*clusterapiv1beta1.PlacementDecision)
				if !ok {
					t.Errorf("expected PlacementDecision was created")
				}

				if len(placementDecision.Status.Decisions) != 0 {
					t.Errorf("expecte %d cluster selected, but got %d", 0, len(placementDecision.Status.Decisions))
				}
				// check if Placement has been updated
				placement := &clusterapiv1beta1.Placement{}
				patchData := actions[1].(clienttesting.PatchActionImpl).Patch
				err := json.Unmarshal(patchData, placement)
				if err != nil {
					t.Fatal(err)
				}

				if placement.Status.NumberOfSelectedClusters != int32(0) {
					t.Errorf("expecte %d cluster selected, but got %d", 0, placement.Status.NumberOfSelectedClusters)
				}
				if len(placement.Status.DecisionGroups) != 1 || placement.Status.DecisionGroups[0].ClustersCount != 0 {
					t.Errorf("expecte %d cluster decision gorups, but got %d", 1, len(placement.Status.DecisionGroups))
				}
				util.HasCondition(
					placement.Status.Conditions,
					clusterapiv1beta1.PlacementConditionSatisfied,
					"NoManagedClusterMatched",
					metav1.ConditionFalse,
				)
			},
		},
		{
			name: "placement status not changed",
			placement: testinghelpers.NewPlacement(placementNamespace, placementName).
				WithNumOfSelectedClusters(3, placementName).WithSatisfiedCondition(3, 0).WithMisconfiguredCondition(metav1.ConditionFalse).Build(),
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet("clusterset1").Build(),
				testinghelpers.NewClusterSetBinding(placementNamespace, "clusterset1"),
				testinghelpers.NewManagedCluster("cluster1").WithLabel(clusterapiv1beta2.ClusterSetLabel, "clusterset1").Build(),
				testinghelpers.NewPlacementDecision(placementNamespace, testinghelpers.PlacementDecisionName(placementName, 1)).
					WithLabel(clusterapiv1beta1.PlacementLabel, placementName).
					WithLabel(clusterapiv1beta1.DecisionGroupNameLabel, "").
					WithLabel(clusterapiv1beta1.DecisionGroupIndexLabel, "0").
					WithDecisions("cluster1", "cluster2", "cluster3").Build(),
			},
			scheduleResult: &scheduleResult{
				feasibleClusters: []*clusterapiv1.ManagedCluster{
					testinghelpers.NewManagedCluster("cluster1").Build(),
					testinghelpers.NewManagedCluster("cluster2").Build(),
					testinghelpers.NewManagedCluster("cluster3").Build(),
				},
				scheduledDecisions: []*clusterapiv1.ManagedCluster{
					testinghelpers.NewManagedCluster("cluster1").Build(),
					testinghelpers.NewManagedCluster("cluster2").Build(),
					testinghelpers.NewManagedCluster("cluster3").Build(),
				},
				unscheduledDecisions: 0,
			},
			validateActions: testingcommon.AssertNoActions,
		},
		{
			name: "placement schedule controller is disabled",
			placement: testinghelpers.NewPlacementWithAnnotations(placementNamespace, placementName,
				map[string]string{
					clusterapiv1beta1.PlacementDisableAnnotation: "true",
				}).Build(),
			scheduleResult: &scheduleResult{
				feasibleClusters: []*clusterapiv1.ManagedCluster{
					testinghelpers.NewManagedCluster("cluster1").Build(),
					testinghelpers.NewManagedCluster("cluster2").Build(),
					testinghelpers.NewManagedCluster("cluster3").Build(),
				},
				scheduledDecisions: []*clusterapiv1.ManagedCluster{},
			},
			validateActions: testingcommon.AssertNoActions,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.initObjs = append(c.initObjs, c.placement)
			clusterClient := clusterfake.NewSimpleClientset(c.initObjs...)
			clusterInformerFactory := newClusterInformerFactory(t, clusterClient, c.initObjs...)
			s := &testScheduler{result: c.scheduleResult}

			ctrl := schedulingController{
				clusterClient:           clusterClient,
				clusterLister:           clusterInformerFactory.Cluster().V1().ManagedClusters().Lister(),
				clusterSetLister:        clusterInformerFactory.Cluster().V1beta2().ManagedClusterSets().Lister(),
				clusterSetBindingLister: clusterInformerFactory.Cluster().V1beta2().ManagedClusterSetBindings().Lister(),
				placementLister:         clusterInformerFactory.Cluster().V1beta1().Placements().Lister(),
				placementDecisionLister: clusterInformerFactory.Cluster().V1beta1().PlacementDecisions().Lister(),
				scheduler:               s,
				eventsRecorder:          kevents.NewFakeRecorder(100),
				metricsRecorder:         metrics.NewScheduleMetrics(clock.RealClock{}),
			}

			key := c.placement.Namespace + "/" + c.placement.Name
			sysCtx := testingcommon.NewFakeSyncContext(t, key)
			syncErr := ctrl.sync(context.TODO(), sysCtx, key)
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
				testinghelpers.NewClusterSet("clusterset1").Build(),
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
				testinghelpers.NewClusterSet("clusterset1").Build(),
				testinghelpers.NewClusterSetBinding(placementNamespace, "clusterset1"),
			},
			expectedClusterSetBindingNames: []string{"clusterset1"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterClient := clusterfake.NewSimpleClientset(c.initObjs...)
			clusterInformerFactory := newClusterInformerFactory(t, clusterClient, c.initObjs...)

			ctrl := &schedulingController{
				clusterSetLister:        clusterInformerFactory.Cluster().V1beta2().ManagedClusterSets().Lister(),
				clusterSetBindingLister: clusterInformerFactory.Cluster().V1beta2().ManagedClusterSetBindings().Lister(),
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

func TestGetValidManagedClusterSets(t *testing.T) {
	placementNamespace := "ns1"
	cases := []struct {
		name                    string
		placement               *clusterapiv1beta1.Placement
		bindings                []*clusterapiv1beta2.ManagedClusterSetBinding
		initObjs                []runtime.Object
		expectedClusterSetNames []string
	}{
		{
			name:      "no clusterset bindings",
			placement: testinghelpers.NewPlacement("ns1", "test").WithClusterSets("clusterset1", "clusterset2").Build(),
			bindings:  []*clusterapiv1beta2.ManagedClusterSetBinding{},
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet("clusterset2").Build(),
				testinghelpers.NewClusterSet("clusterset3").Build(),
			},
			expectedClusterSetNames: []string{},
		},
		{
			name:      "no placement clusterset",
			placement: testinghelpers.NewPlacement("ns1", "test").Build(),
			bindings: []*clusterapiv1beta2.ManagedClusterSetBinding{
				testinghelpers.NewClusterSetBinding(placementNamespace, "clusterset1"),
				testinghelpers.NewClusterSetBinding(placementNamespace, "clusterset2"),
			},
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet("clusterset1").Build(),
				testinghelpers.NewClusterSet("clusterset2").Build(),
			},
			expectedClusterSetNames: []string{"clusterset1", "clusterset2"},
		},
		{
			name:      "intersection of clusterset bindings and placement clusterset",
			placement: testinghelpers.NewPlacement("ns1", "test").WithClusterSets("clusterset1", "clusterset2").Build(),
			bindings: []*clusterapiv1beta2.ManagedClusterSetBinding{
				testinghelpers.NewClusterSetBinding(placementNamespace, "clusterset2"),
				testinghelpers.NewClusterSetBinding(placementNamespace, "clusterset3"),
			},
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet("clusterset2").Build(),
				testinghelpers.NewClusterSet("clusterset3").Build(),
			},
			expectedClusterSetNames: []string{"clusterset2"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterClient := clusterfake.NewSimpleClientset(c.initObjs...)
			clusterInformerFactory := newClusterInformerFactory(t, clusterClient, c.initObjs...)

			ctrl := &schedulingController{
				clusterSetLister:        clusterInformerFactory.Cluster().V1beta2().ManagedClusterSets().Lister(),
				clusterSetBindingLister: clusterInformerFactory.Cluster().V1beta2().ManagedClusterSetBindings().Lister(),
			}
			actualClusterSetNames := ctrl.getEligibleClusterSets(c.placement, c.bindings)

			expectedClusterSetNames := sets.NewString(c.expectedClusterSetNames...)
			if len(actualClusterSetNames) != expectedClusterSetNames.Len() {
				t.Errorf("expected %d bindings but got %d", expectedClusterSetNames.Len(), len(actualClusterSetNames))
			}
			for _, name := range actualClusterSetNames {
				expectedClusterSetNames.Delete(name)
			}
			if expectedClusterSetNames.Len() > 0 {
				t.Errorf("expected names: %s", strings.Join(expectedClusterSetNames.List(), ","))
			}
		})
	}
}
func TestGetAvailableClusters(t *testing.T) {
	cases := []struct {
		name                 string
		clusterSetNames      []string
		initObjs             []runtime.Object
		expectedClusterNames []string
	}{
		{
			name: "no clusterset",
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet("clusterset1").Build(),
			},
		},
		{
			name:            "select clusters from clustersets",
			clusterSetNames: []string{"clusterset1", "clusterset2"},
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet("clusterset1").Build(),
				testinghelpers.NewClusterSet("clusterset2").Build(),
				testinghelpers.NewManagedCluster("cluster1").WithLabel(clusterapiv1beta2.ClusterSetLabel, "clusterset1").Build(),
				testinghelpers.NewManagedCluster("cluster2").WithLabel(clusterapiv1beta2.ClusterSetLabel, "clusterset2").Build(),
			},
			expectedClusterNames: []string{"cluster1", "cluster2"},
		},
		{
			name:            "exclude clusters with deletion timestamp",
			clusterSetNames: []string{"clusterset1", "clusterset2"},
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet("clusterset1").Build(),
				testinghelpers.NewClusterSet("clusterset2").Build(),
				testinghelpers.NewManagedCluster("cluster1").WithLabel(clusterapiv1beta2.ClusterSetLabel, "clusterset1").Build(),
				testinghelpers.NewManagedCluster("cluster2").
					WithLabel(clusterapiv1beta2.ClusterSetLabel, "clusterset2").
					WithDeletionTimestamp().Build(),
			},
			expectedClusterNames: []string{"cluster1"},
		},
		{
			name:            "clusterset has no ClusterSelector",
			clusterSetNames: []string{"clusterset1"},
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet("clusterset1").Build(),
				testinghelpers.NewManagedCluster("cluster1").WithLabel(clusterapiv1beta2.ClusterSetLabel, "clusterset1").Build(),
			},
			expectedClusterNames: []string{"cluster1"},
		},
		{
			name:            "clusterset has default ClusterSelector",
			clusterSetNames: []string{"clusterset1"},
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet("clusterset1").WithClusterSelector(clusterapiv1beta2.ManagedClusterSelector{}).Build(),
				testinghelpers.NewManagedCluster("cluster1").WithLabel(clusterapiv1beta2.ClusterSetLabel, "clusterset1").Build(),
			},
			expectedClusterNames: []string{"cluster1"},
		},
		{
			name:            "clusterset has Legacy set label ClusterSelector",
			clusterSetNames: []string{"clusterset1"},
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet("clusterset1").WithClusterSelector(clusterapiv1beta2.ManagedClusterSelector{
					SelectorType: clusterapiv1beta2.ExclusiveClusterSetLabel,
				}).Build(),
				testinghelpers.NewManagedCluster("cluster1").WithLabel(clusterapiv1beta2.ClusterSetLabel, "clusterset1").Build(),
			},
			expectedClusterNames: []string{"cluster1"},
		},
		{
			name:            "clusterset has labelSelector type ClusterSelector",
			clusterSetNames: []string{"clusterset1"},
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet("clusterset1").WithClusterSelector(clusterapiv1beta2.ManagedClusterSelector{
					SelectorType: clusterapiv1beta2.LabelSelector,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"vendor": "openShift",
						},
					},
				}).Build(),
				testinghelpers.NewManagedCluster("cluster1").WithLabel(clusterapiv1beta2.ClusterSetLabel, "clusterset1").Build(),
				testinghelpers.NewManagedCluster("cluster2").WithLabel("vendor", "openShift").Build(),
			},
			expectedClusterNames: []string{"cluster2"},
		},
		{
			name:            "clusterset has labelSelector type ClusterSelector(select everything)",
			clusterSetNames: []string{"clusterset1"},
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet("clusterset1").WithClusterSelector(clusterapiv1beta2.ManagedClusterSelector{
					SelectorType:  clusterapiv1beta2.LabelSelector,
					LabelSelector: &metav1.LabelSelector{},
				}).Build(),
				testinghelpers.NewManagedCluster("cluster1").WithLabel(clusterapiv1beta2.ClusterSetLabel, "clusterset1").Build(),
				testinghelpers.NewManagedCluster("cluster2").Build(),
			},
			expectedClusterNames: []string{"cluster1", "cluster2"},
		},
		{
			name:            "clusterset has invalid ClusterSelector",
			clusterSetNames: []string{"clusterset1"},
			initObjs: []runtime.Object{
				testinghelpers.NewClusterSet("clusterset1").WithClusterSelector(
					clusterapiv1beta2.ManagedClusterSelector{
						SelectorType: "errorType",
					},
				).Build(),
				testinghelpers.NewManagedCluster("cluster1").WithLabel(clusterapiv1beta2.ClusterSetLabel, "clusterset1").Build(),
			},
			expectedClusterNames: []string{},
		},
		{
			name:                 "clusterset is removed",
			clusterSetNames:      []string{"clusterset1"},
			initObjs:             []runtime.Object{},
			expectedClusterNames: []string{},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterClient := clusterfake.NewSimpleClientset(c.initObjs...)
			clusterInformerFactory := newClusterInformerFactory(t, clusterClient, c.initObjs...)

			ctrl := &schedulingController{
				clusterLister:    clusterInformerFactory.Cluster().V1().ManagedClusters().Lister(),
				clusterSetLister: clusterInformerFactory.Cluster().V1beta2().ManagedClusterSets().Lister(),
			}

			clusters, _ := ctrl.getAvailableClusters(c.clusterSetNames)

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
				nil,
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

func TestNewMisconfiguredCondition(t *testing.T) {
	cases := []struct {
		name            string
		status          *framework.Status
		expectedStatus  metav1.ConditionStatus
		expectedReason  string
		expectedMessage string
	}{
		{
			name:            "Misconfigured is false when status is success",
			status:          framework.NewStatus("plugin", framework.Success, "reasons"),
			expectedStatus:  metav1.ConditionFalse,
			expectedReason:  "Succeedconfigured",
			expectedMessage: "Placement configurations check pass",
		},
		{
			name:            "Misconfigured is false when status is error",
			status:          framework.NewStatus("plugin", framework.Error, "reasons"),
			expectedStatus:  metav1.ConditionFalse,
			expectedReason:  "Succeedconfigured",
			expectedMessage: "Placement configurations check pass",
		},
		{
			name:            "Misconfigured is true",
			status:          framework.NewStatus("plugin", framework.Misconfigured, "reasons"),
			expectedStatus:  metav1.ConditionTrue,
			expectedReason:  "Misconfigured",
			expectedMessage: "plugin:reasons",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			condition := newMisconfiguredCondition(c.status)
			if condition.Status != c.expectedStatus {
				t.Errorf("expected status %q but got %q", c.expectedStatus, condition.Status)
			}
			if condition.Reason != c.expectedReason {
				t.Errorf("expected reason %q but got %q", c.expectedReason, condition.Reason)
			}
			if condition.Message != c.expectedMessage {
				t.Errorf("expected message %q but got %q", c.expectedReason, condition.Reason)
			}
		})
	}
}

func TestBind(t *testing.T) {
	cases := []struct {
		name            string
		initObjs        []runtime.Object
		placement       *clusterapiv1beta1.Placement
		clusters        []*clusterapiv1.ManagedCluster
		validateActions func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:      "create single placementdecision",
			placement: testinghelpers.NewPlacement(placementNamespace, placementName).Build(),
			clusters:  newClusters(10),
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "create")
				actual := actions[0].(clienttesting.CreateActionImpl).Object
				placementDecision, ok := actual.(*clusterapiv1beta1.PlacementDecision)
				if !ok {
					t.Errorf("expected PlacementDecision was updated")
				}
				assertClustersSelected(t, placementDecision.Status.Decisions, newSelectedClusters(10)...)
				if placementDecision.Labels[clusterapiv1beta1.DecisionGroupIndexLabel] != "0" {
					t.Errorf("unexpected PlacementDecision labels %v", placementDecision.Labels)
				}
				if placementDecision.Labels[clusterapiv1beta1.DecisionGroupNameLabel] != "" {
					t.Errorf("unexpected PlacementDecision labels %v", placementDecision.Labels)
				}
			},
		},
		{
			name:      "create multiple placementdecisions",
			placement: testinghelpers.NewPlacement(placementNamespace, placementName).Build(),
			clusters:  newClusters(101),
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "create", "create")
				selectedClusters := newSelectedClusters(101)
				actual := actions[0].(clienttesting.CreateActionImpl).Object
				placementDecision, ok := actual.(*clusterapiv1beta1.PlacementDecision)
				if !ok {
					t.Errorf("expected PlacementDecision was updated")
				}
				assertClustersSelected(t, placementDecision.Status.Decisions, selectedClusters[0:100]...)
				if placementDecision.Labels[clusterapiv1beta1.DecisionGroupIndexLabel] != "0" {
					t.Errorf("unexpected PlacementDecision labels %v", placementDecision.Labels)
				}

				actual = actions[1].(clienttesting.CreateActionImpl).Object
				placementDecision, ok = actual.(*clusterapiv1beta1.PlacementDecision)
				if !ok {
					t.Errorf("expected PlacementDecision was updated")
				}
				assertClustersSelected(t, placementDecision.Status.Decisions, selectedClusters[100:]...)
				if placementDecision.Labels[clusterapiv1beta1.DecisionGroupIndexLabel] != "0" {
					t.Errorf("unexpected PlacementDecision labels %v", placementDecision.Labels)
				}
			},
		},
		{
			name:      "create empty placementdecision",
			placement: testinghelpers.NewPlacement(placementNamespace, placementName).Build(),
			clusters:  newClusters(0),
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "create")
				actual := actions[0].(clienttesting.CreateActionImpl).Object
				placementDecision, ok := actual.(*clusterapiv1beta1.PlacementDecision)
				if !ok {
					t.Errorf("expected PlacementDecision was created")
				}
				if len(placementDecision.Status.Decisions) != 0 {
					t.Errorf("expecte %d cluster selected, but got %d", 0, len(placementDecision.Status.Decisions))
				}
			},
		},
		{
			name: "create placementdecision with canary group strategy",
			placement: testinghelpers.NewPlacement(placementNamespace, placementName).WithGroupStrategy(clusterapiv1beta1.GroupStrategy{
				ClustersPerDecisionGroup: intstr.FromString("25%"),
				DecisionGroups: []clusterapiv1beta1.DecisionGroup{
					{
						GroupName: "canary",
						ClusterSelector: clusterapiv1beta1.GroupClusterSelector{
							LabelSelector: metav1.LabelSelector{MatchLabels: map[string]string{"cloud": "Azure"}},
						},
					},
				}}).Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithLabel("cloud", "Amazon").Build(),
				testinghelpers.NewManagedCluster("cluster2").WithLabel("cloud", "Amazon").Build(),
				testinghelpers.NewManagedCluster("cluster3").WithLabel("cloud", "Azure").Build(),
				testinghelpers.NewManagedCluster("cluster4").WithLabel("cloud", "Azure").Build(),
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "create", "create", "create", "create")
				selectedClusters := newSelectedClusters(4)
				actual := actions[0].(clienttesting.CreateActionImpl).Object
				placementDecision, ok := actual.(*clusterapiv1beta1.PlacementDecision)
				if !ok {
					t.Errorf("expected PlacementDecision was created")
				}
				assertClustersSelected(t, placementDecision.Status.Decisions, selectedClusters[2:3]...)
				if placementDecision.Labels[clusterapiv1beta1.DecisionGroupIndexLabel] != "0" {
					t.Errorf("unexpected PlacementDecision labels %v", placementDecision.Labels)
				}
				if placementDecision.Labels[clusterapiv1beta1.DecisionGroupNameLabel] != "canary" {
					t.Errorf("unexpected PlacementDecision labels %v", placementDecision.Labels)
				}

				actual = actions[1].(clienttesting.CreateActionImpl).Object
				placementDecision, ok = actual.(*clusterapiv1beta1.PlacementDecision)
				if !ok {
					t.Errorf("expected PlacementDecision was created")
				}
				assertClustersSelected(t, placementDecision.Status.Decisions, selectedClusters[3:]...)
				if placementDecision.Labels[clusterapiv1beta1.DecisionGroupIndexLabel] != "1" {
					t.Errorf("unexpected PlacementDecision labels %v", placementDecision.Labels)
				}
				if placementDecision.Labels[clusterapiv1beta1.DecisionGroupNameLabel] != "canary" {
					t.Errorf("unexpected PlacementDecision labels %v", placementDecision.Labels)
				}

				actual = actions[2].(clienttesting.CreateActionImpl).Object
				placementDecision, ok = actual.(*clusterapiv1beta1.PlacementDecision)
				if !ok {
					t.Errorf("expected PlacementDecision was created")
				}
				assertClustersSelected(t, placementDecision.Status.Decisions, selectedClusters[0:1]...)
				if placementDecision.Labels[clusterapiv1beta1.DecisionGroupIndexLabel] != "2" {
					t.Errorf("unexpected PlacementDecision labels %v", placementDecision.Labels)
				}
				if placementDecision.Labels[clusterapiv1beta1.DecisionGroupNameLabel] != "" {
					t.Errorf("unexpected PlacementDecision labels %v", placementDecision.Labels)
				}

				actual = actions[3].(clienttesting.CreateActionImpl).Object
				placementDecision, ok = actual.(*clusterapiv1beta1.PlacementDecision)
				if !ok {
					t.Errorf("expected PlacementDecision was created")
				}
				assertClustersSelected(t, placementDecision.Status.Decisions, selectedClusters[1:2]...)
				if placementDecision.Labels[clusterapiv1beta1.DecisionGroupIndexLabel] != "3" {
					t.Errorf("unexpected PlacementDecision labels %v", placementDecision.Labels)
				}
				if placementDecision.Labels[clusterapiv1beta1.DecisionGroupNameLabel] != "" {
					t.Errorf("unexpected PlacementDecision labels %v", placementDecision.Labels)
				}
			},
		},
		{
			name: "create placementdecision when no cluster selected by canary group strategy",
			placement: testinghelpers.NewPlacement(placementNamespace, placementName).WithGroupStrategy(clusterapiv1beta1.GroupStrategy{
				DecisionGroups: []clusterapiv1beta1.DecisionGroup{
					{
						GroupName: "canary",
						ClusterSelector: clusterapiv1beta1.GroupClusterSelector{
							LabelSelector: metav1.LabelSelector{MatchLabels: map[string]string{"cloud": "Azure"}},
						},
					},
				}}).Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithLabel("cloud", "Amazon").Build(),
				testinghelpers.NewManagedCluster("cluster2").WithLabel("cloud", "Amazon").Build(),
				testinghelpers.NewManagedCluster("cluster3").WithLabel("cloud", "Amazon").Build(),
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "create")
				selectedClusters := newSelectedClusters(3)
				actual := actions[0].(clienttesting.CreateActionImpl).Object
				placementDecision, ok := actual.(*clusterapiv1beta1.PlacementDecision)
				if !ok {
					t.Errorf("expected PlacementDecision was created")
				}
				assertClustersSelected(t, placementDecision.Status.Decisions, selectedClusters...)
				if placementDecision.Labels[clusterapiv1beta1.DecisionGroupIndexLabel] != "0" {
					t.Errorf("unexpected PlacementDecision labels %v", placementDecision.Labels)
				}
				if placementDecision.Labels[clusterapiv1beta1.DecisionGroupNameLabel] != "" {
					t.Errorf("unexpected PlacementDecision labels %v", placementDecision.Labels)
				}
			},
		},
		{
			name: "create placementdecision with multiple group strategy",
			placement: testinghelpers.NewPlacement(placementNamespace, placementName).WithGroupStrategy(clusterapiv1beta1.GroupStrategy{
				DecisionGroups: []clusterapiv1beta1.DecisionGroup{
					{
						GroupName: "group1",
						ClusterSelector: clusterapiv1beta1.GroupClusterSelector{
							LabelSelector: metav1.LabelSelector{MatchLabels: map[string]string{"cloud": "Amazon"}},
						},
					},
					{
						GroupName: "group2",
						ClusterSelector: clusterapiv1beta1.GroupClusterSelector{
							LabelSelector: metav1.LabelSelector{MatchLabels: map[string]string{"cloud": "Azure"}},
						},
					},
				}}).Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithLabel("cloud", "Amazon").Build(),
				testinghelpers.NewManagedCluster("cluster2").WithLabel("cloud", "Amazon").Build(),
				testinghelpers.NewManagedCluster("cluster3").WithLabel("cloud", "Azure").Build(),
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "create", "create")
				selectedClusters := newSelectedClusters(4)
				actual := actions[0].(clienttesting.CreateActionImpl).Object
				placementDecision, ok := actual.(*clusterapiv1beta1.PlacementDecision)
				if !ok {
					t.Errorf("expected PlacementDecision was created")
				}
				assertClustersSelected(t, placementDecision.Status.Decisions, selectedClusters[0:2]...)
				if placementDecision.Labels[clusterapiv1beta1.DecisionGroupIndexLabel] != "0" {
					t.Errorf("unexpected PlacementDecision labels %v", placementDecision.Labels)
				}
				if placementDecision.Labels[clusterapiv1beta1.DecisionGroupNameLabel] != "group1" {
					t.Errorf("unexpected PlacementDecision labels %v", placementDecision.Labels)
				}

				actual = actions[1].(clienttesting.CreateActionImpl).Object
				placementDecision, ok = actual.(*clusterapiv1beta1.PlacementDecision)
				if !ok {
					t.Errorf("expected PlacementDecision was created")
				}
				assertClustersSelected(t, placementDecision.Status.Decisions, selectedClusters[2:3]...)
				if placementDecision.Labels[clusterapiv1beta1.DecisionGroupIndexLabel] != "1" {
					t.Errorf("unexpected PlacementDecision labels %v", placementDecision.Labels)
				}
				if placementDecision.Labels[clusterapiv1beta1.DecisionGroupNameLabel] != "group2" {
					t.Errorf("unexpected PlacementDecision labels %v", placementDecision.Labels)
				}
			},
		},
		{
			name: "create placementdecision with only cluster per decision group",
			placement: testinghelpers.NewPlacement(placementNamespace, placementName).WithGroupStrategy(clusterapiv1beta1.GroupStrategy{
				ClustersPerDecisionGroup: intstr.FromString("25%"),
			}).Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithLabel("cloud", "Amazon").Build(),
				testinghelpers.NewManagedCluster("cluster2").WithLabel("cloud", "Amazon").Build(),
				testinghelpers.NewManagedCluster("cluster3").WithLabel("cloud", "Azure").Build(),
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "create", "create", "create")
				selectedClusters := newSelectedClusters(4)
				actual := actions[0].(clienttesting.CreateActionImpl).Object
				placementDecision, ok := actual.(*clusterapiv1beta1.PlacementDecision)
				if !ok {
					t.Errorf("expected PlacementDecision was created")
				}
				assertClustersSelected(t, placementDecision.Status.Decisions, selectedClusters[0:1]...)
				if placementDecision.Labels[clusterapiv1beta1.DecisionGroupIndexLabel] != "0" {
					t.Errorf("unexpected PlacementDecision labels %v", placementDecision.Labels)
				}
				if placementDecision.Labels[clusterapiv1beta1.DecisionGroupNameLabel] != "" {
					t.Errorf("unexpected PlacementDecision labels %v", placementDecision.Labels)
				}

				actual = actions[1].(clienttesting.CreateActionImpl).Object
				placementDecision, ok = actual.(*clusterapiv1beta1.PlacementDecision)
				if !ok {
					t.Errorf("expected PlacementDecision was created")
				}
				assertClustersSelected(t, placementDecision.Status.Decisions, selectedClusters[1:2]...)
				if placementDecision.Labels[clusterapiv1beta1.DecisionGroupIndexLabel] != "1" {
					t.Errorf("unexpected PlacementDecision labels %v", placementDecision.Labels)
				}
				if placementDecision.Labels[clusterapiv1beta1.DecisionGroupNameLabel] != "" {
					t.Errorf("unexpected PlacementDecision labels %v", placementDecision.Labels)
				}

				actual = actions[2].(clienttesting.CreateActionImpl).Object
				placementDecision, ok = actual.(*clusterapiv1beta1.PlacementDecision)
				if !ok {
					t.Errorf("expected PlacementDecision was created")
				}
				assertClustersSelected(t, placementDecision.Status.Decisions, selectedClusters[2:3]...)
				if placementDecision.Labels[clusterapiv1beta1.DecisionGroupIndexLabel] != "2" {
					t.Errorf("unexpected PlacementDecision labels %v", placementDecision.Labels)
				}
				if placementDecision.Labels[clusterapiv1beta1.DecisionGroupNameLabel] != "" {
					t.Errorf("unexpected PlacementDecision labels %v", placementDecision.Labels)
				}
			},
		},
		{
			name:      "no change",
			placement: testinghelpers.NewPlacement(placementNamespace, placementName).Build(),
			clusters:  newClusters(128),
			initObjs: []runtime.Object{
				testinghelpers.NewPlacementDecision(placementNamespace, testinghelpers.PlacementDecisionName(placementName, 1)).
					WithLabel(clusterapiv1beta1.PlacementLabel, placementName).
					WithLabel(clusterapiv1beta1.DecisionGroupNameLabel, "").
					WithLabel(clusterapiv1beta1.DecisionGroupIndexLabel, "0").
					WithDecisions(newSelectedClusters(128)[:100]...).Build(),
				testinghelpers.NewPlacementDecision(placementNamespace, testinghelpers.PlacementDecisionName(placementName, 2)).
					WithLabel(clusterapiv1beta1.PlacementLabel, placementName).
					WithLabel(clusterapiv1beta1.DecisionGroupNameLabel, "").
					WithLabel(clusterapiv1beta1.DecisionGroupIndexLabel, "0").
					WithDecisions(newSelectedClusters(128)[100:]...).Build(),
			},
			validateActions: testingcommon.AssertNoActions,
		},
		{
			name:      "update one of placementdecisions",
			placement: testinghelpers.NewPlacement(placementNamespace, placementName).Build(),
			clusters:  newClusters(128),
			initObjs: []runtime.Object{
				testinghelpers.NewPlacementDecision(placementNamespace, testinghelpers.PlacementDecisionName(placementName, 1)).
					WithLabel(clusterapiv1beta1.PlacementLabel, placementName).
					WithLabel(clusterapiv1beta1.DecisionGroupNameLabel, "fakegroup").
					WithLabel(clusterapiv1beta1.DecisionGroupIndexLabel, "0").
					WithDecisions(newSelectedClusters(128)[:100]...).Build(),
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch", "create")
				selectedClusters := newSelectedClusters(128)
				placementDecision := &clusterapiv1beta1.PlacementDecision{}
				patchData := actions[0].(clienttesting.PatchActionImpl).Patch
				err := json.Unmarshal(patchData, placementDecision)
				if err != nil {
					t.Fatal(err)
				}
				// only update labels
				assertClustersSelected(t, placementDecision.Status.Decisions)
				if placementDecision.Labels[clusterapiv1beta1.DecisionGroupNameLabel] != "" {
					t.Errorf("unexpected PlacementDecision labels %v", placementDecision.Labels)
				}

				actual := actions[1].(clienttesting.CreateActionImpl).Object
				placementDecision, ok := actual.(*clusterapiv1beta1.PlacementDecision)
				if !ok {
					t.Errorf("expected PlacementDecision was updated")
				}
				assertClustersSelected(t, placementDecision.Status.Decisions, selectedClusters[100:]...)
			},
		},
		{
			name:      "delete redundant placementdecisions",
			placement: testinghelpers.NewPlacement(placementNamespace, placementName).Build(),
			clusters:  newClusters(10),
			initObjs: []runtime.Object{
				testinghelpers.NewPlacementDecision(placementNamespace, testinghelpers.PlacementDecisionName(placementName, 1)).
					WithLabel(clusterapiv1beta1.PlacementLabel, placementName).
					WithLabel(clusterapiv1beta1.DecisionGroupNameLabel, "").
					WithLabel(clusterapiv1beta1.DecisionGroupIndexLabel, "0").
					WithDecisions(newSelectedClusters(128)[:100]...).Build(),
				testinghelpers.NewPlacementDecision(placementNamespace, testinghelpers.PlacementDecisionName(placementName, 2)).
					WithLabel(clusterapiv1beta1.PlacementLabel, placementName).
					WithLabel(clusterapiv1beta1.DecisionGroupNameLabel, "").
					WithLabel(clusterapiv1beta1.DecisionGroupIndexLabel, "0").
					WithDecisions(newSelectedClusters(128)[100:]...).Build(),
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch", "delete")
				placementDecision := &clusterapiv1beta1.PlacementDecision{}
				patchData := actions[0].(clienttesting.PatchActionImpl).Patch
				err := json.Unmarshal(patchData, placementDecision)
				if err != nil {
					t.Fatal(err)
				}
				assertClustersSelected(t, placementDecision.Status.Decisions, newSelectedClusters(10)...)
			},
		},
		{
			name:      "delete all placementdecisions and leave one empty placementdecision",
			placement: testinghelpers.NewPlacement(placementNamespace, placementName).Build(),
			clusters:  newClusters(0),
			initObjs: []runtime.Object{
				testinghelpers.NewPlacementDecision(placementNamespace, testinghelpers.PlacementDecisionName(placementName, 1)).
					WithLabel(clusterapiv1beta1.PlacementLabel, placementName).
					WithLabel(clusterapiv1beta1.DecisionGroupNameLabel, "").
					WithLabel(clusterapiv1beta1.DecisionGroupIndexLabel, "0").
					WithDecisions(newSelectedClusters(128)[:100]...).Build(),
				testinghelpers.NewPlacementDecision(placementNamespace, testinghelpers.PlacementDecisionName(placementName, 2)).
					WithLabel(clusterapiv1beta1.PlacementLabel, placementName).
					WithLabel(clusterapiv1beta1.DecisionGroupNameLabel, "").
					WithLabel(clusterapiv1beta1.DecisionGroupIndexLabel, "0").
					WithDecisions(newSelectedClusters(128)[100:]...).Build(),
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch", "delete")
				placementDecision := &clusterapiv1beta1.PlacementDecision{}
				patchData := actions[0].(clienttesting.PatchActionImpl).Patch
				err := json.Unmarshal(patchData, placementDecision)
				if err != nil {
					t.Fatal(err)
				}
				if len(placementDecision.Status.Decisions) != 0 {
					t.Errorf("expecte %d cluster selected, but got %d", 0, len(placementDecision.Status.Decisions))
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterClient := clusterfake.NewSimpleClientset(c.initObjs...)

			// GenerateName is not working for fake clent, set the name with random suffix
			clusterClient.PrependReactor(
				"create",
				"placementdecisions",
				func(action clienttesting.Action) (handled bool, ret runtime.Object, err error) {
					createAction := action.(clienttesting.CreateActionImpl)
					pd := createAction.Object.(*clusterapiv1beta1.PlacementDecision)
					pd.Name = fmt.Sprintf("%s%s", pd.GenerateName, rand.String(5))
					return false, pd, nil
				},
			)

			clusterInformerFactory := newClusterInformerFactory(t, clusterClient, c.initObjs...)

			s := &testScheduler{}

			ctrl := schedulingController{
				clusterClient:           clusterClient,
				clusterLister:           clusterInformerFactory.Cluster().V1().ManagedClusters().Lister(),
				clusterSetLister:        clusterInformerFactory.Cluster().V1beta2().ManagedClusterSets().Lister(),
				clusterSetBindingLister: clusterInformerFactory.Cluster().V1beta2().ManagedClusterSetBindings().Lister(),
				placementLister:         clusterInformerFactory.Cluster().V1beta1().Placements().Lister(),
				placementDecisionLister: clusterInformerFactory.Cluster().V1beta1().PlacementDecisions().Lister(),
				scheduler:               s,
				eventsRecorder:          kevents.NewFakeRecorder(100),
				metricsRecorder:         metrics.NewScheduleMetrics(clock.RealClock{}),
			}

			decisions, _, _ := ctrl.generatePlacementDecisionsAndStatus(c.placement, c.clusters)
			err := ctrl.bind(
				context.TODO(),
				c.placement,
				decisions,
				nil,
				nil,
			)
			if err != nil {
				t.Errorf("unexpected err: %v", err)
			}
			c.validateActions(t, clusterClient.Actions())
		})
	}
}

func assertClustersSelected(t *testing.T, decisons []clusterapiv1beta1.ClusterDecision, clusterNames ...string) {
	names := sets.NewString(clusterNames...)
	for _, decision := range decisons {
		if names.Has(decision.ClusterName) {
			names.Delete(decision.ClusterName)
		}
	}

	if names.Len() != 0 {
		t.Errorf("expected clusters selected: %s, but got %v", strings.Join(names.UnsortedList(), ","), decisons)
	}
}

func newClusters(num int) (clusters []*clusterapiv1.ManagedCluster) {
	for i := 0; i < num; i++ {
		ClusterName := fmt.Sprintf("cluster%d", i+1)
		c := testinghelpers.NewManagedCluster(ClusterName).Build()
		clusters = append(clusters, c)
	}
	return clusters
}

func newSelectedClusters(num int) (clusters []string) {
	for i := 0; i < num; i++ {
		clusters = append(clusters, fmt.Sprintf("cluster%d", i+1))
	}

	sort.SliceStable(clusters, func(i, j int) bool {
		return clusters[i] < clusters[j]
	})

	return clusters
}
