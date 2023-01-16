package placemanifestworkcontroller

import (
	"context"
	clusterlister "open-cluster-management.io/api/client/cluster/listers/cluster/v1beta1"
	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	worklisterv1 "open-cluster-management.io/api/client/work/listers/work/v1"
	workapiv1alpha1 "open-cluster-management.io/api/work/v1alpha1"
)

// statusReconciler is to update placeManifestwork status.
type statusReconciler struct {
	workClient          workclientset.Interface
	manifestWorkLister  worklisterv1.ManifestWorkLister
	placeDecisionLister clusterlister.PlacementDecisionLister
}

func (d *statusReconciler) reconcile(ctx context.Context, pw *workapiv1alpha1.PlaceManifestWork) (*workapiv1alpha1.PlaceManifestWork, reconcileState, error) {
	// TODO the logic for update placeManifestwork status
	return pw, reconcileContinue, nil
}
