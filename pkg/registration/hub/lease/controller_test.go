package lease

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubeinformers "k8s.io/client-go/informers"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/util/workqueue"

	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterscheme "open-cluster-management.io/api/client/cluster/clientset/versioned/scheme"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/events"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	testinghelpers "open-cluster-management.io/ocm/pkg/registration/helpers/testing"
)

var now = time.Now()

func TestSync(t *testing.T) {
	leaseStopUpdatingValidateActions := func(t *testing.T, hubKubeActions, clusterActions []clienttesting.Action) {
		expected := metav1.Condition{
			Type:    clusterv1.ManagedClusterConditionAvailable,
			Status:  metav1.ConditionUnknown,
			Reason:  "ManagedClusterLeaseUpdateStopped",
			Message: "Registration agent stopped updating its lease.",
		}
		testingcommon.AssertActions(t, clusterActions, "patch")
		patch := clusterActions[0].(clienttesting.PatchAction).GetPatch()
		managedCluster := &clusterv1.ManagedCluster{}
		err := json.Unmarshal(patch, managedCluster)
		if err != nil {
			t.Fatal(err)
		}
		testingcommon.AssertCondition(t, managedCluster.Status.Conditions, expected)

		if len(hubKubeActions) != 1 {
			t.Errorf("Expected 1 event created in the sync loop, actual %d",
				len(hubKubeActions))
		}
		actionEvent := hubKubeActions[0]
		if actionEvent.GetResource().Resource != "events" {
			t.Errorf("Expected event created, actual %s", actionEvent.GetResource())
		}
		if actionEvent.GetNamespace() != testinghelpers.TestManagedClusterName {
			t.Errorf("Expected event created in namespace %s, actual %s",
				testinghelpers.TestManagedClusterName, actionEvent.GetNamespace())
		}
		if actionEvent.GetVerb() != "create" {
			t.Errorf("Expected event created, actual %s", actionEvent.GetVerb())
		}
	}

	cases := []struct {
		name            string
		clusters        []runtime.Object
		clusterLeases   []runtime.Object
		validateActions func(t *testing.T, leaseActions, clusterActions []clienttesting.Action)
	}{
		{
			name:          "sync unaccepted managed cluster",
			clusters:      []runtime.Object{testinghelpers.NewManagedCluster()},
			clusterLeases: []runtime.Object{},
			validateActions: func(t *testing.T, leaseActions, clusterActions []clienttesting.Action) {
				testingcommon.AssertNoActions(t, leaseActions)
				testingcommon.AssertNoActions(t, clusterActions)
			},
		},
		{
			name:          "sync previously accepted managed cluster",
			clusters:      []runtime.Object{testinghelpers.NewDeniedManagedCluster("False")},
			clusterLeases: []runtime.Object{},
			validateActions: func(t *testing.T, leaseActions, clusterActions []clienttesting.Action) {
				testingcommon.AssertActions(t, leaseActions, "create")
				testingcommon.AssertNoActions(t, clusterActions)
			},
		},
		{
			name:          "there is no lease for a managed cluster",
			clusters:      []runtime.Object{testinghelpers.NewAcceptedManagedCluster()},
			clusterLeases: []runtime.Object{},
			validateActions: func(t *testing.T, leaseActions, clusterActions []clienttesting.Action) {
				testingcommon.AssertActions(t, leaseActions, "create")
				testingcommon.AssertNoActions(t, clusterActions)
			},
		},
		{
			name:     "accepted managed cluster stop update lease",
			clusters: []runtime.Object{testinghelpers.NewAvailableManagedCluster()},
			clusterLeases: []runtime.Object{
				testinghelpers.NewManagedClusterLease("managed-cluster-lease", now.Add(-5*time.Minute)),
				testinghelpers.NewManagedClusterLease(fmt.Sprintf("cluster-lease-%s", testinghelpers.TestManagedClusterName), now.Add(-5*time.Minute)),
			},
			validateActions: leaseStopUpdatingValidateActions,
		},
		{
			name:     "previously accepted managed cluster stop update lease",
			clusters: []runtime.Object{testinghelpers.NewDeniedManagedCluster("False")},
			clusterLeases: []runtime.Object{
				testinghelpers.NewManagedClusterLease("managed-cluster-lease", now.Add(-5*time.Minute)),
				testinghelpers.NewManagedClusterLease(fmt.Sprintf("cluster-lease-%s", testinghelpers.TestManagedClusterName), now.Add(-5*time.Minute)),
			},
			validateActions: leaseStopUpdatingValidateActions,
		},
		{
			name:          "managed cluster is available",
			clusters:      []runtime.Object{testinghelpers.NewAvailableManagedCluster()},
			clusterLeases: []runtime.Object{testinghelpers.NewManagedClusterLease("managed-cluster-lease", now)},
			validateActions: func(t *testing.T, leaseActions, clusterActions []clienttesting.Action) {
				testingcommon.AssertNoActions(t, clusterActions)
			},
		},
		{
			name:     "managed cluster is deleting",
			clusters: []runtime.Object{newDeletingManagedCluster()},
			validateActions: func(t *testing.T, leaseActions, clusterActions []clienttesting.Action) {
				expected := metav1.Condition{
					Type:    clusterv1.ManagedClusterConditionAvailable,
					Status:  metav1.ConditionUnknown,
					Reason:  "ManagedClusterLeaseUpdateStopped",
					Message: "Registration agent stopped updating its lease.",
				}
				testingcommon.AssertActions(t, clusterActions, "patch")
				patch := clusterActions[0].(clienttesting.PatchAction).GetPatch()
				managedCluster := &clusterv1.ManagedCluster{}
				err := json.Unmarshal(patch, managedCluster)
				if err != nil {
					t.Fatal(err)
				}
				testingcommon.AssertCondition(t, managedCluster.Status.Conditions, expected)
			},
		},
		{
			name:          "managed cluster is unknown",
			clusters:      []runtime.Object{testinghelpers.NewUnknownManagedCluster()},
			clusterLeases: []runtime.Object{testinghelpers.NewManagedClusterLease("managed-cluster-lease", now.Add(-5*time.Minute))},
			validateActions: func(t *testing.T, leaseActions, clusterActions []clienttesting.Action) {
				testingcommon.AssertNoActions(t, clusterActions)
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

			hubClient := kubefake.NewSimpleClientset(c.clusterLeases...)
			leaseInformerFactory := kubeinformers.NewSharedInformerFactory(hubClient, time.Minute*10)
			leaseStore := leaseInformerFactory.Coordination().V1().Leases().Informer().GetStore()
			for _, lease := range c.clusterLeases {
				if err := leaseStore.Add(lease); err != nil {
					t.Fatal(err)
				}
			}

			ctx := context.TODO()
			syncCtx := testingcommon.NewFakeSyncContext(t, testinghelpers.TestManagedClusterName)
			mcEventRecorder, err := events.NewEventRecorder(ctx, clusterscheme.Scheme, hubClient.EventsV1(), "test")
			if err != nil {
				t.Fatal(err)
			}
			ctrl := &leaseController{
				kubeClient: hubClient,
				patcher: patcher.NewPatcher[
					*clusterv1.ManagedCluster, clusterv1.ManagedClusterSpec, clusterv1.ManagedClusterStatus](
					clusterClient.ClusterV1().ManagedClusters()),
				clusterLister:   clusterInformerFactory.Cluster().V1().ManagedClusters().Lister(),
				leaseLister:     leaseInformerFactory.Coordination().V1().Leases().Lister(),
				mcEventRecorder: mcEventRecorder,
			}
			syncErr := ctrl.sync(context.TODO(), syncCtx, testinghelpers.TestManagedClusterName)
			if syncErr != nil {
				t.Errorf("unexpected err: %v", syncErr)
			}

			// wait for the event to be recorded
			time.Sleep(100 * time.Millisecond)
			c.validateActions(t, hubClient.Actions(), clusterClient.Actions())
		})
	}
}

