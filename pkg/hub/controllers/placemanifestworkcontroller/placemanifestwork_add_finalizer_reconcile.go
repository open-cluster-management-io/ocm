package placemanifestworkcontroller

import (
	"context"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	workapiv1alpha1 "open-cluster-management.io/api/work/v1alpha1"
)

// addFinalizerReconciler is to add finalizer to the placeManifestWork.
type addFinalizerReconciler struct {
	workClient workclientset.Interface
}

func (a *addFinalizerReconciler) reconcile(ctx context.Context, pw *workapiv1alpha1.PlaceManifestWork) (*workapiv1alpha1.PlaceManifestWork, reconcileState, error) {
	// Do not need to add finalizer if it is in delete state already.
	if !pw.DeletionTimestamp.IsZero() {
		return pw, reconcileStop, nil
	}

	// don't add finalizer to instances that already have it
	for i := range pw.Finalizers {
		if pw.Finalizers[i] == PlaceManifestWorkFinalizer {
			return pw, reconcileContinue, nil
		}
	}
	// if this conflicts, we'll simply try again later
	pw.Finalizers = append(pw.Finalizers, PlaceManifestWorkFinalizer)
	_, err := a.workClient.WorkV1alpha1().PlaceManifestWorks(pw.Namespace).Update(ctx, pw, metav1.UpdateOptions{})
	return pw, reconcileStop, err
}
