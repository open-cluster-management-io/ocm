package finalizercontroller

import (
	"context"
	"fmt"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"

	fakeworkclient "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
)

func TestSyncUnamanagedAppliedWork(t *testing.T) {
	cases := []struct {
		name                               string
		appliedManifestWorkName            string
		hubHash                            string
		agentID                            string
		evictionGracePeriod                time.Duration
		manifestWorkInformerSynced         func() bool
		works                              []runtime.Object
		appliedWorks                       []runtime.Object
		expectedQueueLen                   int
		validateAppliedManifestWorkActions func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:                               "appliedmanifestwork is not found",
			appliedManifestWorkName:            "hubhash-test",
			hubHash:                            "hubhash",
			agentID:                            "test-agent",
			works:                              []runtime.Object{},
			appliedWorks:                       []runtime.Object{},
			validateAppliedManifestWorkActions: testingcommon.AssertNoActions,
		},
		{
			name:                    "appliedmanifestwork is deleting",
			appliedManifestWorkName: "hubhash-test",
			hubHash:                 "hubhash",
			agentID:                 "test-agent",
			works:                   []runtime.Object{},
			appliedWorks: []runtime.Object{
				&workapiv1.AppliedManifestWork{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "hubhash-test",
						DeletionTimestamp: &metav1.Time{},
					},
					Spec: workapiv1.AppliedManifestWorkSpec{
						ManifestWorkName: "test",
						HubHash:          "hubhash",
						AgentID:          "test-agent",
					},
				},
			},
			validateAppliedManifestWorkActions: testingcommon.AssertNoActions,
		},
		{
			name:                    "evict appliedmanifestwork when its relating manifestwork is missing on the hub",
			appliedManifestWorkName: "hubhash-test",
			hubHash:                 "hubhash",
			agentID:                 "test-agent",
			works:                   []runtime.Object{},
			appliedWorks: []runtime.Object{
				&workapiv1.AppliedManifestWork{
					ObjectMeta: metav1.ObjectMeta{
						Name: "hubhash-test",
					},
					Spec: workapiv1.AppliedManifestWorkSpec{
						ManifestWorkName: "test",
						HubHash:          "hubhash",
						AgentID:          "test-agent",
					},
				},
			},
			validateAppliedManifestWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
			},
		},
		{
			name:                       "do not start eviction while ManifestWork informer has not synced",
			appliedManifestWorkName:    "hubhash-test",
			hubHash:                    "hubhash",
			agentID:                    "test-agent",
			manifestWorkInformerSynced: func() bool { return false },
			works:                      []runtime.Object{},
			appliedWorks: []runtime.Object{
				&workapiv1.AppliedManifestWork{
					ObjectMeta: metav1.ObjectMeta{
						Name: "hubhash-test",
					},
					Spec: workapiv1.AppliedManifestWorkSpec{
						ManifestWorkName: "test",
						HubHash:          "hubhash",
						AgentID:          "test-agent",
					},
				},
			},
			expectedQueueLen:                   0, // requeued via AddAfter, not the main queue
			validateAppliedManifestWorkActions: testingcommon.AssertNoActions,
		},
		{
			name:                       "clear evictionStartTime while ManifestWork informer has not synced",
			appliedManifestWorkName:    "hubhash-test",
			hubHash:                    "hubhash",
			agentID:                    "test-agent",
			manifestWorkInformerSynced: func() bool { return false },
			works:                      []runtime.Object{},
			appliedWorks: []runtime.Object{
				&workapiv1.AppliedManifestWork{
					ObjectMeta: metav1.ObjectMeta{
						Name: "hubhash-test",
					},
					Spec: workapiv1.AppliedManifestWorkSpec{
						ManifestWorkName: "test",
						HubHash:          "hubhash",
						AgentID:          "test-agent",
					},
					Status: workapiv1.AppliedManifestWorkStatus{
						EvictionStartTime: &metav1.Time{
							Time: time.Now().Add(-5 * time.Minute),
						},
					},
				},
			},
			expectedQueueLen: 0, // requeued via AddAfter, not the main queue
			validateAppliedManifestWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
			},
		},
		{
			name:                    "evict appliedmanifestwork after the hub switched",
			appliedManifestWorkName: "hubhash-test",
			hubHash:                 "hubhash-new",
			agentID:                 "test-agent",
			works: []runtime.Object{
				&workapiv1.ManifestWork{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "test",
					},
				},
			},
			appliedWorks: []runtime.Object{
				&workapiv1.AppliedManifestWork{
					ObjectMeta: metav1.ObjectMeta{
						Name: "hubhash-test",
					},
					Spec: workapiv1.AppliedManifestWorkSpec{
						ManifestWorkName: "test",
						HubHash:          "hubhash",
						AgentID:          "test-agent",
					},
				},
			},
			validateAppliedManifestWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
			},
		},
		{
			name:                    "delete appliedmanifestwork after eviction grace period ",
			appliedManifestWorkName: "hubhash-test",
			hubHash:                 "hubhash-new",
			agentID:                 "test-agent",
			evictionGracePeriod:     10 * time.Minute,
			works: []runtime.Object{
				&workapiv1.ManifestWork{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "test",
					},
				},
			},
			appliedWorks: []runtime.Object{
				&workapiv1.AppliedManifestWork{
					ObjectMeta: metav1.ObjectMeta{
						Name: "hubhash-test",
					},
					Spec: workapiv1.AppliedManifestWorkSpec{
						ManifestWorkName: "test",
						HubHash:          "hubhash",
						AgentID:          "test-agent",
					},
					Status: workapiv1.AppliedManifestWorkStatus{
						EvictionStartTime: &metav1.Time{
							Time: time.Now().Add(-10 * time.Minute),
						},
					},
				},
			},
			validateAppliedManifestWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "delete")
			},
		},
		{
			name:                    "stop to evicte appliedmanifestwork when its relating manifestwork is recreated on the hub",
			appliedManifestWorkName: "hubhash-test",
			hubHash:                 "hubhash",
			agentID:                 "test-agent",
			works: []runtime.Object{
				&workapiv1.ManifestWork{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "test",
					},
				},
			},
			appliedWorks: []runtime.Object{
				&workapiv1.AppliedManifestWork{
					ObjectMeta: metav1.ObjectMeta{
						Name: "hubhash-test",
					},
					Spec: workapiv1.AppliedManifestWorkSpec{
						ManifestWorkName: "test",
						HubHash:          "hubhash",
						AgentID:          "test-agent",
					},
					Status: workapiv1.AppliedManifestWorkStatus{
						EvictionStartTime: &metav1.Time{
							Time: time.Now(),
						},
					},
				},
			},
			validateAppliedManifestWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
			},
		},
		{
			name:                    "requeue eviction appliedmanifestwork",
			appliedManifestWorkName: "hubhash-test",
			hubHash:                 "hubhash",
			agentID:                 "test-agent",
			evictionGracePeriod:     10 * time.Minute,
			works:                   []runtime.Object{},
			appliedWorks: []runtime.Object{
				&workapiv1.AppliedManifestWork{
					ObjectMeta: metav1.ObjectMeta{
						Name: "hubhash-test",
					},
					Spec: workapiv1.AppliedManifestWorkSpec{
						ManifestWorkName: "test",
						HubHash:          "hubhash",
						AgentID:          "test-agent",
					},
					Status: workapiv1.AppliedManifestWorkStatus{
						EvictionStartTime: &metav1.Time{
							Time: time.Now().Add(-5 * time.Minute),
						},
					},
				},
			},
			expectedQueueLen:                   0, // Item is added to delayed queue via AddAfter, not the main queue
			validateAppliedManifestWorkActions: testingcommon.AssertNoActions,
		},
		{
			name:                    "disable the eviction",
			appliedManifestWorkName: "hubhash-test",
			hubHash:                 "hubhash",
			agentID:                 "test-agent",
			evictionGracePeriod:     EvictionGracePeriodBound,
			works:                   []runtime.Object{},
			appliedWorks: []runtime.Object{
				&workapiv1.AppliedManifestWork{
					ObjectMeta: metav1.ObjectMeta{
						Name: "hubhash-test",
					},
					Spec: workapiv1.AppliedManifestWorkSpec{
						ManifestWorkName: "test",
						HubHash:          "hubhash",
						AgentID:          "test-agent",
					},
				},
			},
			validateAppliedManifestWorkActions: testingcommon.AssertNoActions,
		},
		{
			name:                    "disable the eviction and remove the EvictionStartTime",
			appliedManifestWorkName: "hubhash-test",
			hubHash:                 "hubhash",
			agentID:                 "test-agent",
			evictionGracePeriod:     EvictionGracePeriodBound,
			works:                   []runtime.Object{},
			appliedWorks: []runtime.Object{
				&workapiv1.AppliedManifestWork{
					ObjectMeta: metav1.ObjectMeta{
						Name: "hubhash-test",
					},
					Spec: workapiv1.AppliedManifestWorkSpec{
						ManifestWorkName: "test",
						HubHash:          "hubhash",
						AgentID:          "test-agent",
					},
					Status: workapiv1.AppliedManifestWorkStatus{
						EvictionStartTime: &metav1.Time{
							Time: time.Now().Add(-5 * time.Minute),
						},
					},
				},
			},
			validateAppliedManifestWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeClient := fakeworkclient.NewSimpleClientset(c.appliedWorks...)
			informerFactory := workinformers.NewSharedInformerFactory(fakeClient, 5*time.Minute)
			for _, work := range c.works {
				if err := informerFactory.Work().V1().ManifestWorks().Informer().GetStore().Add(work); err != nil {
					t.Fatal(err)
				}
			}
			for _, appliedWork := range c.appliedWorks {
				if err := informerFactory.Work().V1().AppliedManifestWorks().Informer().GetStore().Add(appliedWork); err != nil {
					t.Fatal(err)
				}
			}

			synced := c.manifestWorkInformerSynced
			if synced == nil {
				synced = func() bool { return true }
			}
			controller := &unmanagedAppliedWorkController{
				manifestWorkLister:        informerFactory.Work().V1().ManifestWorks().Lister().ManifestWorks("test"),
				appliedManifestWorkClient: fakeClient.WorkV1().AppliedManifestWorks(),
				patcher: patcher.NewPatcher[
					*workapiv1.AppliedManifestWork, workapiv1.AppliedManifestWorkSpec, workapiv1.AppliedManifestWorkStatus](
					fakeClient.WorkV1().AppliedManifestWorks()),
				appliedManifestWorkLister:  informerFactory.Work().V1().AppliedManifestWorks().Lister(),
				manifestWorkInformerSynced: synced,
				hubHash:                    c.hubHash,
				agentID:                    c.agentID,
				evictionGracePeriod:        c.evictionGracePeriod,
			}

			controllerContext := testingcommon.NewFakeSyncContext(t, c.appliedManifestWorkName)
			if err := controller.sync(context.TODO(), controllerContext, c.appliedManifestWorkName); err != nil {
				t.Errorf("Expect no sync error, but got %v", err)
			}

			appliedWorkAction := fakeClient.Actions()
			c.validateAppliedManifestWorkActions(t, appliedWorkAction)

			queueLen := controllerContext.Queue().Len()
			if queueLen != c.expectedQueueLen {
				t.Errorf("expected %d, but %d", c.expectedQueueLen, queueLen)
			}
		})
	}
}

