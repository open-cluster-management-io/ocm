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
	clienttesting "k8s.io/client-go/testing"
)

func TestAddFinalizer(t *testing.T) {
	cases := []struct {
		name               string
		existingFinalizers []string
		terminated         bool

		validateActions func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name: "add when empty",
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 1 {
					t.Fatal(spew.Sdump(actions))
				}
				work := actions[0].(clienttesting.UpdateAction).GetObject().(*workapiv1.ManifestWork)
				if !reflect.DeepEqual(work.Finalizers, []string{manifestWorkFinalizer}) {
					t.Fatal(spew.Sdump(actions))
				}
			},
		},
		{
			name:               "add when missing",
			existingFinalizers: []string{"other"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 1 {
					t.Fatal(spew.Sdump(actions))
				}
				work := actions[0].(clienttesting.UpdateAction).GetObject().(*workapiv1.ManifestWork)
				if !reflect.DeepEqual(work.Finalizers, []string{"other", manifestWorkFinalizer}) {
					t.Fatal(spew.Sdump(actions))
				}
			},
		},
		{
			name:       "skip when deleted",
			terminated: true,
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) > 0 {
					t.Fatal(spew.Sdump(actions))
				}
			},
		},
		{
			name:               "skip when present",
			existingFinalizers: []string{manifestWorkFinalizer},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) > 0 {
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

			fakeClient := fakeworkclient.NewSimpleClientset(testingWork)
			controller := AddFinalizerController{
				manifestWorkClient: fakeClient.WorkV1().ManifestWorks(testingWork.Namespace),
			}

			err := controller.syncManifestWork(context.TODO(), testingWork)
			if err != nil {
				t.Fatal(err)
			}
			c.validateActions(t, fakeClient.Actions())
		})
	}
}
