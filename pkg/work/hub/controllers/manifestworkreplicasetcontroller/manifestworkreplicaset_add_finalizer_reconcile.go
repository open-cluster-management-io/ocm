package manifestworkreplicasetcontroller

import (
	"context"

	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	workapiv1alpha1 "open-cluster-management.io/api/work/v1alpha1"
	"open-cluster-management.io/sdk-go/pkg/patcher"
)

// addFinalizerReconciler is to add finalizer to the manifestworkreplicaset.
type addFinalizerReconciler struct {
	workClient workclientset.Interface
}

func (a *addFinalizerReconciler) reconcile(ctx context.Context, pw *workapiv1alpha1.ManifestWorkReplicaSet,
) (*workapiv1alpha1.ManifestWorkReplicaSet, reconcileState, error) {
	// Do not need to add finalizer if it is in delete state already.
	if !pw.DeletionTimestamp.IsZero() {
		return pw, reconcileStop, nil
	}

	workSetPatcher := patcher.NewPatcher[
		*workapiv1alpha1.ManifestWorkReplicaSet, workapiv1alpha1.ManifestWorkReplicaSetSpec, workapiv1alpha1.ManifestWorkReplicaSetStatus](
		a.workClient.WorkV1alpha1().ManifestWorkReplicaSets(pw.Namespace))

	updated, err := workSetPatcher.AddFinalizer(ctx, pw, ManifestWorkReplicaSetFinalizer)
	// if this conflicts, we'll simply try again later
	if updated {
		return pw, reconcileStop, err
	}

	return pw, reconcileContinue, err
}
