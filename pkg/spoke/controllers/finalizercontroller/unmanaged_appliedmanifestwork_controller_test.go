package finalizercontroller

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/util/workqueue"

	fakeworkclient "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/work/pkg/spoke/spoketesting"
)

func TestSyncUnamanagedAppliedWork(t *testing.T) {
	cases := []struct {
		name                               string
		appliedManifestWorkName            string
		hubHash                            string
		agentID                            string
		evictionGracePeriod                time.Duration
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
			validateAppliedManifestWorkActions: noAction,
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
				if len(actions) != 1 {
					t.Errorf("Expect 1 actions on appliedmanifestwork, but have %d", len(actions))
				}

				spoketesting.AssertAction(t, actions[0], "patch")
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
				if len(actions) != 1 {
					t.Errorf("Expect 1 actions on appliedmanifestwork, but have %d", len(actions))
				}

				spoketesting.AssertAction(t, actions[0], "patch")
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
				if len(actions) != 1 {
					t.Errorf("Expect 1 actions on appliedmanifestwork, but have %d", len(actions))
				}

				spoketesting.AssertAction(t, actions[0], "delete")
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
				if len(actions) != 1 {
					t.Errorf("Expect 1 actions on appliedmanifestwork, but have %d", len(actions))
				}

				spoketesting.AssertAction(t, actions[0], "patch")
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
			expectedQueueLen:                   1,
			validateAppliedManifestWorkActions: noAction,
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

			controller := &unmanagedAppliedWorkController{
				manifestWorkLister:        informerFactory.Work().V1().ManifestWorks().Lister().ManifestWorks("test"),
				appliedManifestWorkClient: fakeClient.WorkV1().AppliedManifestWorks(),
				appliedManifestWorkLister: informerFactory.Work().V1().AppliedManifestWorks().Lister(),
				hubHash:                   c.hubHash,
				agentID:                   c.agentID,
				evictionGracePeriod:       c.evictionGracePeriod,
				rateLimiter:               workqueue.NewItemExponentialFailureRateLimiter(0, c.evictionGracePeriod),
			}

			controllerContext := spoketesting.NewFakeSyncContext(t, c.appliedManifestWorkName)
			if err := controller.sync(context.TODO(), controllerContext); err != nil {
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
