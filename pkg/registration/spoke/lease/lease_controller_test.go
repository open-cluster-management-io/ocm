package lease

import (
	"context"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"

	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	testinghelpers "open-cluster-management.io/ocm/pkg/registration/helpers/testing"
)

func TestSync(t *testing.T) {
	cases := []struct {
		name                               string
		clusters                           []runtime.Object
		controllerlastLeaseDurationSeconds int32
		expectSyncErr                      string
		validateActions                    func(fakeLeaseUpdater *fakeLeaseUpdater)
	}{
		{
			name:                               "start lease update routine",
			clusters:                           []runtime.Object{testinghelpers.NewAcceptedManagedCluster()},
			controllerlastLeaseDurationSeconds: testinghelpers.TestLeaseDurationSeconds,
			validateActions: func(fakeLeaseUpdater *fakeLeaseUpdater) {
				// start method should be called
				if !fakeLeaseUpdater.startCalled {
					t.Error("start method should be called")
				}
				// stop method should not be called
				if fakeLeaseUpdater.stopCalled {
					t.Error("stop method should not be called")
				}
			},
		},
		{
			name:                               "the managed cluster can not be found",
			clusters:                           []runtime.Object{},
			controllerlastLeaseDurationSeconds: testinghelpers.TestLeaseDurationSeconds,
			expectSyncErr: "unable to get managed cluster \"testmanagedcluster\" from hub: " +
				"managedcluster.cluster.open-cluster-management.io \"testmanagedcluster\" not found",
			validateActions: func(fakeLeaseUpdater *fakeLeaseUpdater) {
				// start method should not be called
				if fakeLeaseUpdater.startCalled {
					t.Error("start method should not be called")
				}
				// stop method should be called
				if !fakeLeaseUpdater.stopCalled {
					t.Error("stop method should be called")
				}
			},
		},
		{
			name:                               "unaccept a managed cluster",
			clusters:                           []runtime.Object{testinghelpers.NewManagedCluster()},
			controllerlastLeaseDurationSeconds: testinghelpers.TestLeaseDurationSeconds,
			validateActions: func(fakeLeaseUpdater *fakeLeaseUpdater) {
				// start method should not be called
				if fakeLeaseUpdater.startCalled {
					t.Error("start method should not be called")
				}
				// stop method should be called
				if !fakeLeaseUpdater.stopCalled {
					t.Error("stop method should be called")
				}
			},
		},
		{
			name:                               "update the lease duration",
			clusters:                           []runtime.Object{testinghelpers.NewAcceptedManagedCluster()},
			controllerlastLeaseDurationSeconds: testinghelpers.TestLeaseDurationSeconds + 1,
			validateActions: func(fakeLeaseUpdater *fakeLeaseUpdater) {
				// first stop the old lease update routine, and then start a new lease update routine
				// stop method should be called
				if !fakeLeaseUpdater.stopCalled {
					t.Error("stop method should be called eventually")
				}
				// start method should be called
				if !fakeLeaseUpdater.startCalled {
					t.Error("start method should not called eventually")
				}
			},
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

			ctrl := &managedClusterLeaseController{
				clusterName:              testinghelpers.TestManagedClusterName,
				hubClusterLister:         clusterInformerFactory.Cluster().V1().ManagedClusters().Lister(),
				leaseUpdater:             &fakeLeaseUpdater{},
				lastLeaseDurationSeconds: c.controllerlastLeaseDurationSeconds,
			}

			syncErr := ctrl.sync(context.TODO(), testingcommon.NewFakeSyncContext(t, ""))
			testingcommon.AssertError(t, syncErr, c.expectSyncErr)

			if c.validateActions != nil {
				c.validateActions(ctrl.leaseUpdater.(*fakeLeaseUpdater))
			}
		})
	}
}

type fakeLeaseUpdater struct {
	startCalled bool
	stopCalled  bool
}

func (f *fakeLeaseUpdater) start(ctx context.Context, leaseDuration time.Duration) {
	f.startCalled = true
}

func (f *fakeLeaseUpdater) stop() {
	f.stopCalled = true
}

func TestLeaseUpdater(t *testing.T) {
	initRenewTime := time.Now()
	hubClient := kubefake.NewSimpleClientset(testinghelpers.NewManagedClusterLease("managed-cluster-lease", initRenewTime))
	leaseUpdater := &leaseUpdater{
		hubClient:   hubClient,
		clusterName: testinghelpers.TestManagedClusterName,
		leaseName:   "managed-cluster-lease",
		recorder:    eventstesting.NewTestingEventRecorder(t),
	}

	// start the updater
	ctx := context.Background()
	leaseUpdater.start(ctx, time.Second*1)

	// wait for 3 second, the all actions should be in get,update pairs
	time.Sleep(time.Second * 3)
	actions := hubClient.Actions()
	if len(actions) == 0 {
		t.Error("expect at least 1 update actions, but got 0")
		return
	}
	for i := 0; i < len(actions); i += 2 {
		if actions[i].GetVerb() != "get" {
			t.Errorf("expect get action, but got %s", actions[i].GetVerb())
		}
		if actions[i+1].GetVerb() != "update" {
			t.Errorf("expect update action, but got %s", actions[i+1].GetVerb())
		}
	}

	// stop the updater
	leaseUpdater.stop()
	actionLen := len(actions)

	// wait for 3 second, no new actions should be added
	time.Sleep(time.Second * 3)
	actions = hubClient.Actions()
	if len(actions) != actionLen {
		t.Errorf("expect %d actions, but got %d", actionLen, len(actions))
	}
}
