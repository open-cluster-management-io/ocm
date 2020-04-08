package spokecluster

import (
	"context"
	"reflect"
	"testing"
	"time"

	clusterfake "github.com/open-cluster-management/api/client/cluster/clientset/versioned/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/util/workqueue"

	v1 "github.com/open-cluster-management/api/cluster/v1"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
)

const testSpokeClusterName = "test_spoke_cluster"

func TestSyncSpokeCluster(t *testing.T) {
	cases := []struct {
		name            string
		startingObjects []runtime.Object
		validateActions func(t *testing.T, actions []clienttesting.Action)
		expectedErr     string
	}{
		{
			name:            "sync a deleted spoke cluster",
			startingObjects: []runtime.Object{},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 1 {
					t.Errorf("expected 1 call but got: %#v", actions)
				}
				assertAction(t, actions[0], "get")
			},
		},
		{
			name:            "create a new spoke cluster",
			startingObjects: []runtime.Object{newSpokeCluster()},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 2 {
					t.Errorf("expected 1 call but got: %#v", actions)
				}
				assertAction(t, actions[1], "update")
				assertFinalizers(t, actions[1].(clienttesting.UpdateActionImpl).Object, []string{spokeClusterFinalizer})
			},
		},
		{
			name:            "accept a spoke cluster",
			startingObjects: []runtime.Object{newAcceptedSpokeCluster([]string{spokeClusterFinalizer})},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 3 {
					t.Errorf("expected 3 call but got: %#v", actions)
				}
				assertAction(t, actions[2], "update")
				assertCondition(t, actions[2].(clienttesting.UpdateActionImpl).Object, v1.SpokeClusterConditionHubAccepted, metav1.ConditionTrue)
			},
		},
		{
			name:            "sync an accepted spoke cluster",
			startingObjects: []runtime.Object{newAcceptedSpokeClusterWithCondition()},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 2 {
					t.Errorf("expected 2 call but got: %#v", actions)
				}
				assertAction(t, actions[1], "get")
			},
		},
		{
			name:            "deny an accepted spoke cluster",
			startingObjects: []runtime.Object{newDeniedSpokeCluster()},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 3 {
					t.Errorf("expected 3 call but got: %#v", actions)
				}
				assertAction(t, actions[2], "update")
				assertCondition(t, actions[2].(clienttesting.UpdateActionImpl).Object, v1.SpokeClusterConditionHubAccepted, metav1.ConditionFalse)
			},
		},
		{
			name:            "delete a spoke cluster",
			startingObjects: []runtime.Object{newDeletingSpokeCluster()},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 2 {
					t.Errorf("expected 2 call but got: %#v", actions)
				}
				assertAction(t, actions[1], "update")
				assertFinalizers(t, actions[1].(clienttesting.UpdateActionImpl).Object, []string{})
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterClient := clusterfake.NewSimpleClientset(c.startingObjects...)
			kubeClient := kubefake.NewSimpleClientset()

			ctrl := spokeClusterController{kubeClient, clusterClient, eventstesting.NewTestingEventRecorder(t)}
			syncErr := ctrl.sync(context.TODO(), newFakeSyncContext(t))
			if len(c.expectedErr) > 0 && syncErr == nil {
				t.Errorf("expected %q error", c.expectedErr)
				return
			}
			if len(c.expectedErr) > 0 && syncErr != nil && syncErr.Error() != c.expectedErr {
				t.Errorf("expected %q error, got %q", c.expectedErr, syncErr.Error())
				return
			}
			if syncErr != nil {
				t.Errorf("unexpected err: %v", syncErr)
			}

			c.validateActions(t, clusterClient.Actions())
		})
	}
}

func assertAction(t *testing.T, actual clienttesting.Action, expected string) {
	if actual.GetVerb() != expected {
		t.Errorf("expected %s action but got: %#v", expected, actual)
	}
}

func assertFinalizers(t *testing.T, actual runtime.Object, expected []string) {
	spokeCluster := actual.(*v1.SpokeCluster)
	if !reflect.DeepEqual(spokeCluster.Finalizers, expected) {
		t.Errorf("expected %#v but got %#v", expected, spokeCluster.Finalizers)
	}
}

func assertCondition(t *testing.T, actual runtime.Object, expectedCondition string, expectedStatus metav1.ConditionStatus) {
	spokeCluster := actual.(*v1.SpokeCluster)
	conditions := spokeCluster.Status.Conditions
	if len(conditions) != 1 {
		t.Errorf("expected 1 condition but got: %#v", conditions)
	}
	condition := conditions[0]
	if condition.Type != expectedCondition {
		t.Errorf("expected %s but got: %s", expectedCondition, condition.Type)
	}
	if condition.Status != expectedStatus {
		t.Errorf("expected %s but got: %s", expectedStatus, condition.Status)
	}
}

func newSpokeCluster() *v1.SpokeCluster {
	return &v1.SpokeCluster{ObjectMeta: metav1.ObjectMeta{Name: testSpokeClusterName}}
}

func newAcceptedSpokeCluster(finalizers []string) *v1.SpokeCluster {
	return &v1.SpokeCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:       testSpokeClusterName,
			Finalizers: finalizers,
		},
		Spec: v1.SpokeClusterSpec{HubAcceptsClient: true},
	}
}

func newAcceptedSpokeClusterWithCondition() *v1.SpokeCluster {
	spokeCluster := newAcceptedSpokeCluster([]string{spokeClusterFinalizer})
	spokeCluster.Finalizers = append(spokeCluster.Finalizers, spokeClusterFinalizer)
	spokeCluster.Status.Conditions = append(spokeCluster.Status.Conditions, v1.StatusCondition{
		Type:               v1.SpokeClusterConditionHubAccepted,
		Status:             metav1.ConditionTrue,
		Reason:             "HubClusterAdminAccepted",
		Message:            "Accepted by hub cluster admin",
		LastTransitionTime: metav1.Time{Time: metav1.Now().Add(-10 * time.Second)},
	})
	return spokeCluster
}

func newDeniedSpokeCluster() *v1.SpokeCluster {
	spokeCluster := newAcceptedSpokeClusterWithCondition()
	spokeCluster.Spec.HubAcceptsClient = false
	return spokeCluster
}

func newDeletingSpokeCluster() *v1.SpokeCluster {
	now := metav1.Now()
	return &v1.SpokeCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:              testSpokeClusterName,
			DeletionTimestamp: &now,
			Finalizers:        []string{spokeClusterFinalizer},
		},
		Spec: v1.SpokeClusterSpec{HubAcceptsClient: true},
	}
}

type fakeSyncContext struct {
	spokeName string
	recorder  events.Recorder
}

func newFakeSyncContext(t *testing.T) *fakeSyncContext {
	return &fakeSyncContext{
		spokeName: testSpokeClusterName,
		recorder:  eventstesting.NewTestingEventRecorder(t),
	}
}

func (f fakeSyncContext) Queue() workqueue.RateLimitingInterface { return nil }
func (f fakeSyncContext) QueueKey() string                       { return f.spokeName }
func (f fakeSyncContext) Recorder() events.Recorder              { return f.recorder }
