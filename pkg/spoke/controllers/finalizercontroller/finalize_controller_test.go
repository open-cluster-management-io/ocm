package finalizercontroller

import (
	"context"
	"reflect"
	"testing"

	"github.com/davecgh/go-spew/spew"
	fakeworkclient "github.com/open-cluster-management/api/client/work/clientset/versioned/fake"
	workapiv1 "github.com/open-cluster-management/api/work/v1"
	"github.com/open-cluster-management/work/pkg/spoke/spoketesting"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
)

func TestFinalize(t *testing.T) {
	cases := []struct {
		name               string
		existingFinalizers []string
		resourcesToRemove  []workapiv1.ManifestResourceMeta
		terminated         bool

		validateManifestWorkActions func(t *testing.T, actions []clienttesting.Action)
		validateDynamicActions      func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:               "skip when not delete",
			existingFinalizers: []string{manifestWorkFinalizer},
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
			name:               "skip when finalizer gone",
			terminated:         true,
			existingFinalizers: []string{"other-finalizer"},
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
			name:               "delete resources",
			terminated:         true,
			existingFinalizers: []string{"a", manifestWorkFinalizer, "b"},
			resourcesToRemove: []workapiv1.ManifestResourceMeta{
				{Group: "g1", Version: "v1", Resource: "r1", Namespace: "", Name: "n1"},
				{Group: "g2", Version: "v2", Resource: "r2", Namespace: "ns2", Name: "n2"},
				{Group: "g3", Version: "v3", Resource: "r3", Namespace: "ns3", Name: "n3"},
				{Group: "g4", Version: "v4", Resource: "r4", Namespace: "", Name: "n4"},
			},
			validateManifestWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 1 {
					t.Fatal(spew.Sdump(actions))
				}
				work := actions[0].(clienttesting.UpdateAction).GetObject().(*workapiv1.ManifestWork)
				if !reflect.DeepEqual(work.Finalizers, []string{"a", "b"}) {
					t.Fatal(spew.Sdump(actions))
				}
			},
			validateDynamicActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 4 {
					t.Fatal(spew.Sdump(actions))
				}

				action := actions[0].(clienttesting.DeleteAction)
				resource, namespace, name := action.GetResource(), action.GetNamespace(), action.GetName()
				if !reflect.DeepEqual(resource, schema.GroupVersionResource{Group: "g1", Version: "v1", Resource: "r1"}) || namespace != "" || name != "n1" {
					t.Fatal(spew.Sdump(actions))
				}
				action = actions[1].(clienttesting.DeleteAction)
				resource, namespace, name = action.GetResource(), action.GetNamespace(), action.GetName()
				if !reflect.DeepEqual(resource, schema.GroupVersionResource{Group: "g2", Version: "v2", Resource: "r2"}) || namespace != "ns2" || name != "n2" {
					t.Fatal(spew.Sdump(actions))
				}
				action = actions[2].(clienttesting.DeleteAction)
				resource, namespace, name = action.GetResource(), action.GetNamespace(), action.GetName()
				if !reflect.DeepEqual(resource, schema.GroupVersionResource{Group: "g3", Version: "v3", Resource: "r3"}) || namespace != "ns3" || name != "n3" {
					t.Fatal(spew.Sdump(actions))
				}
				action = actions[3].(clienttesting.DeleteAction)
				resource, namespace, name = action.GetResource(), action.GetNamespace(), action.GetName()
				if !reflect.DeepEqual(resource, schema.GroupVersionResource{Group: "g4", Version: "v4", Resource: "r4"}) || namespace != "" || name != "n4" {
					t.Fatal(spew.Sdump(actions))
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			testingWork, _ := spoketesting.NewManifestWork(0)
			testingWork.Finalizers = c.existingFinalizers
			if c.terminated {
				now := metav1.Now()
				testingWork.DeletionTimestamp = &now
			}
			for _, curr := range c.resourcesToRemove {
				testingWork.Status.ResourceStatus.Manifests = append(testingWork.Status.ResourceStatus.Manifests, workapiv1.ManifestCondition{ResourceMeta: curr})
			}

			fakeDynamicClient := fakedynamic.NewSimpleDynamicClient(runtime.NewScheme())
			fakeClient := fakeworkclient.NewSimpleClientset(testingWork)
			controller := FinalizeController{
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
