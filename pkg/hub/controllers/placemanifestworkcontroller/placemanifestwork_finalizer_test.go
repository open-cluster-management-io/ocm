package placemanifestworkcontroller

import (
	"context"
	"golang.org/x/exp/slices"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakeclient "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	"open-cluster-management.io/api/utils/work/v1/workapplier"
	helpertest "open-cluster-management.io/work/pkg/hub/test"
	"testing"
	"time"
)

// Test finalize reconcile
func TestFinalizeReconcile(t *testing.T) {
	pmwTest := helpertest.CreateTestPlaceManifestWork("pmw-test", "default", "place-test")
	mw, _ := CreateManifestWork(pmwTest, "cluster1")
	fakeClient := fakeclient.NewSimpleClientset(pmwTest, mw)
	manifestWorkInformerFactory := workinformers.NewSharedInformerFactoryWithOptions(fakeClient, 1*time.Second)
	mwLister := manifestWorkInformerFactory.Work().V1().ManifestWorks().Lister()

	finalizerController := finalizeReconciler{
		workClient:         fakeClient,
		manifestWorkLister: mwLister,
		workApplier:        workapplier.NewWorkApplierWithTypedClient(fakeClient, mwLister),
	}

	// Set placeManifestWork delete time AND Set finalizer
	timeNow := metav1.Now()
	pmwTest.DeletionTimestamp = &timeNow
	pmwTest.Finalizers = append(pmwTest.Finalizers, PlaceManifestWorkFinalizer)

	pmwTest, _, err := finalizerController.reconcile(context.TODO(), pmwTest)
	if err != nil {
		t.Fatal(err)
	}

	// Check pmwTest finalizer removed
	if slices.Contains(pmwTest.Finalizers, PlaceManifestWorkFinalizer) {
		t.Fatal("Finalizer not deleted", pmwTest.Finalizers)
	}
}
