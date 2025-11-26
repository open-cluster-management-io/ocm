package manifestcontroller

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/util/workqueue"

	fakeworkclient "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	"open-cluster-management.io/ocm/pkg/work/helper"
	"open-cluster-management.io/ocm/pkg/work/spoke/spoketesting"
)

func TestSyncManifestWork(t *testing.T) {
	uid := types.UID("test")
	appliedWork := spoketesting.NewAppliedManifestWork("test", 0, uid)
	owner := helper.NewAppliedManifestWorkOwner(appliedWork)

	cases := []struct {
		name                               string
		applied                            bool
		existingResources                  []runtime.Object
		appliedResources                   []workapiv1.AppliedManifestResourceMeta
		appliedResults                     []applyResult
		validateAppliedManifestWorkActions func(t *testing.T, actions []clienttesting.Action)
		expectedDeleteActions              []clienttesting.DeleteActionImpl
		expectedQueueLen                   int
	}{
		{
			name: "skip if the manifestwork has not been applied yet",
			validateAppliedManifestWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) > 0 {
					t.Fatal(spew.Sdump(actions))
				}
			},
		},
		{
			name:    "skip when no applied resource changed",
			applied: true,
			existingResources: []runtime.Object{
				testingcommon.NewUnstructuredSecret("ns1", "n1", false, "ns1-n1", *owner),
			},
			appliedResources: []workapiv1.AppliedManifestResourceMeta{
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "secrets", Namespace: "ns1", Name: "n1"}, UID: "ns1-n1"},
			},
			appliedResults: []applyResult{
				{
					resourceMeta: workapiv1.ManifestResourceMeta{Resource: "secrets", Version: "v1", Namespace: "ns1", Name: "n1"},
					Result:       testingcommon.NewUnstructuredSecret("ns1", "n1", false, "ns1-n1", *owner),
				},
			},
			validateAppliedManifestWorkActions: testingcommon.AssertNoActions,
		},
		{
			name:    "delete untracked resources",
			applied: true,
			existingResources: []runtime.Object{
				testingcommon.NewUnstructuredSecret("ns1", "n1", false, "ns1-n1", *owner),
				testingcommon.NewUnstructuredSecret("ns2", "n2", false, "ns2-n2", *owner),
				testingcommon.NewUnstructuredSecret("ns3", "n3", false, "ns3-n3", *owner),
				testingcommon.NewUnstructuredSecret("ns4", "n4", false, "ns4-n4", *owner),
				testingcommon.NewUnstructuredSecret("ns5", "n5", false, "ns5-n5", *owner),
				testingcommon.NewUnstructuredSecret("ns6", "n6", false, "ns6-n6", *owner),
			},
			appliedResources: []workapiv1.AppliedManifestResourceMeta{
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "", Resource: "secrets", Namespace: "ns1", Name: "n1"}, UID: "ns1-n1"},
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "", Resource: "secrets", Namespace: "ns2", Name: "n2"}, UID: "ns2-n2"},
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "", Resource: "secrets", Namespace: "ns3", Name: "n3"}, UID: "ns3-n3"},
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "", Resource: "secrets", Namespace: "ns4", Name: "n4"}, UID: "ns4-n4"},
			},
			appliedResults: []applyResult{
				{
					resourceMeta: workapiv1.ManifestResourceMeta{Version: "v1", Resource: "secrets", Namespace: "ns1", Name: "n1"},
					Result:       testingcommon.NewUnstructuredSecret("ns1", "n1", false, "ns1-n1", *owner),
				},
				{
					resourceMeta: workapiv1.ManifestResourceMeta{Version: "v1", Resource: "secrets", Namespace: "ns2", Name: "n2"},
					Result:       testingcommon.NewUnstructuredSecret("ns2", "n2", false, "ns2-n2", *owner),
				},
				{
					resourceMeta: workapiv1.ManifestResourceMeta{Version: "v1", Resource: "secrets", Namespace: "ns5", Name: "n5"},
					Result:       testingcommon.NewUnstructuredSecret("ns5", "n5", false, "ns5-n5", *owner),
				},
				{
					resourceMeta: workapiv1.ManifestResourceMeta{Version: "v1", Resource: "secrets", Namespace: "ns6", Name: "n6"},
					Result:       testingcommon.NewUnstructuredSecret("ns6", "n6", false, "ns6-n6", *owner),
				},
			},
			validateAppliedManifestWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				p := actions[0].(clienttesting.PatchActionImpl).Patch
				work := &workapiv1.AppliedManifestWork{}
				if err := json.Unmarshal(p, work); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(work.Status.AppliedResources, []workapiv1.AppliedManifestResourceMeta{
					{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "", Resource: "secrets", Namespace: "ns1", Name: "n1"}, UID: "ns1-n1"},
					{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "", Resource: "secrets", Namespace: "ns2", Name: "n2"}, UID: "ns2-n2"},
					{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "", Resource: "secrets", Namespace: "ns3", Name: "n3"}, UID: "ns3-n3"},
					{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "", Resource: "secrets", Namespace: "ns4", Name: "n4"}, UID: "ns4-n4"},
					{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "", Resource: "secrets", Namespace: "ns5", Name: "n5"}, UID: "ns5-n5"},
					{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "", Resource: "secrets", Namespace: "ns6", Name: "n6"}, UID: "ns6-n6"},
				}) {
					t.Fatal(spew.Sdump(actions))
				}
			},
			expectedDeleteActions: []clienttesting.DeleteActionImpl{
				clienttesting.NewDeleteAction(schema.GroupVersionResource{Group: "", Version: "v1", Resource: "secrets"}, "ns3", "n3"),
				clienttesting.NewDeleteAction(schema.GroupVersionResource{Group: "", Version: "v1", Resource: "secrets"}, "ns4", "n4"),
			},
			expectedQueueLen: 1,
		},
		{
			name:    "requeue work when applied resource for stale manifest is deleting",
			applied: true,
			existingResources: []runtime.Object{
				testingcommon.NewUnstructuredSecret("ns1", "n1", false, "ns1-n1", *owner),
				testingcommon.NewUnstructuredSecret("ns2", "n2", false, "ns2-n2", *owner),
				testingcommon.NewUnstructuredSecret("ns3", "n3", true, "ns3-n3", *owner),
			},
			appliedResources: []workapiv1.AppliedManifestResourceMeta{
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "secrets", Namespace: "ns1", Name: "n1"}, UID: "ns1-n1"},
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "secrets", Namespace: "ns2", Name: "n2"}, UID: "ns2-n2"},
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "secrets", Namespace: "ns3", Name: "n3"}, UID: "ns3-n3"},
			},
			appliedResults: []applyResult{
				{
					resourceMeta: workapiv1.ManifestResourceMeta{Version: "v1", Resource: "secrets", Namespace: "ns1", Name: "n1"},
					Result:       testingcommon.NewUnstructuredSecret("ns1", "n1", false, "ns1-n1", *owner),
				},
				{
					resourceMeta: workapiv1.ManifestResourceMeta{Version: "v1", Resource: "secrets", Namespace: "ns2", Name: "n2"},
					Result:       testingcommon.NewUnstructuredSecret("ns2", "n2", false, "ns2-n2", *owner),
				},
			},
			validateAppliedManifestWorkActions: testingcommon.AssertNoActions,
			expectedQueueLen:                   1,
		},
		{
			name:    "ignore re-created resource",
			applied: true,
			existingResources: []runtime.Object{
				testingcommon.NewUnstructuredSecret("ns3", "n3", false, "ns3-n3-recreated", *owner),
				testingcommon.NewUnstructuredSecret("ns1", "n1", false, "ns1-n1", *owner),
				testingcommon.NewUnstructuredSecret("ns5", "n5", false, "ns5-n5", *owner),
			},
			appliedResources: []workapiv1.AppliedManifestResourceMeta{
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "secrets", Namespace: "ns3", Name: "n3"}, UID: "ns3-n3"},
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "secrets", Namespace: "ns4", Name: "n4"}, UID: "ns4-n4"},
			},
			appliedResults: []applyResult{
				{
					resourceMeta: workapiv1.ManifestResourceMeta{Version: "v1", Resource: "secrets", Namespace: "ns1", Name: "n1"},
					Result:       testingcommon.NewUnstructuredSecret("ns1", "n1", false, "ns1-n1", *owner),
				},
				{
					resourceMeta: workapiv1.ManifestResourceMeta{Version: "v1", Resource: "secrets", Namespace: "ns5", Name: "n5"},
					Result:       testingcommon.NewUnstructuredSecret("ns5", "n5", false, "ns5-n5", *owner),
				},
			},
			validateAppliedManifestWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				p := actions[0].(clienttesting.PatchActionImpl).Patch
				work := &workapiv1.AppliedManifestWork{}
				if err := json.Unmarshal(p, work); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(work.Status.AppliedResources, []workapiv1.AppliedManifestResourceMeta{
					{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "secrets", Namespace: "ns1", Name: "n1"}, UID: "ns1-n1"},
					{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "secrets", Namespace: "ns5", Name: "n5"}, UID: "ns5-n5"},
				}) {
					t.Fatal(spew.Sdump(actions))
				}
			},
		},
		{
			name:    "update resource uid",
			applied: true,
			existingResources: []runtime.Object{
				testingcommon.NewUnstructuredSecret("ns1", "n1", false, "ns1-n1", *owner),
				testingcommon.NewUnstructuredSecret("ns2", "n2", false, "ns2-n2-updated", *owner),
			},
			appliedResources: []workapiv1.AppliedManifestResourceMeta{
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "secrets", Namespace: "ns1", Name: "n1"}, UID: "ns1-n1"},
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "secrets", Namespace: "ns2", Name: "n2"}, UID: "ns2-n2"},
			},
			appliedResults: []applyResult{
				{
					resourceMeta: workapiv1.ManifestResourceMeta{Version: "v1", Resource: "secrets", Namespace: "ns1", Name: "n1"},
					Result:       testingcommon.NewUnstructuredSecret("ns1", "n1", false, "ns1-n1", *owner),
				},
				{
					resourceMeta: workapiv1.ManifestResourceMeta{Version: "v1", Resource: "secrets", Namespace: "ns2", Name: "n2"},
					Result:       testingcommon.NewUnstructuredSecret("ns2", "n2", false, "ns2-n2-updated", *owner),
				},
			},
			validateAppliedManifestWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				p := actions[0].(clienttesting.PatchActionImpl).Patch
				work := &workapiv1.AppliedManifestWork{}
				if err := json.Unmarshal(p, work); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(work.Status.AppliedResources, []workapiv1.AppliedManifestResourceMeta{
					{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "secrets", Namespace: "ns1", Name: "n1"}, UID: "ns1-n1"},
					{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "secrets", Namespace: "ns2", Name: "n2"}, UID: "ns2-n2-updated"},
				}) {
					t.Fatal(work.Status.AppliedResources)
				}
			},
		},
		{
			name:    "resource uid is missing",
			applied: true,
			existingResources: []runtime.Object{
				testingcommon.NewUnstructuredSecret("ns1", "n1", false, "ns1-n1", *owner),
				testingcommon.NewUnstructuredSecret("ns2", "n2", false, "ns2-n2", *owner),
			},
			appliedResources: []workapiv1.AppliedManifestResourceMeta{
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "secrets", Namespace: "ns1", Name: "n1"}, UID: "ns1-n1"},
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "secrets", Namespace: "ns2", Name: "n2"}, UID: "ns2-n2"},
			},
			appliedResults: []applyResult{
				{
					resourceMeta: workapiv1.ManifestResourceMeta{Version: "v1", Resource: "secrets", Namespace: "ns1", Name: "n1"},
					Result:       testingcommon.NewUnstructuredSecret("ns1", "n1", false, "", *owner),
				},
				{
					resourceMeta: workapiv1.ManifestResourceMeta{Version: "v1", Resource: "secrets", Namespace: "ns2", Name: "n2"},
					Result:       testingcommon.NewUnstructuredSecret("ns2", "n2", false, "", *owner),
				},
			},
			validateAppliedManifestWorkActions: testingcommon.AssertNoActions,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			testingWork, _ := spoketesting.NewManifestWork(0)
			testingWork.Finalizers = []string{workapiv1.ManifestWorkFinalizer}
			if c.applied {
				testingWork.Status.Conditions = []metav1.Condition{
					{
						Type:   workapiv1.WorkApplied,
						Status: metav1.ConditionTrue,
					},
				}
			}
			testingAppliedWork := appliedWork.DeepCopy()
			testingAppliedWork.Status.AppliedResources = c.appliedResources

			fakeDynamicClient := fakedynamic.NewSimpleDynamicClient(runtime.NewScheme(), c.existingResources...)
			fakeClient := fakeworkclient.NewSimpleClientset(testingWork, testingAppliedWork)
			informerFactory := workinformers.NewSharedInformerFactory(fakeClient, 5*time.Minute)
			if err := informerFactory.Work().V1().ManifestWorks().Informer().GetStore().Add(testingWork); err != nil {
				t.Fatal(err)
			}

			if err := informerFactory.Work().V1().AppliedManifestWorks().Informer().GetStore().Add(testingAppliedWork); err != nil {
				t.Fatal(err)
			}

			controller := ManifestWorkController{
				manifestWorkLister: informerFactory.Work().V1().ManifestWorks().Lister().ManifestWorks("cluster1"),
				manifestWorkPatcher: patcher.NewPatcher[
					*workapiv1.ManifestWork, workapiv1.ManifestWorkSpec, workapiv1.ManifestWorkStatus](
					fakeClient.WorkV1().ManifestWorks("cluster1")),
				appliedManifestWorkPatcher: patcher.NewPatcher[
					*workapiv1.AppliedManifestWork, workapiv1.AppliedManifestWorkSpec, workapiv1.AppliedManifestWorkStatus](
					fakeClient.WorkV1().AppliedManifestWorks()),
				appliedManifestWorkLister: informerFactory.Work().V1().AppliedManifestWorks().Lister(),
				reconcilers: []workReconcile{
					&testReconciler{
						results: c.appliedResults,
					},
					&appliedManifestWorkReconciler{
						spokeDynamicClient: fakeDynamicClient,
						rateLimiter:        workqueue.NewTypedItemExponentialFailureRateLimiter[string](0, 1*time.Second),
					},
				},
				hubHash: "test",
			}

			controllerContext := testingcommon.NewFakeSyncContext(t, testingWork.Name)
			err := controller.sync(context.TODO(), controllerContext, testingWork.Name)
			if err != nil {
				t.Fatal(err)
			}
			c.validateAppliedManifestWorkActions(t, fakeClient.Actions())

			var deleteActions []clienttesting.DeleteActionImpl
			for _, action := range fakeDynamicClient.Actions() {
				if action.GetVerb() == "delete" {
					deleteActions = append(deleteActions, action.(clienttesting.DeleteActionImpl))
				}
			}
			if len(deleteActions) != len(c.expectedDeleteActions) {
				t.Fatalf("expected %d deleteActions, got %d", len(c.expectedDeleteActions), len(deleteActions))
			}
			for idx, action := range deleteActions {
				if action.GetVerb() != c.expectedDeleteActions[idx].GetVerb() ||
					!reflect.DeepEqual(action.GetResource(), c.expectedDeleteActions[idx].GetResource()) ||
					action.GetName() != c.expectedDeleteActions[idx].GetName() ||
					action.GetNamespace() != c.expectedDeleteActions[idx].GetNamespace() {
					t.Fatalf("expected delete action '%v', got '%v'", c.expectedDeleteActions[idx], action)
				}
			}

			queueLen := controllerContext.Queue().Len()
			if queueLen != c.expectedQueueLen {
				t.Errorf("expected %d, but %d", c.expectedQueueLen, queueLen)
			}
		})
	}
}

type testReconciler struct {
	results []applyResult
}

func (r *testReconciler) reconcile(_ context.Context,
	_ factory.SyncContext,
	mw *workapiv1.ManifestWork,
	amw *workapiv1.AppliedManifestWork,
	_ []applyResult) (*workapiv1.ManifestWork, *workapiv1.AppliedManifestWork, []applyResult, error) {
	return mw, amw, r.results, nil
}
