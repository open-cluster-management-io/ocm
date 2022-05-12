package managedcluster

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	v1 "open-cluster-management.io/api/cluster/v1"
	testinghelpers "open-cluster-management.io/registration/pkg/helpers/testing"

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
				testinghelpers.AssertNoActions(t, actions)
			},
		},
		{
			name:            "create a new spoke cluster",
			startingObjects: []runtime.Object{testinghelpers.NewManagedCluster()},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertActions(t, actions, "patch")
				patch := actions[0].(clienttesting.PatchAction).GetPatch()
				managedCluster := &v1.ManagedCluster{}
				err := json.Unmarshal(patch, managedCluster)
				if err != nil {
					t.Fatal(err)
				}
				testinghelpers.AssertFinalizers(t, managedCluster, []string{managedClusterFinalizer})
			},
		},
		{
			name:            "accept a spoke cluster",
			startingObjects: []runtime.Object{testinghelpers.NewAcceptingManagedCluster()},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				expectedCondition := metav1.Condition{
					Type:    v1.ManagedClusterConditionHubAccepted,
					Status:  metav1.ConditionTrue,
					Reason:  "HubClusterAdminAccepted",
					Message: "Accepted by hub cluster admin",
				}
				testinghelpers.AssertActions(t, actions, "get", "patch")
				patch := actions[1].(clienttesting.PatchAction).GetPatch()
				managedCluster := &v1.ManagedCluster{}
				err := json.Unmarshal(patch, managedCluster)
				if err != nil {
					t.Fatal(err)
				}
				testinghelpers.AssertManagedClusterCondition(t, managedCluster.Status.Conditions, expectedCondition)
			},
		},
		{
			name:            "sync an accepted spoke cluster",
			startingObjects: []runtime.Object{testinghelpers.NewAcceptedManagedCluster()},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertActions(t, actions, "get")
			},
		},
		{
			name:            "deny an accepted spoke cluster",
			startingObjects: []runtime.Object{testinghelpers.NewDeniedManagedCluster()},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				expectedCondition := metav1.Condition{
					Type:    v1.ManagedClusterConditionHubAccepted,
					Status:  metav1.ConditionFalse,
					Reason:  "HubClusterAdminDenied",
					Message: "Denied by hub cluster admin",
				}
				testinghelpers.AssertActions(t, actions, "get", "patch")
				patch := actions[1].(clienttesting.PatchAction).GetPatch()
				managedCluster := &v1.ManagedCluster{}
				err := json.Unmarshal(patch, managedCluster)
				if err != nil {
					t.Fatal(err)
				}
				testinghelpers.AssertManagedClusterCondition(t, managedCluster.Status.Conditions, expectedCondition)
			},
		},
		{
			name:            "delete a spoke cluster",
			startingObjects: []runtime.Object{testinghelpers.NewDeletingManagedCluster()},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertActions(t, actions, "patch")
				patch := actions[0].(clienttesting.PatchAction).GetPatch()
				managedCluster := &v1.ManagedCluster{}
				err := json.Unmarshal(patch, managedCluster)
				if err != nil {
					t.Fatal(err)
				}
				testinghelpers.AssertFinalizers(t, managedCluster, []string{})
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterClient := clusterfake.NewSimpleClientset(c.startingObjects...)
			kubeClient := kubefake.NewSimpleClientset()
			clusterInformerFactory := clusterinformers.NewSharedInformerFactory(clusterClient, time.Minute*10)
			clusterStore := clusterInformerFactory.Cluster().V1().ManagedClusters().Informer().GetStore()
			for _, cluster := range c.startingObjects {
				if err := clusterStore.Add(cluster); err != nil {
					t.Fatal(err)
				}
			}

			ctrl := managedClusterController{kubeClient, clusterClient, clusterInformerFactory.Cluster().V1().ManagedClusters().Lister(), resourceapply.NewResourceCache(), eventstesting.NewTestingEventRecorder(t)}
			syncErr := ctrl.sync(context.TODO(), testinghelpers.NewFakeSyncContext(t, testinghelpers.TestManagedClusterName))
			if syncErr != nil {
				t.Errorf("unexpected err: %v", syncErr)
			}

			c.validateActions(t, clusterClient.Actions())
		})
	}
}
