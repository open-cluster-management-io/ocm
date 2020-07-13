package managedcluster

import (
	"context"
	"testing"

	clusterfake "github.com/open-cluster-management/api/client/cluster/clientset/versioned/fake"
	v1 "github.com/open-cluster-management/api/cluster/v1"
	testinghelpers "github.com/open-cluster-management/registration/pkg/helpers/testing"

	"github.com/openshift/library-go/pkg/operator/events/eventstesting"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

func TestSyncManagedCluster(t *testing.T) {
	cases := []struct {
		name            string
		startingObjects []runtime.Object
		validateActions func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:            "sync a deleted spoke cluster",
			startingObjects: []runtime.Object{},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertActions(t, actions, "get")
			},
		},
		{
			name:            "create a new spoke cluster",
			startingObjects: []runtime.Object{testinghelpers.NewManagedCluster()},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertActions(t, actions, "get", "update")
				managedCluster := (actions[1].(clienttesting.UpdateActionImpl).Object).(*v1.ManagedCluster)
				testinghelpers.AssertFinalizers(t, managedCluster, []string{managedClusterFinalizer})
			},
		},
		{
			name:            "accept a spoke cluster",
			startingObjects: []runtime.Object{testinghelpers.NewAcceptingManagedCluster()},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				expectedCondition := v1.StatusCondition{
					Type:    v1.ManagedClusterConditionHubAccepted,
					Status:  metav1.ConditionTrue,
					Reason:  "HubClusterAdminAccepted",
					Message: "Accepted by hub cluster admin",
				}
				testinghelpers.AssertActions(t, actions, "get", "get", "update")
				actual := actions[2].(clienttesting.UpdateActionImpl).Object
				managedCluster := actual.(*v1.ManagedCluster)
				testinghelpers.AssertManagedClusterCondition(t, managedCluster.Status.Conditions, expectedCondition)
			},
		},
		{
			name:            "sync an accepted spoke cluster",
			startingObjects: []runtime.Object{testinghelpers.NewAcceptedManagedCluster()},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertActions(t, actions, "get", "get")
			},
		},
		{
			name:            "deny an accepted spoke cluster",
			startingObjects: []runtime.Object{testinghelpers.NewDeniedManagedCluster()},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				expectedCondition := v1.StatusCondition{
					Type:    v1.ManagedClusterConditionHubAccepted,
					Status:  metav1.ConditionFalse,
					Reason:  "HubClusterAdminDenied",
					Message: "Denied by hub cluster admin",
				}
				testinghelpers.AssertActions(t, actions, "get", "get", "update")
				actual := actions[2].(clienttesting.UpdateActionImpl).Object
				managedCluster := actual.(*v1.ManagedCluster)
				testinghelpers.AssertManagedClusterCondition(t, managedCluster.Status.Conditions, expectedCondition)
			},
		},
		{
			name:            "delete a spoke cluster",
			startingObjects: []runtime.Object{testinghelpers.NewDeletingManagedCluster()},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertActions(t, actions, "get", "update")
				managedCluster := (actions[1].(clienttesting.UpdateActionImpl).Object).(*v1.ManagedCluster)
				testinghelpers.AssertFinalizers(t, managedCluster, []string{})
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterClient := clusterfake.NewSimpleClientset(c.startingObjects...)
			kubeClient := kubefake.NewSimpleClientset()

			ctrl := managedClusterController{kubeClient, clusterClient, eventstesting.NewTestingEventRecorder(t)}
			syncErr := ctrl.sync(context.TODO(), testinghelpers.NewFakeSyncContext(t, testinghelpers.TestManagedClusterName))
			if syncErr != nil {
				t.Errorf("unexpected err: %v", syncErr)
			}

			c.validateActions(t, clusterClient.Actions())
		})
	}
}
