package appliedmanifestcontroller

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/diff"
	fakeworkclient "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/work/pkg/helper"
	"open-cluster-management.io/work/pkg/spoke/spoketesting"
)

func newManifest(group, version, resource, namespace, name string) workapiv1.ManifestCondition {
	return workapiv1.ManifestCondition{
		ResourceMeta: workapiv1.ManifestResourceMeta{
			Group:     group,
			Version:   version,
			Resource:  resource,
			Namespace: namespace,
			Name:      name,
		},
	}
}

func TestSyncManifestWork(t *testing.T) {
	uid := types.UID("test")
	appliedWork := spoketesting.NewAppliedManifestWork("test", 0, uid)
	owner := helper.NewAppliedManifestWorkOwner(appliedWork)

	cases := []struct {
		name                               string
		existingResources                  []runtime.Object
		appliedResources                   []workapiv1.AppliedManifestResourceMeta
		manifests                          []workapiv1.ManifestCondition
		validateAppliedManifestWorkActions func(t *testing.T, actions []clienttesting.Action)
		expectedDeleteActions              []clienttesting.DeleteActionImpl
		expectedQueueLen                   int
	}{
		{
			name: "skip when no applied resource changed",
			existingResources: []runtime.Object{
				spoketesting.NewUnstructuredSecret("ns1", "n1", false, "ns1-n1", *owner),
			},
			appliedResources: []workapiv1.AppliedManifestResourceMeta{
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "secrets", Namespace: "ns1", Name: "n1"}, UID: "ns1-n1"},
			},
			manifests: []workapiv1.ManifestCondition{newManifest("", "v1", "secrets", "ns1", "n1")},
			validateAppliedManifestWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) > 0 {
					t.Fatal(spew.Sdump(actions))
				}
			},
			expectedDeleteActions: []clienttesting.DeleteActionImpl{},
		},
		{
			name: "delete untracked resources",
			existingResources: []runtime.Object{
				spoketesting.NewUnstructuredSecret("ns1", "n1", false, "ns1-n1", *owner),
				spoketesting.NewUnstructuredSecret("ns2", "n2", false, "ns2-n2", *owner),
				spoketesting.NewUnstructuredSecret("ns3", "n3", false, "ns3-n3", *owner),
				spoketesting.NewUnstructuredSecret("ns4", "n4", false, "ns4-n4", *owner),
				spoketesting.NewUnstructuredSecret("ns5", "n5", false, "ns5-n5", *owner),
				spoketesting.NewUnstructuredSecret("ns6", "n6", false, "ns6-n6", *owner),
			},
			appliedResources: []workapiv1.AppliedManifestResourceMeta{
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "", Resource: "secrets", Namespace: "ns1", Name: "n1"}, UID: "ns1-n1"},
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "", Resource: "secrets", Namespace: "ns2", Name: "n2"}, UID: "ns2-n2"},
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "", Resource: "secrets", Namespace: "ns3", Name: "n3"}, UID: "ns3-n3"},
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "", Resource: "secrets", Namespace: "ns4", Name: "n4"}, UID: "ns4-n4"},
			},
			manifests: []workapiv1.ManifestCondition{
				newManifest("", "v1", "secrets", "ns1", "n1"),
				newManifest("", "v1", "secrets", "ns2", "n2"),
				newManifest("", "v1", "secrets", "ns5", "n5"),
				newManifest("", "v1", "secrets", "ns6", "n6"),
			},
			validateAppliedManifestWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 1 {
					t.Fatal(spew.Sdump(actions))
				}
				work := actions[0].(clienttesting.UpdateAction).GetObject().(*workapiv1.AppliedManifestWork)
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
		},
		{
			name: "requeue work when applied resource for stale manifest is deleting",
			existingResources: []runtime.Object{
				spoketesting.NewUnstructuredSecret("ns1", "n1", false, "ns1-n1", *owner),
				spoketesting.NewUnstructuredSecret("ns2", "n2", false, "ns2-n2", *owner),
				spoketesting.NewUnstructuredSecret("ns3", "n3", true, "ns3-n3", *owner),
			},
			appliedResources: []workapiv1.AppliedManifestResourceMeta{
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "secrets", Namespace: "ns1", Name: "n1"}, UID: "ns1-n1"},
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "secrets", Namespace: "ns2", Name: "n2"}, UID: "ns2-n2"},
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "secrets", Namespace: "ns3", Name: "n3"}, UID: "ns3-n3"},
			},
			manifests: []workapiv1.ManifestCondition{
				newManifest("", "v1", "secrets", "ns1", "n1"),
				newManifest("", "v1", "secrets", "ns2", "n2"),
			},
			validateAppliedManifestWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) > 0 {
					t.Fatal(spew.Sdump(actions))
				}
			},
			expectedDeleteActions: []clienttesting.DeleteActionImpl{},
			expectedQueueLen:      1,
		},
		{
			name: "ignore re-created resource",
			existingResources: []runtime.Object{
				spoketesting.NewUnstructuredSecret("ns3", "n3", false, "ns3-n3-recreated", *owner),
				spoketesting.NewUnstructuredSecret("ns1", "n1", false, "ns1-n1", *owner),
				spoketesting.NewUnstructuredSecret("ns5", "n5", false, "ns5-n5", *owner),
			},
			appliedResources: []workapiv1.AppliedManifestResourceMeta{
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "secrets", Namespace: "ns3", Name: "n3"}, UID: "ns3-n3"},
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "secrets", Namespace: "ns4", Name: "n4"}, UID: "ns4-n4"},
			},
			manifests: []workapiv1.ManifestCondition{
				newManifest("", "v1", "secrets", "ns1", "n1"),
				newManifest("", "v1", "secrets", "ns5", "n5"),
			},
			validateAppliedManifestWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 1 {
					t.Fatal(spew.Sdump(actions))
				}
				work := actions[0].(clienttesting.UpdateAction).GetObject().(*workapiv1.AppliedManifestWork)
				if !reflect.DeepEqual(work.Status.AppliedResources, []workapiv1.AppliedManifestResourceMeta{
					{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "secrets", Namespace: "ns1", Name: "n1"}, UID: "ns1-n1"},
					{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "secrets", Namespace: "ns5", Name: "n5"}, UID: "ns5-n5"},
				}) {
					t.Fatal(spew.Sdump(actions))
				}
			},
			expectedDeleteActions: []clienttesting.DeleteActionImpl{},
		},
		{
			name: "update resource uid",
			existingResources: []runtime.Object{
				spoketesting.NewUnstructuredSecret("ns1", "n1", false, "ns1-n1", *owner),
				spoketesting.NewUnstructuredSecret("ns2", "n2", false, "ns2-n2-updated", *owner),
			},
			appliedResources: []workapiv1.AppliedManifestResourceMeta{
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "secrets", Namespace: "ns1", Name: "n1"}, UID: "ns1-n1"},
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "secrets", Namespace: "ns2", Name: "n2"}, UID: "ns2-n2"},
			},
			manifests: []workapiv1.ManifestCondition{
				newManifest("", "v1", "secrets", "ns1", "n1"),
				newManifest("", "v1", "secrets", "ns2", "n2"),
			},
			validateAppliedManifestWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 1 {
					t.Fatal(spew.Sdump(actions))
				}
				work := actions[0].(clienttesting.UpdateAction).GetObject().(*workapiv1.AppliedManifestWork)
				if !reflect.DeepEqual(work.Status.AppliedResources, []workapiv1.AppliedManifestResourceMeta{
					{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "secrets", Namespace: "ns1", Name: "n1"}, UID: "ns1-n1"},
					{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "secrets", Namespace: "ns2", Name: "n2"}, UID: "ns2-n2-updated"},
				}) {
					t.Fatal(spew.Sdump(actions))
				}
			},
			expectedDeleteActions: []clienttesting.DeleteActionImpl{},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			testingWork, _ := spoketesting.NewManifestWork(0)
			testingAppliedWork := appliedWork.DeepCopy()
			testingAppliedWork.Status.AppliedResources = c.appliedResources
			testingWork.Status.ResourceStatus.Manifests = c.manifests

			fakeDynamicClient := fakedynamic.NewSimpleDynamicClient(runtime.NewScheme(), c.existingResources...)
			fakeClient := fakeworkclient.NewSimpleClientset(testingWork, testingAppliedWork)
			informerFactory := workinformers.NewSharedInformerFactory(fakeClient, 5*time.Minute)
			informerFactory.Work().V1().ManifestWorks().Informer().GetStore().Add(testingWork)
			informerFactory.Work().V1().AppliedManifestWorks().Informer().GetStore().Add(testingAppliedWork)
			controller := AppliedManifestWorkController{
				manifestWorkClient:        fakeClient.WorkV1().ManifestWorks(testingWork.Namespace),
				manifestWorkLister:        informerFactory.Work().V1().ManifestWorks().Lister().ManifestWorks("cluster1"),
				appliedManifestWorkClient: fakeClient.WorkV1().AppliedManifestWorks(),
				appliedManifestWorkLister: informerFactory.Work().V1().AppliedManifestWorks().Lister(),
				spokeDynamicClient:        fakeDynamicClient,
				hubHash:                   "test",
				rateLimiter:               workqueue.NewItemExponentialFailureRateLimiter(0, 1*time.Second),
			}

			controllerContext := spoketesting.NewFakeSyncContext(t, testingWork.Name)
			err := controller.sync(context.TODO(), controllerContext)
			if err != nil {
				t.Fatal(err)
			}
			c.validateAppliedManifestWorkActions(t, fakeClient.Actions())

			deleteActions := []clienttesting.DeleteActionImpl{}
			for _, action := range fakeDynamicClient.Actions() {
				if action.GetVerb() == "delete" {
					deleteActions = append(deleteActions, action.(clienttesting.DeleteActionImpl))
				}
			}
			if !reflect.DeepEqual(c.expectedDeleteActions, deleteActions) {
				t.Fatal(spew.Sdump(deleteActions))
			}

			queueLen := controllerContext.Queue().Len()
			if queueLen != c.expectedQueueLen {
				t.Errorf("expected %d, but %d", c.expectedQueueLen, queueLen)
			}
		})
	}

}

