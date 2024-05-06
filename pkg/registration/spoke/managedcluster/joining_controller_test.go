package managedcluster

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubeinformers "k8s.io/client-go/informers"
	fakekube "k8s.io/client-go/kubernetes/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"

	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterscheme "open-cluster-management.io/api/client/cluster/clientset/versioned/scheme"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

	"open-cluster-management.io/ocm/pkg/common/helpers"
	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	testinghelpers "open-cluster-management.io/ocm/pkg/registration/helpers/testing"
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
			expectedErr: "unable to get managed cluster \"testmanagedcluster\" from hub: " +
				"managedcluster.cluster.open-cluster-management.io \"testmanagedcluster\" not found",
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
				testingcommon.AssertActions(t, actions, "patch")
				patch := actions[0].(clienttesting.PatchAction).GetPatch()
				managedCluster := &clusterv1.ManagedCluster{}
				err := json.Unmarshal(patch, managedCluster)
				if err != nil {
					t.Fatal(err)
				}
				testingcommon.AssertCondition(t, managedCluster.Status.Conditions, expectedCondition)
			},
		},
	}

	apiServer, discoveryClient := newDiscoveryServer(t, nil)
	defer apiServer.Close()
	kubeClient := kubefake.NewSimpleClientset()
	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClient, time.Minute*10)

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

			fakeHubClient := fakekube.NewSimpleClientset()
			ctx := context.TODO()
			hubEventRecorder, err := helpers.NewEventRecorder(ctx,
				clusterscheme.Scheme, fakeHubClient, "test")
			if err != nil {
				t.Fatal(err)
			}
			ctrl := newManagedClusterStatusController(
				testinghelpers.TestManagedClusterName,
				clusterClient,
				clusterInformerFactory.Cluster().V1().ManagedClusters(),
				discoveryClient,
				clusterInformerFactory.Cluster().V1alpha1().ClusterClaims(),
				kubeInformerFactory.Core().V1().Nodes(),
				20,
				eventstesting.NewTestingEventRecorder(t),
				hubEventRecorder,
			)

			syncErr := ctrl.sync(ctx, testingcommon.NewFakeSyncContext(t, ""))
			testingcommon.AssertError(t, syncErr, c.expectedErr)

			c.validateActions(t, clusterClient.Actions())
		})
	}
}
