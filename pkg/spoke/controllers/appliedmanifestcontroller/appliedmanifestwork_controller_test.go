package appliedmanifestcontroller

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	fakeworkclient "github.com/open-cluster-management/api/client/work/clientset/versioned/fake"
	workinformers "github.com/open-cluster-management/api/client/work/informers/externalversions"
	workapiv1 "github.com/open-cluster-management/api/work/v1"
	"github.com/open-cluster-management/work/pkg/spoke/spoketesting"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/diff"
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
	cases := []struct {
		name                               string
		existingResources                  []runtime.Object
		appliedResources                   []workapiv1.AppliedManifestResourceMeta
		manifests                          []workapiv1.ManifestCondition
		validateAppliedManifestWorkActions func(t *testing.T, actions []clienttesting.Action)
		validateDynamicActions             func(t *testing.T, actions []clienttesting.Action)
		expectedQueueLen                   int
	}{
		{
			name: "skip when no applied resource changed",
			appliedResources: []workapiv1.AppliedManifestResourceMeta{
				{Group: "g1", Version: "v1", Resource: "r1", Namespace: "ns1", Name: "n1"},
			},
			manifests: []workapiv1.ManifestCondition{newManifest("g1", "v1", "r1", "ns1", "n1")},
			validateAppliedManifestWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) > 0 {
					t.Fatal(spew.Sdump(actions))
				}
			},
			validateDynamicActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) > 0 {
					t.Fatal(spew.Sdump(actions))
				}
			},
		},
		{
			name: "delete untracked resources",
			appliedResources: []workapiv1.AppliedManifestResourceMeta{
				{Group: "g1", Version: "v1", Resource: "r1", Namespace: "ns1", Name: "n1"},
				{Group: "g2", Version: "v2", Resource: "r2", Namespace: "ns2", Name: "n2"},
				{Group: "g3", Version: "v3", Resource: "r3", Namespace: "ns3", Name: "n3"},
				{Group: "g4", Version: "v4", Resource: "r4", Namespace: "ns4", Name: "n4"},
			},
			manifests: []workapiv1.ManifestCondition{
				newManifest("g1", "v1", "r1", "ns1", "n1"),
				newManifest("g2", "v2", "r2", "ns2", "n2"),
				newManifest("g5", "v5", "r5", "ns5", "n5"),
				newManifest("g6", "v6", "r6", "ns6", "n6"),
			},
			validateAppliedManifestWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 1 {
					t.Fatal(spew.Sdump(actions))
				}
				work := actions[0].(clienttesting.UpdateAction).GetObject().(*workapiv1.AppliedManifestWork)
				if !reflect.DeepEqual(work.Status.AppliedResources, []workapiv1.AppliedManifestResourceMeta{
					{Group: "g1", Version: "v1", Resource: "r1", Namespace: "ns1", Name: "n1"},
					{Group: "g2", Version: "v2", Resource: "r2", Namespace: "ns2", Name: "n2"},
					{Group: "g5", Version: "v5", Resource: "r5", Namespace: "ns5", Name: "n5"},
					{Group: "g6", Version: "v6", Resource: "r6", Namespace: "ns6", Name: "n6"},
				}) {
					t.Fatal(spew.Sdump(actions))
				}
			},
			validateDynamicActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 2 {
					t.Fatal(spew.Sdump(actions))
				}

				action := actions[0].(clienttesting.GetAction)
				resource, namespace, name := action.GetResource(), action.GetNamespace(), action.GetName()
				if !reflect.DeepEqual(resource, schema.GroupVersionResource{Group: "g3", Version: "v3", Resource: "r3"}) || namespace != "ns3" || name != "n3" {
					t.Fatal(spew.Sdump(actions))
				}
				action = actions[1].(clienttesting.GetAction)
				resource, namespace, name = action.GetResource(), action.GetNamespace(), action.GetName()
				if !reflect.DeepEqual(resource, schema.GroupVersionResource{Group: "g4", Version: "v4", Resource: "r4"}) || namespace != "ns4" || name != "n4" {
					t.Fatal(spew.Sdump(actions))
				}
			},
		},
		{
			name: "requeue work when applied resource for stale manifest is deleting",
			existingResources: []runtime.Object{
				spoketesting.NewUnstructuredSecret("ns3", "n3", true, "ns3-n3"),
			},
			appliedResources: []workapiv1.AppliedManifestResourceMeta{
				{Version: "v1", Resource: "secrets", Namespace: "ns1", Name: "n1"},
				{Version: "v1", Resource: "secrets", Namespace: "ns2", Name: "n2"},
				{Version: "v1", Resource: "secrets", Namespace: "ns3", Name: "n3", UID: "ns3-n3"},
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
			validateDynamicActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 1 {
					t.Fatal(spew.Sdump(actions))
				}

				action := actions[0].(clienttesting.GetAction)
				resource, namespace, name := action.GetResource(), action.GetNamespace(), action.GetName()
				if !reflect.DeepEqual(resource, schema.GroupVersionResource{Group: "", Version: "v1", Resource: "secrets"}) || namespace != "ns3" || name != "n3" {
					t.Fatal(spew.Sdump(actions))
				}
			},
			expectedQueueLen: 1,
		},
		{
			name: "ignore re-created resource",
			existingResources: []runtime.Object{
				spoketesting.NewUnstructuredSecret("ns3", "n3", false, "ns3-n3-recreated"),
			},
			appliedResources: []workapiv1.AppliedManifestResourceMeta{
				{Version: "v1", Resource: "secrets", Namespace: "ns3", Name: "n3", UID: "ns3-n3"},
				{Version: "v1", Resource: "secrets", Namespace: "ns4", Name: "n4", UID: "ns4-n4"},
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
					{Version: "v1", Resource: "secrets", Namespace: "ns1", Name: "n1"},
					{Version: "v1", Resource: "secrets", Namespace: "ns5", Name: "n5"},
				}) {
					t.Fatal(spew.Sdump(actions))
				}
			},
			validateDynamicActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 2 {
					t.Fatal(spew.Sdump(actions))
				}

				action := actions[0].(clienttesting.GetAction)
				resource, namespace, name := action.GetResource(), action.GetNamespace(), action.GetName()
				if !reflect.DeepEqual(resource, schema.GroupVersionResource{Group: "", Version: "v1", Resource: "secrets"}) || namespace != "ns3" || name != "n3" {
					t.Fatal(spew.Sdump(actions))
				}
				action = actions[1].(clienttesting.GetAction)
				resource, namespace, name = action.GetResource(), action.GetNamespace(), action.GetName()
				if !reflect.DeepEqual(resource, schema.GroupVersionResource{Group: "", Version: "v1", Resource: "secrets"}) || namespace != "ns4" || name != "n4" {
					t.Fatal(spew.Sdump(actions))
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			testingWork, _ := spoketesting.NewManifestWork(0)
			testingAppliedWork := spoketesting.NewAppliedManifestWork("test", 0)
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
			c.validateDynamicActions(t, fakeDynamicClient.Actions())

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
				{Group: "g1", Version: "v1", Resource: "r1", Namespace: "ns1", Name: "n1"},
			},
			expectedUntrackedResources: nil,
		},
		{
			name: "some of original resources untracked",
			appliedResources: []workapiv1.AppliedManifestResourceMeta{
				{Group: "g1", Version: "v1", Resource: "r1", Namespace: "ns1", Name: "n1"},
				{Group: "g2", Version: "v2", Resource: "r2", Namespace: "ns2", Name: "n2"},
			},
			newAppliedResources: []workapiv1.AppliedManifestResourceMeta{
				{Group: "g2", Version: "v2", Resource: "r2", Namespace: "ns2", Name: "n2"},
				{Group: "g3", Version: "v3", Resource: "r3", Namespace: "ns3", Name: "n3"},
			},
			expectedUntrackedResources: []workapiv1.AppliedManifestResourceMeta{
				{Group: "g1", Version: "v1", Resource: "r1", Namespace: "ns1", Name: "n1"},
			},
		},
		{
			name: "all original resources untracked",
			appliedResources: []workapiv1.AppliedManifestResourceMeta{
				{Group: "g1", Version: "v1", Resource: "r1", Namespace: "ns1", Name: "n1"},
				{Group: "g2", Version: "v2", Resource: "r2", Namespace: "ns2", Name: "n2"},
			},
			newAppliedResources: []workapiv1.AppliedManifestResourceMeta{
				{Group: "g3", Version: "v3", Resource: "r3", Namespace: "ns3", Name: "n3"},
				{Group: "g4", Version: "v4", Resource: "r4", Namespace: "ns4", Name: "n4"},
			},
			expectedUntrackedResources: []workapiv1.AppliedManifestResourceMeta{
				{Group: "g1", Version: "v1", Resource: "r1", Namespace: "ns1", Name: "n1"},
				{Group: "g2", Version: "v2", Resource: "r2", Namespace: "ns2", Name: "n2"},
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
