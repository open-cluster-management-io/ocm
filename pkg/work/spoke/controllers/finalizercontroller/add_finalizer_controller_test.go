package finalizercontroller

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/davecgh/go-spew/spew"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clienttesting "k8s.io/client-go/testing"

	fakeworkclient "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	"open-cluster-management.io/ocm/pkg/work/spoke/spoketesting"
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
				testingcommon.AssertActions(t, actions, "patch")
				p := actions[0].(clienttesting.PatchActionImpl).Patch
				work := &workapiv1.ManifestWork{}
				if err := json.Unmarshal(p, work); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(work.Finalizers, []string{workapiv1.ManifestWorkFinalizer}) {
					t.Fatal(spew.Sdump(actions))
				}
			},
		},
		{
			name:               "add when missing",
			existingFinalizers: []string{"other"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				p := actions[0].(clienttesting.PatchActionImpl).Patch
				work := &workapiv1.ManifestWork{}
				if err := json.Unmarshal(p, work); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(work.Finalizers, []string{"other", workapiv1.ManifestWorkFinalizer}) {
					t.Fatal(spew.Sdump(actions))
				}
			},
		},
		{
			name:            "skip when deleted",
			terminated:      true,
			validateActions: testingcommon.AssertNoActions,
		},
		{
			name:               "skip when present",
			existingFinalizers: []string{workapiv1.ManifestWorkFinalizer},
			validateActions:    testingcommon.AssertNoActions,
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
				patcher: patcher.NewPatcher[
					*workapiv1.ManifestWork, workapiv1.ManifestWorkSpec, workapiv1.ManifestWorkStatus](
					fakeClient.WorkV1().ManifestWorks(testingWork.Namespace)),
			}

			err := controller.syncManifestWork(context.TODO(), testingWork)
			if err != nil {
				t.Fatal(err)
			}
			c.validateActions(t, fakeClient.Actions())
		})
	}
}
