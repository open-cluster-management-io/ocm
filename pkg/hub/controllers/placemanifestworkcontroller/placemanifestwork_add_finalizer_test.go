package placemanifestworkcontroller

import (
	"context"
	"golang.org/x/exp/slices"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakeclient "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	helpertest "open-cluster-management.io/work/pkg/hub/test"
	"testing"
)

// Test add finalizer reconcile
func TestAddFinalizerReconcile(t *testing.T) {
	pmwTest := helpertest.CreateTestPlaceManifestWork("pmw-test", "default", "place-test")
	fakeClient := fakeclient.NewSimpleClientset(pmwTest)

	addFinalizerController := &addFinalizerReconciler{
		workClient: fakeClient,
	}

	pmwTest, _, err := addFinalizerController.reconcile(context.TODO(), pmwTest)
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Contains(pmwTest.Finalizers, PlaceManifestWorkFinalizer) {
		t.Fatal("Finalizer did not added")
	}

	// Set placeManifestWork delete time AND remove finalizer
	timeNow := metav1.Now()
	pmwTest.DeletionTimestamp = &timeNow
	pmwTest.Finalizers = []string{}

	// Run reconcile
	pmwTest, _, err = addFinalizerController.reconcile(context.TODO(), pmwTest)
	if err != nil {
		t.Fatal(err)
	}

	// Check finalizer not exist
	if slices.Contains(pmwTest.Finalizers, PlaceManifestWorkFinalizer) {
		t.Fatal("Finalizer added to deleted placemanifestWork")
	}
}

func TestAddFinalizerTwiceReconcile(t *testing.T) {
	pmwTest := helpertest.CreateTestPlaceManifestWork("pmw-test", "default", "place-test")
	pmwTest.Finalizers = []string{PlaceManifestWorkFinalizer}
	fakeClient := fakeclient.NewSimpleClientset(pmwTest)

	addFinalizerController := &addFinalizerReconciler{
		workClient: fakeClient,
	}

	pmwTest, _, err := addFinalizerController.reconcile(context.TODO(), pmwTest)
	if err != nil {
		t.Fatal(err)
	}

	// Check fakeClient Actions does not have update action
	if len(fakeClient.Actions()) > 0 {
		t.Fatal("PlaceMW finalizer should not be updated")
	}
}
