package finalizercontroller

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	fakeworkclient "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/work/pkg/spoke/spoketesting"
)

func TestSyncUnamanagedAppliedWork(t *testing.T) {
	cases := []struct {
		name                               string
		workName                           string
		hubHash                            string
		agentID                            string
		works                              []runtime.Object
		appliedWorks                       []runtime.Object
		validateAppliedManifestWorkActions func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:     "delete applied work if unmanaged",
			workName: "test",
			hubHash:  "hubhash1",
			agentID:  "test-agent",
			works: []runtime.Object{
				&workapiv1.ManifestWork{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "test",
					},
					Status: workapiv1.ManifestWorkStatus{
						Conditions: []metav1.Condition{
							{
								Type:   workapiv1.WorkApplied,
								Status: metav1.ConditionTrue,
							},
						},
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
				&workapiv1.AppliedManifestWork{
					ObjectMeta: metav1.ObjectMeta{
						Name: "hubhash1-test",
					},
					Spec: workapiv1.AppliedManifestWorkSpec{
						ManifestWorkName: "test",
						HubHash:          "hubhash1",
						AgentID:          "test-agent",
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
			name:     "no action if the work is not applied",
			workName: "test",
			hubHash:  "hubhash1",
			agentID:  "test-agent",
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
				&workapiv1.AppliedManifestWork{
					ObjectMeta: metav1.ObjectMeta{
						Name: "hubhash1-test",
					},
					Spec: workapiv1.AppliedManifestWorkSpec{
						ManifestWorkName: "test",
						HubHash:          "hubhash1",
						AgentID:          "test-agent",
					},
				},
			},
			validateAppliedManifestWorkActions: noAction,
		},
		{
			name:     "no action for different AgentID",
			workName: "test",
			hubHash:  "hubhash1",
			agentID:  "test-agent",
			works: []runtime.Object{
				&workapiv1.ManifestWork{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "test",
					},
					Status: workapiv1.ManifestWorkStatus{
						Conditions: []metav1.Condition{
							{
								Type:   workapiv1.WorkApplied,
								Status: metav1.ConditionTrue,
							},
						},
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
						AgentID:          "test-agent1",
					},
				},
				&workapiv1.AppliedManifestWork{
					ObjectMeta: metav1.ObjectMeta{
						Name: "hubhash1-test",
					},
					Spec: workapiv1.AppliedManifestWorkSpec{
						ManifestWorkName: "test",
						HubHash:          "hubhash1",
						AgentID:          "test-agent",
					},
				},
			},
			validateAppliedManifestWorkActions: noAction,
		},
		{
			name:     "no action for different work",
			workName: "test",
			hubHash:  "hubhash1",
			agentID:  "test-agent",
			works: []runtime.Object{
				&workapiv1.ManifestWork{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "test",
					},
					Status: workapiv1.ManifestWorkStatus{
						Conditions: []metav1.Condition{
							{
								Type:   workapiv1.WorkApplied,
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			appliedWorks: []runtime.Object{
				&workapiv1.AppliedManifestWork{
					ObjectMeta: metav1.ObjectMeta{
						Name: "hubhash-test1",
					},
					Spec: workapiv1.AppliedManifestWorkSpec{
						ManifestWorkName: "test1",
						HubHash:          "hubhash",
						AgentID:          "test-agent",
					},
				},
				&workapiv1.AppliedManifestWork{
					ObjectMeta: metav1.ObjectMeta{
						Name: "hubhash1-test",
					},
					Spec: workapiv1.AppliedManifestWorkSpec{
						ManifestWorkName: "test",
						HubHash:          "hubhash1",
						AgentID:          "test-agent",
					},
				},
			},
			validateAppliedManifestWorkActions: noAction,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeClient := fakeworkclient.NewSimpleClientset(c.appliedWorks...)
			informerFactory := workinformers.NewSharedInformerFactory(fakeClient, 5*time.Minute)
			err := informerFactory.Work().V1().AppliedManifestWorks().Informer().AddIndexers(cache.Indexers{
				byWorkNameAndAgentID: indexByWorkNameAndAgentID,
			})
			if err != nil {
				t.Fatal(err)
			}
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

			controller := &UnManagedAppliedWorkController{
				manifestWorkLister:         informerFactory.Work().V1().ManifestWorks().Lister().ManifestWorks("test"),
				appliedManifestWorkClient:  fakeClient.WorkV1().AppliedManifestWorks(),
				appliedManifestWorkLister:  informerFactory.Work().V1().AppliedManifestWorks().Lister(),
				appliedManifestWorkIndexer: informerFactory.Work().V1().AppliedManifestWorks().Informer().GetIndexer(),
				hubHash:                    c.hubHash,
				agentID:                    c.agentID,
			}

			controllerContext := spoketesting.NewFakeSyncContext(t, c.hubHash+"-"+c.workName)
			err = controller.sync(context.TODO(), controllerContext)
			if err != nil {
				t.Errorf("Expect no sync error, but got %v", err)
			}

			appliedWorkAction := fakeClient.Actions()
			c.validateAppliedManifestWorkActions(t, appliedWorkAction)
		})
	}
}
