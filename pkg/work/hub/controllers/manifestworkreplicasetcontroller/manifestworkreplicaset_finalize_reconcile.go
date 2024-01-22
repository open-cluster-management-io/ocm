package manifestworkreplicasetcontroller

import (
	"context"

	"k8s.io/apimachinery/pkg/api/errors"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"

	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	worklisterv1 "open-cluster-management.io/api/client/work/listers/work/v1"
	workapiv1alpha1 "open-cluster-management.io/api/work/v1alpha1"
	workapplier "open-cluster-management.io/sdk-go/pkg/apis/work/v1/applier"
	"open-cluster-management.io/sdk-go/pkg/patcher"
)

// finalizeReconciler is to finalize the manifestWorkReplicaSet by deleting all related manifestWorks.
type finalizeReconciler struct {
	workApplier        *workapplier.WorkApplier
	workClient         workclientset.Interface
	manifestWorkLister worklisterv1.ManifestWorkLister
}

func (f *finalizeReconciler) reconcile(ctx context.Context, mwrSet *workapiv1alpha1.ManifestWorkReplicaSet,
) (*workapiv1alpha1.ManifestWorkReplicaSet, reconcileState, error) {
	if mwrSet.DeletionTimestamp.IsZero() {
		return mwrSet, reconcileContinue, nil
	}

	if err := f.finalizeManifestWorkReplicaSet(ctx, mwrSet); err != nil {
		return mwrSet, reconcileContinue, err
	}

	workSetPatcher := patcher.NewPatcher[
		*workapiv1alpha1.ManifestWorkReplicaSet, workapiv1alpha1.ManifestWorkReplicaSetSpec, workapiv1alpha1.ManifestWorkReplicaSetStatus](
		f.workClient.WorkV1alpha1().ManifestWorkReplicaSets(mwrSet.Namespace))

	// Remove finalizer after delete all created Manifestworks
	if err := workSetPatcher.RemoveFinalizer(ctx, mwrSet, ManifestWorkReplicaSetFinalizer); err != nil {
		return mwrSet, reconcileContinue, err
	}

	return mwrSet, reconcileStop, nil
}

func (f *finalizeReconciler) finalizeManifestWorkReplicaSet(ctx context.Context, manifestWorkReplicaSet *workapiv1alpha1.ManifestWorkReplicaSet) error {
	manifestWorks, err := listManifestWorksByManifestWorkReplicaSet(manifestWorkReplicaSet, f.manifestWorkLister)
	if err != nil {
		return err
	}

	var errs []error
	for _, mw := range manifestWorks {
		err = f.workApplier.Delete(ctx, mw.Namespace, mw.Name)
		if err != nil && !errors.IsNotFound(err) {
			errs = append(errs, err)
		}
	}

	return utilerrors.NewAggregate(errs)
}
