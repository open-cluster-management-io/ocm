package finalizercontroller

import (
	"context"
	"fmt"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/util/workqueue"
	fakeworkclient "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/work/pkg/spoke/controllers"
	"open-cluster-management.io/work/pkg/spoke/spoketesting"
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
			validateManifestWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 0 {
					t.Errorf("Suppose nothing done for manifestwork")
				}
			},
			expectedQueueLen: 0,
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
				if len(actions) != 1 {
					t.Errorf("Expect 1 actions on appliedmanifestwork, but have %d", len(actions))
				}

				spoketesting.AssertAction(t, actions[0], "delete")
			},
			validateManifestWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 0 {
					t.Errorf("Suppose nothing done for manifestwork")
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
					Finalizers:        []string{controllers.ManifestWorkFinalizer},
				},
			},
			appliedWork: &workapiv1.AppliedManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("%s-work", hubHash),
				},
			},
			validateAppliedManifestWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 1 {
					t.Errorf("Expect 1 actions on appliedmanifestwork, but have %d", len(actions))
				}

				spoketesting.AssertAction(t, actions[0], "delete")
			},
			validateManifestWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 0 {
					t.Errorf("Suppose nothing done for manifestwork")
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
					Finalizers:        []string{controllers.ManifestWorkFinalizer},
				},
			},
			appliedWork: &workapiv1.AppliedManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:              fmt.Sprintf("%s-work", hubHash),
					DeletionTimestamp: &now,
				},
			},
			validateAppliedManifestWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 0 {
					t.Errorf("Expect 0 actions on appliedmanifestwork, but have %d", len(actions))
				}
			},
			validateManifestWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 0 {
					t.Errorf("Suppose nothing done for manifestwork")
				}
			},
			expectedQueueLen: 1,
		},
		{
			name:     "remove finalizer when applied work is cleaned",
			workName: "work",
			work: &workapiv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "work",
					Namespace:         "cluster1",
					DeletionTimestamp: &now,
					Finalizers:        []string{controllers.ManifestWorkFinalizer},
				},
			},
			appliedWork: &workapiv1.AppliedManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name: "fake",
				},
			},
			validateAppliedManifestWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 0 {
					t.Errorf("Expect 0 actions on appliedmanifestwork, but have %d", len(actions))
				}
			},
			validateManifestWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 1 {
					t.Errorf("Suppose 1 action for manifestwork, but got %d", len(actions))
				}
				spoketesting.AssertAction(t, actions[0], "update")
				updateAction := actions[0].(clienttesting.UpdateActionImpl)
				obj := updateAction.Object.(*workapiv1.ManifestWork)
				if len(obj.Finalizers) != 0 {
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
			validateAppliedManifestWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 1 {
					t.Errorf("Expect 2 actions on appliedmanifestwork, but have %d", len(actions))
				}

				spoketesting.AssertAction(t, actions[0], "delete")
			},
			validateManifestWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 0 {
					t.Errorf("Suppose nothing done for manifestwork")
				}
			},
			expectedQueueLen: 1,
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
				manifestWorkClient:        fakeClient.WorkV1().ManifestWorks("cluster1"),
				manifestWorkLister:        informerFactory.Work().V1().ManifestWorks().Lister().ManifestWorks("cluster1"),
				appliedManifestWorkClient: fakeClient.WorkV1().AppliedManifestWorks(),
				appliedManifestWorkLister: informerFactory.Work().V1().AppliedManifestWorks().Lister(),
				hubHash:                   hubHash,
				rateLimiter:               workqueue.NewItemExponentialFailureRateLimiter(0, 1*time.Second),
			}

			controllerContext := spoketesting.NewFakeSyncContext(t, c.workName)
			err := controller.sync(context.TODO(), controllerContext)
			if err != nil {
				t.Errorf("Expect no sync error, but got %v", err)
			}

			workAction := []clienttesting.Action{}
			appliedWorkAction := []clienttesting.Action{}
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