func TestShouldWarnMassEviction(t *testing.T) {
	cases := []struct {
		name    string
		stamped int
		total   int
		want    bool
	}{
		{name: "empty", stamped: 0, total: 0, want: false},
		{name: "below min count", stamped: 9, total: 9, want: false},
		{name: "min count but below ratio", stamped: 10, total: 30, want: false},
		{name: "min count at ratio", stamped: 10, total: 20, want: true},
		{name: "production-like mass stamp", stamped: 353, total: 402, want: true},
		{name: "all stamped small estate", stamped: 5, total: 5, want: false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := shouldWarnMassEviction(c.stamped, c.total); got != c.want {
				t.Fatalf("shouldWarnMassEviction(%d,%d)=%v, want %v", c.stamped, c.total, got, c.want)
			}
		})
	}
}

func TestCountAgentEvictionStamps(t *testing.T) {
	fakeClient := fakeworkclient.NewSimpleClientset(
		&workapiv1.AppliedManifestWork{
			ObjectMeta: metav1.ObjectMeta{Name: "a"},
			Spec:       workapiv1.AppliedManifestWorkSpec{AgentID: "agent-1"},
			Status:     workapiv1.AppliedManifestWorkStatus{EvictionStartTime: &metav1.Time{Time: time.Now()}},
		},
		&workapiv1.AppliedManifestWork{
			ObjectMeta: metav1.ObjectMeta{Name: "b"},
			Spec:       workapiv1.AppliedManifestWorkSpec{AgentID: "agent-1"},
		},
		&workapiv1.AppliedManifestWork{
			ObjectMeta: metav1.ObjectMeta{Name: "c"},
			Spec:       workapiv1.AppliedManifestWorkSpec{AgentID: "other"},
			Status:     workapiv1.AppliedManifestWorkStatus{EvictionStartTime: &metav1.Time{Time: time.Now()}},
		},
	)
	informerFactory := workinformers.NewSharedInformerFactory(fakeClient, 5*time.Minute)
	for _, obj := range []runtime.Object{
		&workapiv1.AppliedManifestWork{
			ObjectMeta: metav1.ObjectMeta{Name: "a"},
			Spec:       workapiv1.AppliedManifestWorkSpec{AgentID: "agent-1"},
			Status:     workapiv1.AppliedManifestWorkStatus{EvictionStartTime: &metav1.Time{Time: time.Now()}},
		},
		&workapiv1.AppliedManifestWork{
			ObjectMeta: metav1.ObjectMeta{Name: "b"},
			Spec:       workapiv1.AppliedManifestWorkSpec{AgentID: "agent-1"},
		},
		&workapiv1.AppliedManifestWork{
			ObjectMeta: metav1.ObjectMeta{Name: "c"},
			Spec:       workapiv1.AppliedManifestWorkSpec{AgentID: "other"},
			Status:     workapiv1.AppliedManifestWorkStatus{EvictionStartTime: &metav1.Time{Time: time.Now()}},
		},
	} {
		if err := informerFactory.Work().V1().AppliedManifestWorks().Informer().GetStore().Add(obj); err != nil {
			t.Fatal(err)
		}
	}

	stamped, total := countAgentEvictionStamps(informerFactory.Work().V1().AppliedManifestWorks().Lister(), "agent-1")
	if stamped != 1 || total != 2 {
		t.Fatalf("countAgentEvictionStamps=(%d,%d), want (1,2)", stamped, total)
	}
}