func newDeletingManagedCluster() *clusterv1.ManagedCluster {
	now := metav1.Now()
	cluster := testinghelpers.NewAcceptedManagedCluster()
	cluster.DeletionTimestamp = &now
	return cluster
}

// spyQueue wraps a real queue and captures AddAfter calls
type spyQueue struct {
	workqueue.TypedRateLimitingInterface[string]
	addAfterDelay time.Duration
	addAfterKey   interface{}
}

func (s *spyQueue) AddAfter(item string, duration time.Duration) {
	s.addAfterDelay = duration
	s.addAfterKey = item
	s.TypedRateLimitingInterface.AddAfter(item, duration)
}

// testSyncContext is a custom sync context for testing requeue timing
type testSyncContext struct {
	queueKey string
	recorder events.Recorder
	queue    *spyQueue
}

func (t *testSyncContext) Queue() workqueue.TypedRateLimitingInterface[string] { return t.queue }
func (t *testSyncContext) QueueKey() string                                    { return t.queueKey }
func (t *testSyncContext) Recorder() events.Recorder                           { return t.recorder }

func newManagedClusterWithLeaseDuration(seconds int32) *clusterv1.ManagedCluster {
	cluster := testinghelpers.NewAvailableManagedCluster()
	cluster.Spec.LeaseDurationSeconds = seconds
	return cluster
}

