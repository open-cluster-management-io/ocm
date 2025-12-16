package manifestworkreplicasetcontroller

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	fakeclient "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	workapiv1 "open-cluster-management.io/api/work/v1"
	workapiv1alpha1 "open-cluster-management.io/api/work/v1alpha1"
	workapplier "open-cluster-management.io/sdk-go/pkg/apis/work/v1/applier"

	commonhelper "open-cluster-management.io/ocm/pkg/common/helpers"
	helpertest "open-cluster-management.io/ocm/pkg/work/hub/test"
)

// Test finalize reconcile
func TestFinalizeReconcile(t *testing.T) {
	cases := []struct {
		name            string
		resources       func() (*workapiv1alpha1.ManifestWorkReplicaSet, []*workapiv1.ManifestWork)
		validateActions func(t *testing.T, manifestWorkReplicaSet *workapiv1alpha1.ManifestWorkReplicaSet,
			state reconcileState, err error)
	}{
		{
			name: "deleting manifestWorkReplicaSet with no pending works",
			resources: func() (*workapiv1alpha1.ManifestWorkReplicaSet, []*workapiv1.ManifestWork) {
				mwrSetTest := helpertest.CreateTestManifestWorkReplicaSet("mwrSet-test", "default", "place-test")
				timeNow := metav1.Now()
				mwrSetTest.DeletionTimestamp = &timeNow
				mwrSetTest.Finalizers = append(mwrSetTest.Finalizers, workapiv1alpha1.ManifestWorkReplicaSetFinalizer)

				mw, _ := CreateManifestWork(mwrSetTest, "cluster1", "place-test")
				return mwrSetTest, []*workapiv1.ManifestWork{mw}
			},
			validateActions: func(t *testing.T, manifestWorkReplicaSet *workapiv1alpha1.ManifestWorkReplicaSet,
				state reconcileState, err error) {
				if err != nil {
					t.Fatalf("Failed to create manifestWorkReplicaSet: %v", err)
				}
				if state != reconcileStop {
					t.Fatalf("manifestWorkReplicaSet should be stopped after finalizeReconcile")
				}
				if commonhelper.HasFinalizer(manifestWorkReplicaSet.Finalizers, workapiv1alpha1.ManifestWorkReplicaSetFinalizer) {
					t.Fatalf("Finalizer %v not deleted", manifestWorkReplicaSet.Finalizers)
				}
			},
		},
		{
			name: "deleting Foreground manifestWorkReplicaSet with pending works",
			resources: func() (*workapiv1alpha1.ManifestWorkReplicaSet, []*workapiv1.ManifestWork) {
				mwrSetTest := helpertest.CreateTestManifestWorkReplicaSet("mwrSet-test", "default", "place-test")
				timeNow := metav1.Now()
				mwrSetTest.DeletionTimestamp = &timeNow
				mwrSetTest.Finalizers = append(mwrSetTest.Finalizers, workapiv1alpha1.ManifestWorkReplicaSetFinalizer)
				mwrSetTest.Spec.CascadeDeletionPolicy = workapiv1alpha1.Foreground

				mw1, _ := CreateManifestWork(mwrSetTest, "cluster1", "place-test")
				mw2, _ := CreateManifestWork(mwrSetTest, "cluster2", "place-test")
				mw2.Finalizers = append(mw2.Finalizers, workapiv1.ManifestWorkFinalizer)
				return mwrSetTest, []*workapiv1.ManifestWork{mw1, mw2}
			},
			validateActions: func(t *testing.T, manifestWorkReplicaSet *workapiv1alpha1.ManifestWorkReplicaSet,
				state reconcileState, err error) {
				if err != nil {
					t.Fatalf("Failed to create manifestWorkReplicaSet: %v", err)
				}
				if state != reconcileContinue {
					t.Fatalf("manifestWorkReplicaSet should be continue after finalizeReconcile")
				}
				if !commonhelper.HasFinalizer(manifestWorkReplicaSet.Finalizers, workapiv1alpha1.ManifestWorkReplicaSetFinalizer) {
					t.Fatalf("Finalizer %v is deleted", manifestWorkReplicaSet.Finalizers)
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			manifestWorkReplicaSet, manifestWorks := c.resources()
			objects := []runtime.Object{manifestWorkReplicaSet}
			for _, mw := range manifestWorks {
				objects = append(objects, mw)
			}
			fakeClient := fakeclient.NewSimpleClientset(objects...)
			manifestWorkInformerFactory := workinformers.NewSharedInformerFactoryWithOptions(fakeClient, 1*time.Second)
			mwLister := manifestWorkInformerFactory.Work().V1().ManifestWorks().Lister()
			workStore := manifestWorkInformerFactory.Work().V1().ManifestWorks().Informer().GetStore()
			for _, work := range manifestWorks {
				if err := workStore.Add(work); err != nil {
					t.Fatal(err)
				}
			}
			finalizerController := finalizeReconciler{
				workClient:         fakeClient,
				manifestWorkLister: mwLister,
				workApplier:        workapplier.NewWorkApplierWithTypedClient(fakeClient, mwLister),
			}

			_, state, rstErr := finalizerController.reconcile(context.TODO(), manifestWorkReplicaSet)

			updatetSet, err := fakeClient.WorkV1alpha1().ManifestWorkReplicaSets(manifestWorkReplicaSet.Namespace).
				Get(context.TODO(), manifestWorkReplicaSet.Name, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Failed to get manifestWorkReplicaSet: %v", err)
			}
			c.validateActions(t, updatetSet, state, rstErr)
		})
	}
}
