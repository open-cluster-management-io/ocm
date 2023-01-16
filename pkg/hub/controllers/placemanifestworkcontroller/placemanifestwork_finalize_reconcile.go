package placemanifestworkcontroller

import (
	"context"
	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	worklisterv1 "open-cluster-management.io/api/client/work/listers/work/v1"
	workapiv1alpha1 "open-cluster-management.io/api/work/v1alpha1"
)

// finalizeReconciler is to finalize the placeManifestWork by deleteing all related manifestWorks.
type finalizeReconciler struct {
	workClient         workclientset.Interface
	manifestWorkLister worklisterv1.ManifestWorkLister
}

func (f *finalizeReconciler) reconcile(ctx context.Context, pw *workapiv1alpha1.PlaceManifestWork) (*workapiv1alpha1.PlaceManifestWork, reconcileState, error) {
	if pw.DeletionTimestamp.IsZero() {
		return pw, reconcileContinue, nil
	}

	var found bool
	for i := range pw.Finalizers {
		if pw.Finalizers[i] == PlaceManifestWorkFinalizer {
			found = true
			break
		}
	}
	// if there is no finalizer, we do not need to reconcile anymore and we do not need to
	if !found {
		return pw, reconcileStop, nil
	}

	if err := f.finalizePlaceManifestWork(ctx, pw); err != nil {
		return pw, reconcileStop, err
	}

	// TODO remove finalizer finally
	return pw, reconcileStop, nil
}

func (m *finalizeReconciler) finalizePlaceManifestWork(ctx context.Context, placeManifestWork *workapiv1alpha1.PlaceManifestWork) error {
	// TODO: Delete all ManifestWork owned by the given placeManifestWork
	return nil
}
