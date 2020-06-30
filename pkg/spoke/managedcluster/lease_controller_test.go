package managedcluster

import (
	"context"
	"fmt"
	"testing"
	"time"

	clusterfake "github.com/open-cluster-management/api/client/cluster/clientset/versioned/fake"
	clusterinformers "github.com/open-cluster-management/api/client/cluster/informers/externalversions"
	clusterv1 "github.com/open-cluster-management/api/cluster/v1"
	testinghelpers "github.com/open-cluster-management/registration/pkg/helpers/testing"

	"github.com/openshift/library-go/pkg/operator/events/eventstesting"

	coordinationv1 "k8s.io/api/coordination/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

const testLeaseDurationSeconds int32 = 1

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
			clusters: []runtime.Object{newAcceptedManagedClusterWithLeaseDuration()},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				assertLeaseUpdateActions(t, actions)
				leaseObj := actions[1].(clienttesting.UpdateActionImpl).Object
				lastLeaseObj := actions[len(actions)-1].(clienttesting.UpdateActionImpl).Object
				assertLeaseUpdated(t, leaseObj, lastLeaseObj)
			},
		},
		{
			name:                    "delete a managed cluster after lease update routine is started",
			clusters:                []runtime.Object{},
			needToStartUpdateBefore: true,
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				assertNoMoreUpdates(t, actions)
			},
			expectedErr: "unable to get managed cluster \"testmanagedcluster\" from hub: managedcluster.cluster.open-cluster-management.io \"testmanagedcluster\" not found",
		},
		{
			name:                    "unaccept a managed cluster after lease update routine is started",
			clusters:                []runtime.Object{newManagedCluster()},
			needToStartUpdateBefore: true,
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				assertNoMoreUpdates(t, actions)
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterClient := clusterfake.NewSimpleClientset(c.clusters...)
			clusterInformerFactory := clusterinformers.NewSharedInformerFactory(clusterClient, time.Minute*10)
			clusterStore := clusterInformerFactory.Cluster().V1().ManagedClusters().Informer().GetStore()
			for _, cluster := range c.clusters {
				clusterStore.Add(cluster)
			}

			hubClient := kubefake.NewSimpleClientset(&coordinationv1.Lease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-lease-testmanagedcluster",
					Namespace: "testmanagedcluster",
				},
			})

			leaseUpdater := &leaseUpdater{
				hubClient:   hubClient,
				clusterName: testManagedClusterName,
				leaseName:   fmt.Sprintf("cluster-lease-%s", testManagedClusterName),
				recorder:    eventstesting.NewTestingEventRecorder(t),
			}

			if c.needToStartUpdateBefore {
				leaseUpdater.start(context.TODO(), time.Duration(testLeaseDurationSeconds)*time.Second)
				// wait a few milliseconds to start the lease update routine
				time.Sleep(200 * time.Millisecond)
			}

			ctrl := &managedClusterLeaseController{
				clusterName:      testManagedClusterName,
				hubClusterLister: clusterInformerFactory.Cluster().V1().ManagedClusters().Lister(),
				leaseUpdater:     leaseUpdater,
			}
			syncErr := ctrl.sync(context.TODO(), testinghelpers.NewFakeSyncContext(t, ""))
			if len(c.expectedErr) > 0 && syncErr == nil {
				t.Errorf("expected %q error", c.expectedErr)
				return
			}
			if len(c.expectedErr) > 0 && syncErr != nil && syncErr.Error() != c.expectedErr {
				t.Errorf("expected %q error, got %q", c.expectedErr, syncErr.Error())
				return
			}
			if len(c.expectedErr) == 0 && syncErr != nil {
				t.Errorf("unexpected err: %v", syncErr)
			}

			// wait one cycle
			time.Sleep(1200 * time.Millisecond)
			c.validateActions(t, hubClient.Actions())
		})
	}
}

func newAcceptedManagedClusterWithLeaseDuration() *clusterv1.ManagedCluster {
	cluster := newAcceptedManagedCluster()
	cluster.Spec.LeaseDurationSeconds = testLeaseDurationSeconds
	return cluster
}

func assertLeaseUpdateActions(t *testing.T, actions []clienttesting.Action) {
	for i := 0; i < len(actions); i = i + 2 {
		if actions[i].GetVerb() != "get" {
			t.Errorf("expected action %d is get, but %v", i, actions[i])
		}
		if actions[i+1].GetVerb() != "update" {
			t.Errorf("expected action %d is update, but %v", i, actions[i+1])
		}
	}
}

func assertLeaseUpdated(t *testing.T, lease, lastLeaseObj runtime.Object) {
	firstRenewTime := lease.(*coordinationv1.Lease).Spec.RenewTime
	lastRenewTime := lastLeaseObj.(*coordinationv1.Lease).Spec.RenewTime
	if !firstRenewTime.BeforeTime(&metav1.Time{Time: lastRenewTime.Time}) {
		t.Errorf("expected lease updated, but failed")
	}
}

func assertNoMoreUpdates(t *testing.T, actions []clienttesting.Action) {
	updateActions := 0
	for _, action := range actions {
		if action.GetVerb() == "update" {
			updateActions++
		}
	}
	// make sure the lease update routine is started and no more update actions
	if updateActions != 1 {
		t.Errorf("expected there is only one update action, but failed")
	}
}
