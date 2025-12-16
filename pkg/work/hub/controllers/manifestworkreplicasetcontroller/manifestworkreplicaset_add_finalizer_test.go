package manifestworkreplicasetcontroller

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	fakeclient "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	workapiv1alpha1 "open-cluster-management.io/api/work/v1alpha1"

	commonhelper "open-cluster-management.io/ocm/pkg/common/helpers"
	helpertest "open-cluster-management.io/ocm/pkg/work/hub/test"
)

// Test add finalizer reconcile
func TestAddFinalizerReconcile(t *testing.T) {
	mwrSetTest := helpertest.CreateTestManifestWorkReplicaSet("mwrset-test", "default", "place-test")
	fakeClient := fakeclient.NewSimpleClientset(mwrSetTest)

	addFinalizerController := &addFinalizerReconciler{
		workClient: fakeClient,
	}

	_, _, err := addFinalizerController.reconcile(context.TODO(), mwrSetTest)
	if err != nil {
		t.Fatal(err)
	}

	updatetSet, err := fakeClient.WorkV1alpha1().ManifestWorkReplicaSets(mwrSetTest.Namespace).Get(context.TODO(), mwrSetTest.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if !commonhelper.HasFinalizer(updatetSet.Finalizers, workapiv1alpha1.ManifestWorkReplicaSetFinalizer) {
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
	if commonhelper.HasFinalizer(mwrSetTest.Finalizers, workapiv1alpha1.ManifestWorkReplicaSetFinalizer) {
		t.Fatal("Finalizer added to deleted ManifestWorkReplicaSet")
	}
}

func TestAddFinalizerTwiceReconcile(t *testing.T) {
	mwrSetTest := helpertest.CreateTestManifestWorkReplicaSet("mwrset-test", "default", "place-test")
	mwrSetTest.Finalizers = []string{workapiv1alpha1.ManifestWorkReplicaSetFinalizer}
	fakeClient := fakeclient.NewSimpleClientset(mwrSetTest)

	addFinalizerController := &addFinalizerReconciler{
		workClient: fakeClient,
	}

	_, _, err := addFinalizerController.reconcile(context.TODO(), mwrSetTest)
	if err != nil {
		t.Fatal(err)
	}

	// Check fakeClient Actions does not have update action
	if len(fakeClient.Actions()) > 0 {
		t.Fatal("PlaceMW finalizer should not be updated")
	}
}
