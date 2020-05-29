package deletioncontroller

import (
	"context"
	"reflect"
	"testing"

	"github.com/davecgh/go-spew/spew"
	fakeworkclient "github.com/open-cluster-management/api/client/work/clientset/versioned/fake"
	workapiv1 "github.com/open-cluster-management/api/work/v1"
	"github.com/open-cluster-management/work/pkg/spoke/spoketesting"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/utils/diff"
)

func newAppliedResource(group, version, resource, namespace, name string) workapiv1.AppliedManifestResourceMeta {
	return workapiv1.AppliedManifestResourceMeta{
		Group:     group,
		Version:   version,
		Resource:  resource,
		Namespace: namespace,
		Name:      name,
	}
}

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

func TestGarbageCollection(t *testing.T) {
	cases := []struct {
		name                        string
		appliedResources            []workapiv1.AppliedManifestResourceMeta
		manifests                   []workapiv1.ManifestCondition
		validateManifestWorkActions func(t *testing.T, actions []clienttesting.Action)
		validateDynamicActions      func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:             "skip when no applied resource changed",
			appliedResources: []workapiv1.AppliedManifestResourceMeta{newAppliedResource("g1", "v1", "r1", "ns1", "n1")},
			manifests:        []workapiv1.ManifestCondition{newManifest("g1", "v1", "r1", "ns1", "n1")},
			validateManifestWorkActions: func(t *testing.T, actions []clienttesting.Action) {
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
				newAppliedResource("g1", "v1", "r1", "ns1", "n1"),
				newAppliedResource("g2", "v2", "r2", "ns2", "n2"),
				newAppliedResource("g3", "v3", "r3", "ns3", "n3"),
				newAppliedResource("g4", "v4", "r4", "ns4", "n4"),
			},
			manifests: []workapiv1.ManifestCondition{
				newManifest("g1", "v1", "r1", "ns1", "n1"),
				newManifest("g2", "v2", "r2", "ns2", "n2"),
				newManifest("g5", "v5", "r5", "ns5", "n5"),
				newManifest("g6", "v6", "r6", "ns6", "n6"),
			},
			validateManifestWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 1 {
					t.Fatal(spew.Sdump(actions))
				}
				work := actions[0].(clienttesting.UpdateAction).GetObject().(*workapiv1.ManifestWork)
				if !reflect.DeepEqual(work.Status.AppliedResources, []workapiv1.AppliedManifestResourceMeta{
					newAppliedResource("g1", "v1", "r1", "ns1", "n1"),
					newAppliedResource("g2", "v2", "r2", "ns2", "n2"),
					newAppliedResource("g5", "v5", "r5", "ns5", "n5"),
					newAppliedResource("g6", "v6", "r6", "ns6", "n6"),
				}) {
					t.Fatal(spew.Sdump(actions))
				}
			},
			validateDynamicActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 2 {
					t.Fatal(spew.Sdump(actions))
				}

				action := actions[0].(clienttesting.DeleteAction)
				resource, namespace, name := action.GetResource(), action.GetNamespace(), action.GetName()
				if !reflect.DeepEqual(resource, schema.GroupVersionResource{Group: "g3", Version: "v3", Resource: "r3"}) || namespace != "ns3" || name != "n3" {
					t.Fatal(spew.Sdump(actions))
				}
				action = actions[1].(clienttesting.DeleteAction)
				resource, namespace, name = action.GetResource(), action.GetNamespace(), action.GetName()
				if !reflect.DeepEqual(resource, schema.GroupVersionResource{Group: "g4", Version: "v4", Resource: "r4"}) || namespace != "ns4" || name != "n4" {
					t.Fatal(spew.Sdump(actions))
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			testingWork, _ := spoketesting.NewManifestWork(0)
			testingWork.Status.AppliedResources = c.appliedResources
			testingWork.Status.ResourceStatus.Manifests = c.manifests

			fakeDynamicClient := fakedynamic.NewSimpleDynamicClient(runtime.NewScheme())
			fakeClient := fakeworkclient.NewSimpleClientset(testingWork)
			controller := StaleManifestDeletionController{
				manifestWorkClient: fakeClient.WorkV1().ManifestWorks(testingWork.Namespace),
				spokeDynamicClient: fakeDynamicClient,
			}

			err := controller.syncManifestWork(context.TODO(), testingWork)
			if err != nil {
				t.Fatal(err)
			}
			c.validateManifestWorkActions(t, fakeClient.Actions())
			c.validateDynamicActions(t, fakeDynamicClient.Actions())
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
				newAppliedResource("g1", "v1", "r1", "ns1", "n1"),
			},
			expectedUntrackedResources: nil,
		},
		{
			name: "some of original resources untracked",
			appliedResources: []workapiv1.AppliedManifestResourceMeta{
				newAppliedResource("g1", "v1", "r1", "ns1", "n1"),
				newAppliedResource("g2", "v2", "r2", "ns2", "n2"),
			},
			newAppliedResources: []workapiv1.AppliedManifestResourceMeta{
				newAppliedResource("g2", "v2", "r2", "ns2", "n2"),
				newAppliedResource("g3", "v3", "r3", "ns3", "n3"),
			},
			expectedUntrackedResources: []workapiv1.AppliedManifestResourceMeta{
				newAppliedResource("g1", "v1", "r1", "ns1", "n1"),
			},
		},
		{
			name: "all original resources untracked",
			appliedResources: []workapiv1.AppliedManifestResourceMeta{
				newAppliedResource("g1", "v1", "r1", "ns1", "n1"),
				newAppliedResource("g2", "v2", "r2", "ns2", "n2"),
			},
			newAppliedResources: []workapiv1.AppliedManifestResourceMeta{
				newAppliedResource("g3", "v3", "r3", "ns3", "n3"),
				newAppliedResource("g4", "v4", "r4", "ns4", "n4"),
			},
			expectedUntrackedResources: []workapiv1.AppliedManifestResourceMeta{
				newAppliedResource("g1", "v1", "r1", "ns1", "n1"),
				newAppliedResource("g2", "v2", "r2", "ns2", "n2"),
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