func TestRequeueTime(t *testing.T) {
	// Using 60 second lease duration (grace period = 5 * 60 = 300 seconds)
	cases := []struct {
		name               string
		cluster            runtime.Object
		clusterLease       runtime.Object
		expectedRequeueMin time.Duration
		expectedRequeueMax time.Duration
	}{
		{
			name:    "requeue for expired lease - recovery detected via watch, use grace period",
			cluster: newManagedClusterWithLeaseDuration(60),
			// Lease expired 5 minutes ago (grace period is 5 * 60 = 300 seconds)
			clusterLease:       testinghelpers.NewManagedClusterLease("managed-cluster-lease", now.Add(-5*time.Minute)),
			expectedRequeueMin: 295 * time.Second, // Should be ~300s (grace period)
			expectedRequeueMax: 305 * time.Second, // Allow some tolerance
		},
		{
			name:    "requeue for fresh lease - should use time until expiry for immediate detection",
			cluster: newManagedClusterWithLeaseDuration(60),
			// Lease renewed 4 minutes ago, will expire in 1 minute (grace period is 5 minutes)
			clusterLease:       testinghelpers.NewManagedClusterLease("managed-cluster-lease", now.Add(-4*time.Minute)),
			expectedRequeueMin: 55 * time.Second, // Should be ~60s (1 minute)
			expectedRequeueMax: 65 * time.Second, // Allow some tolerance
		},
		{
			name:    "requeue for recently renewed lease - should check just before expiry",
			cluster: newManagedClusterWithLeaseDuration(60),
			// Lease renewed 30 seconds ago, will expire in 4.5 minutes
			clusterLease:       testinghelpers.NewManagedClusterLease("managed-cluster-lease", now.Add(-30*time.Second)),
			expectedRequeueMin: 265 * time.Second, // Should be ~270s (4.5 minutes)
			expectedRequeueMax: 275 * time.Second, // Allow some tolerance
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterClient := clusterfake.NewSimpleClientset(c.cluster)
			clusterInformerFactory := clusterinformers.NewSharedInformerFactory(clusterClient, time.Minute*10)
			clusterStore := clusterInformerFactory.Cluster().V1().ManagedClusters().Informer().GetStore()
			if err := clusterStore.Add(c.cluster); err != nil {
				t.Fatal(err)
			}

			hubClient := kubefake.NewSimpleClientset(c.clusterLease)
			leaseInformerFactory := kubeinformers.NewSharedInformerFactory(hubClient, time.Minute*10)
			leaseStore := leaseInformerFactory.Coordination().V1().Leases().Informer().GetStore()
			if err := leaseStore.Add(c.clusterLease); err != nil {
				t.Fatal(err)
			}

			ctx := context.TODO()
			mcEventRecorder, err := events.NewEventRecorder(ctx, clusterscheme.Scheme, hubClient.EventsV1(), "test")
			if err != nil {
				t.Fatal(err)
			}

			// Create a custom sync context with spy queue to capture AddAfter calls
			spyQ := &spyQueue{
				TypedRateLimitingInterface: workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[string]()),
			}
			syncCtx := &testSyncContext{
				queueKey: testinghelpers.TestManagedClusterName,
				recorder: events.NewContextualLoggingEventRecorder(t.Name()),
				queue:    spyQ,
			}

			ctrl := &leaseController{
				kubeClient: hubClient,
				patcher: patcher.NewPatcher[
					*clusterv1.ManagedCluster, clusterv1.ManagedClusterSpec, clusterv1.ManagedClusterStatus](
					clusterClient.ClusterV1().ManagedClusters()),
				clusterLister:   clusterInformerFactory.Cluster().V1().ManagedClusters().Lister(),
				leaseLister:     leaseInformerFactory.Coordination().V1().Leases().Lister(),
				mcEventRecorder: mcEventRecorder,
			}

			syncErr := ctrl.sync(context.TODO(), syncCtx, testinghelpers.TestManagedClusterName)
			if syncErr != nil {
				t.Errorf("unexpected err: %v", syncErr)
			}

			// Verify the requeue delay captured by the spy
			actualDelay := spyQ.addAfterDelay
			if actualDelay < c.expectedRequeueMin || actualDelay > c.expectedRequeueMax {
				t.Errorf("Unexpected requeue delay: got %v, expected between %v and %v",
					actualDelay, c.expectedRequeueMin, c.expectedRequeueMax)
			} else {
				t.Logf("✓ Requeue delay %v is within expected range [%v, %v]",
					actualDelay, c.expectedRequeueMin, c.expectedRequeueMax)
			}
		})
	}
}