func TestWarnIfMassEviction(t *testing.T) {
	const agentID = "test-agent"
	now := time.Now()
	appliedWorks := make([]runtime.Object, 0, 20)
	for i := 0; i < 20; i++ {
		aw := &workapiv1.AppliedManifestWork{
			ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("hubhash-w-%d", i)},
			Spec: workapiv1.AppliedManifestWorkSpec{
				ManifestWorkName: fmt.Sprintf("w-%d", i),
				HubHash:          "hubhash",
				AgentID:          agentID,
			},
		}
		// 9 already stamped; the sync under test stamps the 10th → 10/20 at ratio.
		if i < 9 {
			aw.Status.EvictionStartTime = &metav1.Time{Time: now}
		}
		appliedWorks = append(appliedWorks, aw)
	}

	fakeClient := fakeworkclient.NewSimpleClientset(appliedWorks...)
	informerFactory := workinformers.NewSharedInformerFactory(fakeClient, 5*time.Minute)
	for _, appliedWork := range appliedWorks {
		if err := informerFactory.Work().V1().AppliedManifestWorks().Informer().GetStore().Add(appliedWork); err != nil {
			t.Fatal(err)
		}
	}

	controller := &unmanagedAppliedWorkController{
		manifestWorkLister:        informerFactory.Work().V1().ManifestWorks().Lister().ManifestWorks("test"),
		appliedManifestWorkClient: fakeClient.WorkV1().AppliedManifestWorks(),
		patcher: patcher.NewPatcher[
			*workapiv1.AppliedManifestWork, workapiv1.AppliedManifestWorkSpec, workapiv1.AppliedManifestWorkStatus](
			fakeClient.WorkV1().AppliedManifestWorks()),
		appliedManifestWorkLister:  informerFactory.Work().V1().AppliedManifestWorks().Lister(),
		manifestWorkInformerSynced: func() bool { return true },
		hubHash:                    "hubhash",
		agentID:                    agentID,
		evictionGracePeriod:        time.Hour,
	}

	controllerContext := testingcommon.NewFakeSyncContext(t, "hubhash-w-9")
	if err := controller.sync(context.TODO(), controllerContext, "hubhash-w-9"); err != nil {
		t.Fatalf("sync: %v", err)
	}
	testingcommon.AssertActions(t, fakeClient.Actions(), "patch")
	if controller.lastMassEvictionWarnAt.IsZero() {
		t.Fatal("expected mass eviction warning to be recorded")
	}

	// Cooldown should suppress a second warning immediately.
	lastWarn := controller.lastMassEvictionWarnAt
	controllerContext2 := testingcommon.NewFakeSyncContext(t, "hubhash-w-10")
	// Ensure w-10 is still unstamped in the store so sync will stamp it again.
	if err := controller.sync(context.TODO(), controllerContext2, "hubhash-w-10"); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if !controller.lastMassEvictionWarnAt.Equal(lastWarn) {
		t.Fatalf("expected warning cooldown to hold lastMassEvictionWarnAt=%v, got %v", lastWarn, controller.lastMassEvictionWarnAt)
	}
}
