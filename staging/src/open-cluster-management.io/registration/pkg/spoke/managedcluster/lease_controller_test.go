package managedcluster

import (
	"context"
	"testing"
	"time"

	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	testinghelpers "open-cluster-management.io/registration/pkg/helpers/testing"

	"github.com/openshift/library-go/pkg/operator/events/eventstesting"

	coordinationv1 "k8s.io/api/coordination/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

func TestLeaseUpdate(t *testing.T) {
	cases := []struct {
		name                    string
		clusters                []runtime.Object
		validateActions         func(t *testing.T, actions []clienttesting.Action)
		needToStartUpdateBefore bool
		expectedErr             string
	}{
		{
			name:     "start lease update routine",
			clusters: []runtime.Object{testinghelpers.NewAcceptedManagedCluster()},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertUpdateActions(t, actions)
				leaseObj := actions[1].(clienttesting.UpdateActionImpl).Object
				lastLeaseObj := actions[len(actions)-1].(clienttesting.UpdateActionImpl).Object
				testinghelpers.AssertLeaseUpdated(t, leaseObj.(*coordinationv1.Lease), lastLeaseObj.(*coordinationv1.Lease))
			},
		},
		{
			name:                    "delete a managed cluster after lease update routine is started",
			clusters:                []runtime.Object{},
			needToStartUpdateBefore: true,
			validateActions:         testinghelpers.AssertNoMoreUpdates,
			expectedErr:             "unable to get managed cluster \"testmanagedcluster\" from hub: managedcluster.cluster.open-cluster-management.io \"testmanagedcluster\" not found",
		},
		{
			name:                    "unaccept a managed cluster after lease update routine is started",
			clusters:                []runtime.Object{testinghelpers.NewManagedCluster()},
			needToStartUpdateBefore: true,
			validateActions:         testinghelpers.AssertNoMoreUpdates,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterClient := clusterfake.NewSimpleClientset(c.clusters...)
			clusterInformerFactory := clusterinformers.NewSharedInformerFactory(clusterClient, time.Minute*10)
			clusterStore := clusterInformerFactory.Cluster().V1().ManagedClusters().Informer().GetStore()
			for _, cluster := range c.clusters {
				if err := clusterStore.Add(cluster); err != nil {
					t.Fatal(err)
				}
			}

			hubClient := kubefake.NewSimpleClientset(testinghelpers.NewManagedClusterLease("managed-cluster-lease", time.Now()))

			leaseUpdater := &leaseUpdater{
				hubClient:   hubClient,
				clusterName: testinghelpers.TestManagedClusterName,
				leaseName:   "managed-cluster-lease",
				recorder:    eventstesting.NewTestingEventRecorder(t),
			}

			if c.needToStartUpdateBefore {
				leaseUpdater.start(context.TODO(), time.Duration(testinghelpers.TestLeaseDurationSeconds)*time.Second)
				// wait a few milliseconds to start the lease update routine
				time.Sleep(200 * time.Millisecond)
			}

			ctrl := &managedClusterLeaseController{
				clusterName:      testinghelpers.TestManagedClusterName,
				hubClusterLister: clusterInformerFactory.Cluster().V1().ManagedClusters().Lister(),
				leaseUpdater:     leaseUpdater,
			}
			syncErr := ctrl.sync(context.TODO(), testinghelpers.NewFakeSyncContext(t, ""))
			testinghelpers.AssertError(t, syncErr, c.expectedErr)

			// wait one cycle
			time.Sleep(1200 * time.Millisecond)
			c.validateActions(t, hubClient.Actions())
		})
	}
}
