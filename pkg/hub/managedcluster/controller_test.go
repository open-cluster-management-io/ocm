package managedcluster

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

	v1 "github.com/open-cluster-management/api/cluster/v1"
	testinghelpers "github.com/open-cluster-management/registration/pkg/helpers/testing"
	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
)

const testManagedClusterName = "test_managed_cluster"

func TestSyncManagedCluster(t *testing.T) {
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
			startingObjects: []runtime.Object{newManagedCluster()},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 2 {
					t.Errorf("expected 1 call but got: %#v", actions)
				}
				assertAction(t, actions[1], "update")
				assertFinalizers(t, actions[1].(clienttesting.UpdateActionImpl).Object, []string{managedClusterFinalizer})
			},
		},
		{
			name:            "accept a spoke cluster",
			startingObjects: []runtime.Object{newAcceptedManagedCluster([]string{managedClusterFinalizer})},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 3 {
					t.Errorf("expected 3 call but got: %#v", actions)
				}
				assertAction(t, actions[2], "update")
				assertCondition(t, actions[2].(clienttesting.UpdateActionImpl).Object, v1.ManagedClusterConditionHubAccepted, metav1.ConditionTrue)
			},
		},
		{
			name:            "sync an accepted spoke cluster",
			startingObjects: []runtime.Object{newAcceptedManagedClusterWithCondition()},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 2 {
					t.Errorf("expected 2 call but got: %#v", actions)
				}
				assertAction(t, actions[1], "get")
			},
		},
		{
			name:            "deny an accepted spoke cluster",
			startingObjects: []runtime.Object{newDeniedManagedCluster()},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 3 {
					t.Errorf("expected 3 call but got: %#v", actions)
				}
				assertAction(t, actions[2], "update")
				assertCondition(t, actions[2].(clienttesting.UpdateActionImpl).Object, v1.ManagedClusterConditionHubAccepted, metav1.ConditionFalse)
			},
		},
		{
			name:            "delete a spoke cluster",
			startingObjects: []runtime.Object{newDeletingManagedCluster()},
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

			ctrl := managedClusterController{kubeClient, clusterClient, eventstesting.NewTestingEventRecorder(t)}
			syncErr := ctrl.sync(context.TODO(), testinghelpers.NewFakeSyncContext(t, testManagedClusterName))
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
	managedCluster := actual.(*v1.ManagedCluster)
	if !reflect.DeepEqual(managedCluster.Finalizers, expected) {
		t.Errorf("expected %#v but got %#v", expected, managedCluster.Finalizers)
	}
}

func assertCondition(t *testing.T, actual runtime.Object, expectedCondition string, expectedStatus metav1.ConditionStatus) {
	managedCluster := actual.(*v1.ManagedCluster)
	conditions := managedCluster.Status.Conditions
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

func newManagedCluster() *v1.ManagedCluster {
	return &v1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: testManagedClusterName}}
}

func newAcceptedManagedCluster(finalizers []string) *v1.ManagedCluster {
	return &v1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:       testManagedClusterName,
			Finalizers: finalizers,
		},
		Spec: v1.ManagedClusterSpec{HubAcceptsClient: true},
	}
}

func newAcceptedManagedClusterWithCondition() *v1.ManagedCluster {
	spokeCluster := newAcceptedManagedCluster([]string{managedClusterFinalizer})
	spokeCluster.Finalizers = append(spokeCluster.Finalizers, managedClusterFinalizer)
	spokeCluster.Status.Conditions = append(spokeCluster.Status.Conditions, v1.StatusCondition{
		Type:               v1.ManagedClusterConditionHubAccepted,
		Status:             metav1.ConditionTrue,
		Reason:             "HubClusterAdminAccepted",
		Message:            "Accepted by hub cluster admin",
		LastTransitionTime: metav1.Time{Time: metav1.Now().Add(-10 * time.Second)},
	})
	return spokeCluster
}

func newDeniedManagedCluster() *v1.ManagedCluster {
	spokeCluster := newAcceptedManagedClusterWithCondition()
	spokeCluster.Spec.HubAcceptsClient = false
	return spokeCluster
}

func newDeletingManagedCluster() *v1.ManagedCluster {
	now := metav1.Now()
	return &v1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:              testManagedClusterName,
			DeletionTimestamp: &now,
			Finalizers:        []string{managedClusterFinalizer},
		},
		Spec: v1.ManagedClusterSpec{HubAcceptsClient: true},
	}
}
