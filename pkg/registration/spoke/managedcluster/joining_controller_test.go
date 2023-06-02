package managedcluster

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	testinghelpers "open-cluster-management.io/ocm/pkg/registration/helpers/testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"
)

func TestSyncManagedCluster(t *testing.T) {
	cases := []struct {
		name            string
		startingObjects []runtime.Object
		validateActions func(t *testing.T, actions []clienttesting.Action)
		expectedErr     string
	}{
		{
			name:            "sync no managed cluster",
			startingObjects: []runtime.Object{},
			validateActions: testingcommon.AssertNoActions,
			expectedErr:     "unable to get managed cluster with name \"testmanagedcluster\" from hub: managedcluster.cluster.open-cluster-management.io \"testmanagedcluster\" not found",
		},
		{
			name:            "sync an unaccepted managed cluster",
			startingObjects: []runtime.Object{testinghelpers.NewManagedCluster()},
			validateActions: testingcommon.AssertNoActions,
		},
		{
			name:            "sync an accepted managed cluster",
			startingObjects: []runtime.Object{testinghelpers.NewAcceptedManagedCluster()},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				expectedCondition := metav1.Condition{
					Type:    clusterv1.ManagedClusterConditionJoined,
					Status:  metav1.ConditionTrue,
					Reason:  "ManagedClusterJoined",
					Message: "Managed cluster joined",
				}
				testingcommon.AssertActions(t, actions, "get", "patch")
				patch := actions[1].(clienttesting.PatchAction).GetPatch()
				managedCluster := &clusterv1.ManagedCluster{}
				err := json.Unmarshal(patch, managedCluster)
				if err != nil {
					t.Fatal(err)
				}
				testingcommon.AssertCondition(t, managedCluster.Status.Conditions, expectedCondition)
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterClient := clusterfake.NewSimpleClientset(c.startingObjects...)
			clusterInformerFactory := clusterinformers.NewSharedInformerFactory(clusterClient, time.Minute*10)
			clusterStore := clusterInformerFactory.Cluster().V1().ManagedClusters().Informer().GetStore()
			for _, cluster := range c.startingObjects {
				if err := clusterStore.Add(cluster); err != nil {
					t.Fatal(err)
				}
			}

			ctrl := managedClusterJoiningController{
				clusterName:      testinghelpers.TestManagedClusterName,
				hubClusterClient: clusterClient,
				hubClusterLister: clusterInformerFactory.Cluster().V1().ManagedClusters().Lister(),
			}

			syncErr := ctrl.sync(context.TODO(), testingcommon.NewFakeSyncContext(t, ""))
			testingcommon.AssertError(t, syncErr, c.expectedErr)

			c.validateActions(t, clusterClient.Actions())
		})
	}
}
