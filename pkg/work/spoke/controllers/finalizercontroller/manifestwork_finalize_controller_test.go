package finalizercontroller

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/util/workqueue"

	fakeworkclient "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
)

func TestSyncManifestWorkController(t *testing.T) {
	hubHash := "test"
	now := metav1.Now()
	cases := []struct {
		name                               string
		workName                           string
		work                               *workapiv1.ManifestWork
		appliedWork                        *workapiv1.AppliedManifestWork
		validateAppliedManifestWorkActions func(t *testing.T, actions []clienttesting.Action)
		validateManifestWorkActions        func(t *testing.T, actions []clienttesting.Action)
		expectedQueueLen                   int
	}{
		{
			name:     "do nothing when work is not deleting",
			workName: "work",
			work:     &workapiv1.ManifestWork{ObjectMeta: metav1.ObjectMeta{Name: "work", Namespace: "cluster1"}},
			appliedWork: &workapiv1.AppliedManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("%s-work", hubHash),
				},
			},
			validateAppliedManifestWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 0 {
					t.Errorf("Suppose nothing done for appliedmanifestwork")
				}
			},
			validateManifestWorkActions: testingcommon.AssertNoActions,
			expectedQueueLen:            0,
		},
		{
			name:     "delete appliedmanifestworkwork when work has no finalizer on that",
			workName: "work",
			work: &workapiv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "work",
					Namespace:         "cluster1",
					DeletionTimestamp: &now,
				},
			},
			appliedWork: &workapiv1.AppliedManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("%s-work", hubHash),
				},
			},
			validateAppliedManifestWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "delete")
			},
			validateManifestWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				p := actions[0].(clienttesting.PatchActionImpl).Patch
				work := &workapiv1.ManifestWork{}
				if err := json.Unmarshal(p, work); err != nil {
					t.Fatal(err)
				}
				if !meta.IsStatusConditionTrue(work.Status.Conditions, workapiv1.WorkDeleting) {
					t.Errorf("expected work to have deleting condition")
				}
			},
			expectedQueueLen: 1,
		},
		{
			name:     "delete applied work when work is deleting",
			workName: "work",
			work: &workapiv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "work",
					Namespace:         "cluster1",
					DeletionTimestamp: &now,
					Finalizers:        []string{workapiv1.ManifestWorkFinalizer},
				},
			},
			appliedWork: &workapiv1.AppliedManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("%s-work", hubHash),
				},
			},
			validateAppliedManifestWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "delete")
			},
			validateManifestWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				p := actions[0].(clienttesting.PatchActionImpl).Patch
				work := &workapiv1.ManifestWork{}
				if err := json.Unmarshal(p, work); err != nil {
					t.Fatal(err)
				}
				if !meta.IsStatusConditionTrue(work.Status.Conditions, workapiv1.WorkDeleting) {
					t.Errorf("expected work to have deleting condition")
				}
			},
			expectedQueueLen: 1,
		},
		{
			name:     "requeue work when applied work is deleting",
			workName: "work",
			work: &workapiv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "work",
					Namespace:         "cluster1",
					DeletionTimestamp: &now,
					Finalizers:        []string{workapiv1.ManifestWorkFinalizer},
				},
			},
			appliedWork: &workapiv1.AppliedManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:              fmt.Sprintf("%s-work", hubHash),
					DeletionTimestamp: &now,
				},
			},
			validateAppliedManifestWorkActions: testingcommon.AssertNoActions,
			validateManifestWorkActions:        testingcommon.AssertNoActions,
			expectedQueueLen:                   1,
		},
		{
			name:     "remove finalizer when applied work is cleaned",
			workName: "work",
			work: &workapiv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "work",
					Namespace:         "cluster1",
					DeletionTimestamp: &now,
					Finalizers:        []string{workapiv1.ManifestWorkFinalizer},
				},
			},
			appliedWork: &workapiv1.AppliedManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name: "fake",
				},
			},
			validateAppliedManifestWorkActions: testingcommon.AssertNoActions,
			validateManifestWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				p := actions[0].(clienttesting.PatchActionImpl).Patch
				work := &workapiv1.ManifestWork{}
				if err := json.Unmarshal(p, work); err != nil {
					t.Fatal(err)
				}
				if len(work.Finalizers) != 0 {
					t.Errorf("Expect finalizer is cleaned")
				}
			},
			expectedQueueLen: 0,
		},
		{
			name:     "delete appliedmanifestwork when no matched manifestwoork",
			workName: "work",
			work: &workapiv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "fakework",
					Namespace: "cluster1",
				},
			},
			appliedWork: &workapiv1.AppliedManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("%s-work", hubHash),
				},
			},
			validateAppliedManifestWorkActions: testingcommon.AssertNoActions,
			validateManifestWorkActions:        testingcommon.AssertNoActions,
			expectedQueueLen:                   0,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeClient := fakeworkclient.NewSimpleClientset(c.work, c.appliedWork)
			informerFactory := workinformers.NewSharedInformerFactory(fakeClient, 5*time.Minute)
			if err := informerFactory.Work().V1().ManifestWorks().Informer().GetStore().Add(c.work); err != nil {
				t.Fatal(err)
			}
			if err := informerFactory.Work().V1().AppliedManifestWorks().Informer().GetStore().Add(c.appliedWork); err != nil {
				t.Fatal(err)
			}
			controller := &ManifestWorkFinalizeController{
				patcher: patcher.NewPatcher[
					*workapiv1.ManifestWork, workapiv1.ManifestWorkSpec, workapiv1.ManifestWorkStatus](
					fakeClient.WorkV1().ManifestWorks("cluster1")),
				manifestWorkLister:        informerFactory.Work().V1().ManifestWorks().Lister().ManifestWorks("cluster1"),
				appliedManifestWorkClient: fakeClient.WorkV1().AppliedManifestWorks(),
				appliedManifestWorkLister: informerFactory.Work().V1().AppliedManifestWorks().Lister(),
				hubHash:                   hubHash,
				rateLimiter:               workqueue.NewItemExponentialFailureRateLimiter(0, 1*time.Second),
			}

			controllerContext := testingcommon.NewFakeSyncContext(t, c.workName)
			err := controller.sync(context.TODO(), controllerContext, c.workName)
			if err != nil {
				t.Errorf("Expect no sync error, but got %v", err)
			}

			var workAction, appliedWorkAction []clienttesting.Action
			for _, action := range fakeClient.Actions() {
				if action.GetResource().Resource == "manifestworks" {
					workAction = append(workAction, action)
				}
				if action.GetResource().Resource == "appliedmanifestworks" {
					appliedWorkAction = append(appliedWorkAction, action)
				}
			}

			c.validateManifestWorkActions(t, workAction)
			c.validateAppliedManifestWorkActions(t, appliedWorkAction)

			queueLen := controllerContext.Queue().Len()
			if queueLen != c.expectedQueueLen {
				t.Errorf("expected %d, but %d", c.expectedQueueLen, queueLen)
			}
		})
	}
}
