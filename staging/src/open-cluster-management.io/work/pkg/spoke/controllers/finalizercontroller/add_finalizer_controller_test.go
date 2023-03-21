package finalizercontroller

import (
	"context"
	"reflect"
	"testing"

	"github.com/davecgh/go-spew/spew"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clienttesting "k8s.io/client-go/testing"
	fakeworkclient "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/work/pkg/spoke/controllers"
	"open-cluster-management.io/work/pkg/spoke/spoketesting"
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
				if !reflect.DeepEqual(work.Finalizers, []string{controllers.ManifestWorkFinalizer}) {
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
				if !reflect.DeepEqual(work.Finalizers, []string{"other", controllers.ManifestWorkFinalizer}) {
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
			existingFinalizers: []string{controllers.ManifestWorkFinalizer},
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
