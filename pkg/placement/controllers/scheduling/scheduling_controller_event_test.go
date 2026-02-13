package scheduling

import (
	"context"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kevents "k8s.io/client-go/tools/events"

	clusterapiv1beta1 "open-cluster-management.io/api/cluster/v1beta1"

	"open-cluster-management.io/ocm/pkg/placement/controllers/framework"
	testinghelpers "open-cluster-management.io/ocm/pkg/placement/helpers/testing"
)

func TestCreateOrUpdatePlacementDecision_EventRecording(t *testing.T) {
	cases := []struct {
		name                 string
		existingPD           *clusterapiv1beta1.PlacementDecision
		newDecisions         []clusterapiv1beta1.ClusterDecision
		newLabels            map[string]string
		status               *framework.Status
		clusterScores        PrioritizerScore
		expectEventCount     int
		expectDecisionCreate bool
		expectDecisionUpdate bool
		expectScoreUpdate    bool
		expectWarning        bool
	}{
		{
			name:                 "new placement decision created",
			existingPD:           nil,
			newDecisions:         []clusterapiv1beta1.ClusterDecision{{ClusterName: "cluster1"}},
			newLabels:            map[string]string{"test": "label"},
			status:               framework.NewStatus("", framework.Success, ""),
			clusterScores:        PrioritizerScore{"cluster1": 85},
			expectEventCount:     1,
			expectDecisionCreate: true,
			expectDecisionUpdate: false,
			expectScoreUpdate:    false,
		},
		{
			name: "decisions changed",
			existingPD: testinghelpers.NewPlacementDecision(placementNamespace, "placement1-decision-1").
				WithDecisions("cluster1").
				Build(),
			newDecisions: []clusterapiv1beta1.ClusterDecision{
				{ClusterName: "cluster1"},
				{ClusterName: "cluster2"},
			},
			status:               framework.NewStatus("", framework.Success, ""),
			clusterScores:        PrioritizerScore{"cluster1": 85, "cluster2": 92},
			expectEventCount:     2,
			expectDecisionCreate: false,
			expectDecisionUpdate: true,
			expectScoreUpdate:    true,
		},
		{
			name: "labels changed only",
			existingPD: testinghelpers.NewPlacementDecision(placementNamespace, "placement1-decision-1").
				WithDecisions("cluster1").
				WithLabel(clusterapiv1beta1.DecisionGroupIndexLabel, "0").
				Build(),
			newDecisions: []clusterapiv1beta1.ClusterDecision{{ClusterName: "cluster1"}},
			newLabels: map[string]string{
				clusterapiv1beta1.PlacementLabel:          placementName,
				clusterapiv1beta1.DecisionGroupIndexLabel: "1",
			},
			status:               framework.NewStatus("", framework.Success, ""),
			clusterScores:        PrioritizerScore{"cluster1": 85},
			expectEventCount:     0,
			expectDecisionCreate: false,
			expectDecisionUpdate: false,
			expectScoreUpdate:    false,
		},
		{
			name: "no changes",
			existingPD: testinghelpers.NewPlacementDecision(placementNamespace, "placement1-decision-1").
				WithDecisions("cluster1").
				WithLabel(clusterapiv1beta1.PlacementLabel, placementName).
				WithLabel(clusterapiv1beta1.DecisionGroupIndexLabel, "0").
				Build(),
			newDecisions: []clusterapiv1beta1.ClusterDecision{{ClusterName: "cluster1"}},
			newLabels: map[string]string{
				clusterapiv1beta1.PlacementLabel:          placementName,
				clusterapiv1beta1.DecisionGroupIndexLabel: "0",
			},
			status:               framework.NewStatus("", framework.Success, ""),
			clusterScores:        PrioritizerScore{"cluster1": 85},
			expectEventCount:     0,
			expectDecisionCreate: false,
			expectDecisionUpdate: false,
			expectScoreUpdate:    false,
		},
		{
			name: "warning status event",
			existingPD: testinghelpers.NewPlacementDecision(placementNamespace, "placement1-decision-1").
				WithDecisions("cluster1").
				Build(),
			newDecisions: []clusterapiv1beta1.ClusterDecision{
				{ClusterName: "cluster1"},
				{ClusterName: "cluster2"},
			},
			status:               framework.NewStatus("TestPlugin", framework.Warning, "test warning"),
			clusterScores:        PrioritizerScore{"cluster1": 85},
			expectEventCount:     2,
			expectDecisionCreate: false,
			expectDecisionUpdate: true,
			expectScoreUpdate:    true,
			expectWarning:        true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var initObjs []runtime.Object
			if c.existingPD != nil {
				initObjs = append(initObjs, c.existingPD)
			}

			ctrl, _, fakeRecorder := newTestSchedulingController(t, initObjs)

			placement := testinghelpers.NewPlacement(placementNamespace, placementName).Build()
			placementDecision := &clusterapiv1beta1.PlacementDecision{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "placement1-decision-1",
					Namespace: placementNamespace,
					Labels:    c.newLabels,
				},
				Status: clusterapiv1beta1.PlacementDecisionStatus{
					Decisions: c.newDecisions,
				},
			}

			err := ctrl.createOrUpdatePlacementDecision(context.TODO(), placement, placementDecision, c.clusterScores, c.status)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Collect and verify events
			events := collectEvents(fakeRecorder, c.expectEventCount, 500*time.Millisecond)
			verifyEvents(t, events, c.expectEventCount, c.expectDecisionCreate, c.expectDecisionUpdate, c.expectScoreUpdate, c.expectWarning)
		})
	}
}

// collectEvents collects events from FakeRecorder with a timeout
func collectEvents(recorder *kevents.FakeRecorder, expectedCount int, timeout time.Duration) []string {
	var events []string
	deadline := time.After(timeout)

	for i := 0; i < expectedCount; i++ {
		select {
		case event := <-recorder.Events:
			events = append(events, event)
		case <-deadline:
			return events
		}
	}

	return events
}

// verifyEvents verifies the collected events match expectations
func verifyEvents(t *testing.T, events []string, expectEventCount int, expectDecisionCreate, expectDecisionUpdate, expectScoreUpdate, expectWarning bool) {
	if len(events) != expectEventCount {
		t.Errorf("expected %d events, got %d events: %v", expectEventCount, len(events), events)
	}

	eventTypes := map[string]bool{
		"DecisionCreate": expectDecisionCreate,
		"DecisionUpdate": expectDecisionUpdate,
		"ScoreUpdate":    expectScoreUpdate,
	}

	for eventType, expected := range eventTypes {
		found := containsEventType(events, eventType)
		if found != expected {
			t.Errorf("expected %s=%v, got %v", eventType, expected, found)
		}
	}

	if expectWarning && !containsEventType(events, "Warning") {
		t.Errorf("expected Warning event, got events: %v", events)
	}
}

// containsEventType checks if any event contains the specified type
func containsEventType(events []string, eventType string) bool {
	for _, event := range events {
		if strings.Contains(event, eventType) {
			return true
		}
	}
	return false
}
