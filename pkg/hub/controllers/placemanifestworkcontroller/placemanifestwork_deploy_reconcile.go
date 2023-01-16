package placemanifestworkcontroller

import (
	"context"
	clusterlister "open-cluster-management.io/api/client/cluster/listers/cluster/v1beta1"
	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	worklisterv1 "open-cluster-management.io/api/client/work/listers/work/v1"
	workapiv1alpha1 "open-cluster-management.io/api/work/v1alpha1"
)

// deployReconciler is to manage ManifestWork based on the placement.
type deployReconciler struct {
	workClient          workclientset.Interface
	manifestWorkLister  worklisterv1.ManifestWorkLister
	placeDecisionLister clusterlister.PlacementDecisionLister
	placementLister     clusterlister.PlacementLister
}

func (d *deployReconciler) reconcile(ctx context.Context, pw *workapiv1alpha1.PlaceManifestWork) (*workapiv1alpha1.PlaceManifestWork, reconcileState, error) {
	// TODO the main manifestwork create/update/delete logic, we may also need to manage the status condition for whether manifestwork is created here
	return pw, reconcileContinue, nil
}
