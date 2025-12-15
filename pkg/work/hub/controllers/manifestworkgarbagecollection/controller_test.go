package manifestworkgarbagecollection

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"

	fakeworkclient "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	workapiv1 "open-cluster-management.io/api/work/v1"
	workapiv1alpha1 "open-cluster-management.io/api/work/v1alpha1"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
)

func TestManifestWorkGarbageCollectionController(t *testing.T) {
	cases := []struct {
		name                   string
		works                  []runtime.Object
		expectedDeleteActions  int
		expectedRequeueActions int
		validateActions        func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name: "no TTL configured",
			works: []runtime.Object{
				createManifestWorkWithoutTTL("test", "default"),
			},
			expectedDeleteActions:  0,
			expectedRequeueActions: 0,
		},
		{
			name: "not completed",
			works: []runtime.Object{
				createManifestWorkWithTTL("test", "default", 300),
			},
			expectedDeleteActions:  0,
			expectedRequeueActions: 0,
		},
		{
			name: "completed but TTL not expired",
			works: []runtime.Object{
				createCompletedManifestWorkWithTTL("test", "default", 300, time.Now().Add(-60*time.Second)),
			},
			expectedDeleteActions:  0,
			expectedRequeueActions: 0, // AddAfter doesn't increment queue length immediately
		},
		{
			name: "completed and TTL expired",
			works: []runtime.Object{
				createCompletedManifestWorkWithTTL("test", "default", 300, time.Now().Add(-400*time.Second)),
			},
			expectedDeleteActions:  1,
			expectedRequeueActions: 0,
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "delete")
				deleteAction := actions[0].(clienttesting.DeleteActionImpl)
				if deleteAction.Namespace != "default" || deleteAction.Name != "test" {
					t.Errorf("Expected delete action for default/test, got %s/%s", deleteAction.Namespace, deleteAction.Name)
				}
			},
		},
		{
			name: "TTL is zero - delete immediately",
			works: []runtime.Object{
				createCompletedManifestWorkWithTTL("test", "default", 0, time.Now().Add(-1*time.Second)),
			},
			expectedDeleteActions:  1,
			expectedRequeueActions: 0,
		},
		{
			name: "ManifestWork with ManifestWorkReplicaSet label - should skip GC",
			works: []runtime.Object{
				createCompletedManifestWorkWithLabel("test", "default", 300, time.Now().Add(-400*time.Second), map[string]string{
					workapiv1alpha1.ManifestWorkReplicaSetControllerNameLabelKey: "test-replicaset",
				}),
			},
			expectedDeleteActions:  0,
			expectedRequeueActions: 0,
		},
		{
			name: "ManifestWork with other labels but no ReplicaSet label - should continue with GC",
			works: []runtime.Object{
				createCompletedManifestWorkWithLabel("test", "default", 300, time.Now().Add(-400*time.Second), map[string]string{
					"app": "test-app",
					"env": "production",
				}),
			},
			expectedDeleteActions:  1,
			expectedRequeueActions: 0,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeWorkClient := fakeworkclient.NewSimpleClientset(c.works...)
			workInformerFactory := workinformers.NewSharedInformerFactory(fakeWorkClient, time.Minute*10)

			controller := &ManifestWorkGarbageCollectionController{
				workClient: fakeWorkClient,
				workLister: workInformerFactory.Work().V1().ManifestWorks().Lister(),
			}

			ctx := context.TODO()
			workInformerFactory.Start(ctx.Done())
			workInformerFactory.WaitForCacheSync(ctx.Done())

			syncContext := testingcommon.NewFakeSyncContext(t, "default/test")
			err := controller.sync(ctx, syncContext, "default/test")
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			// Filter client actions to only include relevant ones (skip list/watch)
			actions := fakeWorkClient.Actions()
			deleteActions := 0
			requeueActions := syncContext.Queue().Len()

			for _, action := range actions {
				if action.GetVerb() == "delete" {
					deleteActions++
				}
			}

			if deleteActions != c.expectedDeleteActions {
				t.Errorf("Expected %d delete actions, got %d", c.expectedDeleteActions, deleteActions)
			}

			if requeueActions != c.expectedRequeueActions {
				t.Errorf("Expected %d requeue actions, got %d", c.expectedRequeueActions, requeueActions)
			}

			if c.validateActions != nil {
				var deleteActionsOnly []clienttesting.Action
				for _, action := range actions {
					if action.GetVerb() == "delete" {
						deleteActionsOnly = append(deleteActionsOnly, action)
					}
				}
				c.validateActions(t, deleteActionsOnly)
			}
		})
	}
}

func TestNewManifestWorkGarbageCollectionController(t *testing.T) {
	fakeWorkClient := fakeworkclient.NewSimpleClientset()
	workInformerFactory := workinformers.NewSharedInformerFactory(fakeWorkClient, time.Minute*10)

	ctrl := NewManifestWorkGarbageCollectionController(
		fakeWorkClient,
		workInformerFactory.Work().V1().ManifestWorks(),
	)

	if ctrl == nil {
		t.Errorf("Expected controller to be created")
	}
}

func createManifestWorkWithoutTTL(name, namespace string) *workapiv1.ManifestWork {
	obj := testingcommon.NewUnstructured("v1", "ConfigMap", "test-ns", "test-configmap")
	return &workapiv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: workapiv1.ManifestWorkSpec{
			Workload: workapiv1.ManifestsTemplate{
				Manifests: []workapiv1.Manifest{
					{RawExtension: runtime.RawExtension{Object: obj}},
				},
			},
		},
	}
}

func createManifestWorkWithTTL(name, namespace string, ttlSeconds int64) *workapiv1.ManifestWork {
	mw := createManifestWorkWithoutTTL(name, namespace)
	mw.Spec.DeleteOption = &workapiv1.DeleteOption{
		TTLSecondsAfterFinished: &ttlSeconds,
	}
	return mw
}

func createCompletedManifestWorkWithTTL(name, namespace string, ttlSeconds int64, completedTime time.Time) *workapiv1.ManifestWork {
	mw := createManifestWorkWithTTL(name, namespace, ttlSeconds)

	// Add Complete condition
	completedCondition := metav1.Condition{
		Type:               workapiv1.WorkComplete,
		Status:             metav1.ConditionTrue,
		Reason:             workapiv1.WorkManifestsComplete,
		Message:            "All manifests have completed",
		LastTransitionTime: metav1.NewTime(completedTime),
	}

	mw.Status.Conditions = []metav1.Condition{completedCondition}
	return mw
}

func createCompletedManifestWorkWithLabel(name, namespace string, ttlSeconds int64, completedTime time.Time, labels map[string]string) *workapiv1.ManifestWork {
	mw := createCompletedManifestWorkWithTTL(name, namespace, ttlSeconds, completedTime)
	mw.Labels = labels
	return mw
}