func TestFindUntrackedResources(t *testing.T) {
	cases := []struct {
		name                       string
		appliedResources           []workapiv1.AppliedManifestResourceMeta
		newAppliedResources        []workapiv1.AppliedManifestResourceMeta
		expectedUntrackedResources []workapiv1.AppliedManifestResourceMeta
	}{
		{
			name:             "no resource untracked",
			appliedResources: nil,
			newAppliedResources: []workapiv1.AppliedManifestResourceMeta{
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "g1", Resource: "r1", Namespace: "ns1", Name: "n1"}},
			},
			expectedUntrackedResources: nil,
		},
		{
			name: "some of original resources untracked",
			appliedResources: []workapiv1.AppliedManifestResourceMeta{
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "g1", Resource: "r1", Namespace: "ns1", Name: "n1"}},
				{Version: "v2", ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "g2", Resource: "r2", Namespace: "ns2", Name: "n2"}},
			},
			newAppliedResources: []workapiv1.AppliedManifestResourceMeta{
				{Version: "v2", ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "g2", Resource: "r2", Namespace: "ns2", Name: "n2"}},
				{Version: "v3", ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "g3", Resource: "r3", Namespace: "ns3", Name: "n3"}},
			},
			expectedUntrackedResources: []workapiv1.AppliedManifestResourceMeta{
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "g1", Resource: "r1", Namespace: "ns1", Name: "n1"}},
			},
		},
		{
			name: "all original resources untracked",
			appliedResources: []workapiv1.AppliedManifestResourceMeta{
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "g1", Resource: "r1", Namespace: "ns1", Name: "n1"}},
				{Version: "v2", ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "g2", Resource: "r2", Namespace: "ns2", Name: "n2"}},
			},
			newAppliedResources: []workapiv1.AppliedManifestResourceMeta{
				{Version: "v3", ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "g3", Resource: "r3", Namespace: "ns3", Name: "n3"}},
				{Version: "v4", ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "g4", Resource: "r4", Namespace: "ns4", Name: "n4"}},
			},
			expectedUntrackedResources: []workapiv1.AppliedManifestResourceMeta{
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "g1", Resource: "r1", Namespace: "ns1", Name: "n1"}},
				{Version: "v2", ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "g2", Resource: "r2", Namespace: "ns2", Name: "n2"}},
			},
		},
		{
			name: "changing version of original resources does not make it untracked",
			appliedResources: []workapiv1.AppliedManifestResourceMeta{
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "g1", Resource: "r1", Namespace: "ns1", Name: "n1"}},
				{Version: "v2", ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "g2", Resource: "r2", Namespace: "ns2", Name: "n2"}},
			},
			newAppliedResources: []workapiv1.AppliedManifestResourceMeta{
				{Version: "v2", ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "g1", Resource: "r1", Namespace: "ns1", Name: "n1"}},
				{Version: "v4", ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "g4", Resource: "r4", Namespace: "ns4", Name: "n4"}},
			},
			expectedUntrackedResources: []workapiv1.AppliedManifestResourceMeta{
				{Version: "v2", ResourceIdentifier: workapiv1.ResourceIdentifier{Group: "g2", Resource: "r2", Namespace: "ns2", Name: "n2"}},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			actual := findUntrackedResources(c.appliedResources, c.newAppliedResources)
			if !reflect.DeepEqual(actual, c.expectedUntrackedResources) {
				t.Errorf(diff.ObjectDiff(actual, c.expectedUntrackedResources))
			}
		})
	}
}
