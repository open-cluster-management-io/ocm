package manifestworkreplicasetcontroller

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
	mwrSetTest := helpertest.CreateTestManifestWorkReplicaSet("mwrset-test", "default", "place-test")
	fakeClient := fakeclient.NewSimpleClientset(mwrSetTest)

	addFinalizerController := &addFinalizerReconciler{
		workClient: fakeClient,
	}

	mwrSetTest, _, err := addFinalizerController.reconcile(context.TODO(), mwrSetTest)
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Contains(mwrSetTest.Finalizers, ManifestWorkReplicaSetFinalizer) {
		t.Fatal("Finalizer did not added")
	}

	// Set ManifestWorkReplicaSet delete time AND remove finalizer
	timeNow := metav1.Now()
	mwrSetTest.DeletionTimestamp = &timeNow
	mwrSetTest.Finalizers = []string{}

	// Run reconcile
	mwrSetTest, _, err = addFinalizerController.reconcile(context.TODO(), mwrSetTest)
	if err != nil {
		t.Fatal(err)
	}

	// Check finalizer not exist
	if slices.Contains(mwrSetTest.Finalizers, ManifestWorkReplicaSetFinalizer) {
		t.Fatal("Finalizer added to deleted ManifestWorkReplicaSet")
	}
}

func TestAddFinalizerTwiceReconcile(t *testing.T) {
	mwrSetTest := helpertest.CreateTestManifestWorkReplicaSet("mwrset-test", "default", "place-test")
	mwrSetTest.Finalizers = []string{ManifestWorkReplicaSetFinalizer}
	fakeClient := fakeclient.NewSimpleClientset(mwrSetTest)

	addFinalizerController := &addFinalizerReconciler{
		workClient: fakeClient,
	}

	mwrSetTest, _, err := addFinalizerController.reconcile(context.TODO(), mwrSetTest)
	if err != nil {
		t.Fatal(err)
	}

	// Check fakeClient Actions does not have update action
	if len(fakeClient.Actions()) > 0 {
		t.Fatal("PlaceMW finalizer should not be updated")
	}
}
