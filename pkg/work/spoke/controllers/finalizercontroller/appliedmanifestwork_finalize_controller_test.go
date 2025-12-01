package finalizercontroller

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
	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	"open-cluster-management.io/ocm/pkg/work/helper"
	"open-cluster-management.io/ocm/pkg/work/spoke/spoketesting"
)

func TestFinalize(t *testing.T) {
	uid := types.UID("test")
	appliedWork := spoketesting.NewAppliedManifestWork("test", 0, uid)
	owner := helper.NewAppliedManifestWorkOwner(appliedWork)

	cases := []struct {
		name                               string
		existingFinalizers                 []string
		existingResources                  []runtime.Object
		resourcesToRemove                  []workapiv1.AppliedManifestResourceMeta
		terminated                         bool
		validateAppliedManifestWorkActions func(t *testing.T, actions []clienttesting.Action)
		validateDynamicActions             func(t *testing.T, actions []clienttesting.Action)
		expectedQueueLen                   int
	}{
		{
			name:                               "skip when not delete",
			existingFinalizers:                 []string{workapiv1.ManifestWorkFinalizer},
			validateAppliedManifestWorkActions: testingcommon.AssertNoActions,
			validateDynamicActions:             testingcommon.AssertNoActions,
		},
		{
			name:                               "skip when finalizer gone",
			terminated:                         true,
			existingFinalizers:                 []string{"other-finalizer"},
			validateAppliedManifestWorkActions: testingcommon.AssertNoActions,
			validateDynamicActions:             testingcommon.AssertNoActions,
		},
		{
			name:               "get resources and remove finalizer",
			terminated:         true,
			existingFinalizers: []string{"a", workapiv1.AppliedManifestWorkFinalizer, "b"},
			resourcesToRemove: []workapiv1.AppliedManifestResourceMeta{
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "g1", Resource: "r1", Namespace: "", Name: "n1"}},
				{Version: "v2", ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "g2", Resource: "r2", Namespace: "ns2", Name: "n2"}},
				{Version: "v3", ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "g3", Resource: "r3", Namespace: "ns3", Name: "n3"}},
				{Version: "v4", ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "g4", Resource: "r4", Namespace: "", Name: "n4"}},
			},
			validateAppliedManifestWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				p := actions[0].(clienttesting.PatchActionImpl).Patch
				work := &workapiv1.AppliedManifestWork{}
				if err := json.Unmarshal(p, work); err != nil {
					t.Fatal(err)
				}
				if len(work.Status.AppliedResources) != 0 {
					t.Fatal(spew.Sdump(actions[0]))
				}
			},
			validateDynamicActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 4 {
					t.Fatal(spew.Sdump(actions))
				}

				action := actions[0].(clienttesting.GetActionImpl)
				resource, namespace, name := action.GetResource(), action.GetNamespace(), action.GetName()
				if !reflect.DeepEqual(resource, schema.GroupVersionResource{Group: "g1", Version: "v1", Resource: "r1"}) || namespace != "" || name != "n1" {
					t.Fatal(spew.Sdump(actions))
				}
				action = actions[1].(clienttesting.GetActionImpl)
				resource, namespace, name = action.GetResource(), action.GetNamespace(), action.GetName()
				if !reflect.DeepEqual(resource, schema.GroupVersionResource{Group: "g2", Version: "v2", Resource: "r2"}) || namespace != "ns2" || name != "n2" {
					t.Fatal(spew.Sdump(actions))
				}
				action = actions[2].(clienttesting.GetActionImpl)
				resource, namespace, name = action.GetResource(), action.GetNamespace(), action.GetName()
				if !reflect.DeepEqual(resource, schema.GroupVersionResource{Group: "g3", Version: "v3", Resource: "r3"}) || namespace != "ns3" || name != "n3" {
					t.Fatal(spew.Sdump(actions))
				}
				action = actions[3].(clienttesting.GetActionImpl)
				resource, namespace, name = action.GetResource(), action.GetNamespace(), action.GetName()
				if !reflect.DeepEqual(resource, schema.GroupVersionResource{Group: "g4", Version: "v4", Resource: "r4"}) || namespace != "" || name != "n4" {
					t.Fatal(spew.Sdump(actions))
				}
			},
		},
		{
			name:               "requeue work when deleting resources are still visible",
			terminated:         true,
			existingFinalizers: []string{workapiv1.AppliedManifestWorkFinalizer},
			existingResources: []runtime.Object{
				testingcommon.NewUnstructuredSecret("ns1", "n1", true, "ns1-n1", *owner),
				testingcommon.NewUnstructuredSecret("ns2", "n2", true, "ns2-n2", *owner),
			},
			resourcesToRemove: []workapiv1.AppliedManifestResourceMeta{
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "secrets", Namespace: "ns1", Name: "n1"}, UID: "ns1-n1"},
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "secrets", Namespace: "ns2", Name: "n2"}, UID: "ns2-n2"},
			},
			validateAppliedManifestWorkActions: testingcommon.AssertNoActions,
			validateDynamicActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 2 {
					t.Fatal(spew.Sdump(actions))
				}

				action := actions[0].(clienttesting.GetAction)
				resource, namespace, name := action.GetResource(), action.GetNamespace(), action.GetName()
				if !reflect.DeepEqual(resource, schema.GroupVersionResource{Version: "v1", Resource: "secrets"}) || namespace != "ns1" || name != "n1" {
					t.Fatal(spew.Sdump(actions[0]))
				}
				action = actions[1].(clienttesting.GetAction)
				resource, namespace, name = action.GetResource(), action.GetNamespace(), action.GetName()
				if !reflect.DeepEqual(resource, schema.GroupVersionResource{Version: "v1", Resource: "secrets"}) || namespace != "ns2" || name != "n2" {
					t.Fatal(spew.Sdump(actions[0]))
				}
			},
			expectedQueueLen: 1,
		},
		{
			name:               "ignore re-created resource and remove finalizer",
			terminated:         true,
			existingFinalizers: []string{workapiv1.AppliedManifestWorkFinalizer},
			existingResources: []runtime.Object{
				testingcommon.NewUnstructuredSecret("ns1", "n1", false, "ns1-n1", *owner),
			},
			resourcesToRemove: []workapiv1.AppliedManifestResourceMeta{
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "secrets", Namespace: "ns1", Name: "n1"}, UID: "n1"},
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "secrets", Namespace: "ns2", Name: "n2"}, UID: "n2"},
			},
			validateAppliedManifestWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				p := actions[0].(clienttesting.PatchActionImpl).Patch
				work := &workapiv1.AppliedManifestWork{}
				if err := json.Unmarshal(p, work); err != nil {
					t.Fatal(err)
				}
				if len(work.Status.AppliedResources) != 0 {
					t.Fatal(spew.Sdump(actions[0]))
				}
			},
			validateDynamicActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 2 {
					t.Fatal(spew.Sdump(actions))
				}

				action := actions[0].(clienttesting.GetAction)
				resource, namespace, name := action.GetResource(), action.GetNamespace(), action.GetName()
				if !reflect.DeepEqual(resource, schema.GroupVersionResource{Version: "v1", Resource: "secrets"}) || namespace != "ns1" || name != "n1" {
					t.Fatal(spew.Sdump(actions[0]))
				}

				action = actions[1].(clienttesting.GetAction)
				resource, namespace, name = action.GetResource(), action.GetNamespace(), action.GetName()
				if !reflect.DeepEqual(resource, schema.GroupVersionResource{Version: "v1", Resource: "secrets"}) || namespace != "ns2" || name != "n2" {
					t.Fatal(spew.Sdump(actions[0]))
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			testingWork := appliedWork.DeepCopy()
			testingWork.Finalizers = c.existingFinalizers
			if c.terminated {
				now := metav1.Now()
				testingWork.DeletionTimestamp = &now
			}
			testingWork.Status.AppliedResources = append(testingWork.Status.AppliedResources, c.resourcesToRemove...)

			fakeDynamicClient := fakedynamic.NewSimpleDynamicClient(runtime.NewScheme(), c.existingResources...)
			fakeClient := fakeworkclient.NewSimpleClientset(testingWork)
			controller := AppliedManifestWorkFinalizeController{
				patcher: patcher.NewPatcher[
					*workapiv1.AppliedManifestWork, workapiv1.AppliedManifestWorkSpec, workapiv1.AppliedManifestWorkStatus](
					fakeClient.WorkV1().AppliedManifestWorks()),
				spokeDynamicClient: fakeDynamicClient,
				rateLimiter:        workqueue.NewTypedItemExponentialFailureRateLimiter[string](0, 1*time.Second),
			}

			controllerContext := testingcommon.NewFakeSyncContext(t, testingWork.Name)
			err := controller.syncAppliedManifestWork(context.TODO(), controllerContext, testingWork)
			if err != nil {
				t.Fatal(err)
			}
			c.validateAppliedManifestWorkActions(t, fakeClient.Actions())
			c.validateDynamicActions(t, fakeDynamicClient.Actions())

			queueLen := controllerContext.Queue().Len()
			if queueLen != c.expectedQueueLen {
				t.Errorf("expected %d, but %d", c.expectedQueueLen, queueLen)
			}
		})
	}
}
