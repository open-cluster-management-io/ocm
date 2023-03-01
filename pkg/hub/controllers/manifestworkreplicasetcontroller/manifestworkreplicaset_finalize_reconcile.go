package manifestworkreplicasetcontroller

import (
	"context"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	worklisterv1 "open-cluster-management.io/api/client/work/listers/work/v1"
	"open-cluster-management.io/api/utils/work/v1/workapplier"
	workapiv1alpha1 "open-cluster-management.io/api/work/v1alpha1"
	"open-cluster-management.io/work/pkg/helper"
)

// finalizeReconciler is to finalize the manifestWorkReplicaSet by deleting all related manifestWorks.
type finalizeReconciler struct {
	workApplier        *workapplier.WorkApplier
	workClient         workclientset.Interface
	manifestWorkLister worklisterv1.ManifestWorkLister
}

func (f *finalizeReconciler) reconcile(ctx context.Context, mwrSet *workapiv1alpha1.ManifestWorkReplicaSet) (*workapiv1alpha1.ManifestWorkReplicaSet, reconcileState, error) {
	if mwrSet.DeletionTimestamp.IsZero() {
		return mwrSet, reconcileContinue, nil
	}

	var found bool
	for i := range mwrSet.Finalizers {
		if mwrSet.Finalizers[i] == ManifestWorkReplicaSetFinalizer {
			found = true
			break
		}
	}
	// if there is no finalizer, we do not need to reconcile anymore and we do not need to
	if !found {
		return mwrSet, reconcileStop, nil
	}

	if err := f.finalizeManifestWorkReplicaSet(ctx, mwrSet); err != nil {
		return mwrSet, reconcileContinue, err
	}

	// Remove finalizer after delete all created Manifestworks
	if helper.RemoveFinalizer(mwrSet, ManifestWorkReplicaSetFinalizer) {
		_, err := f.workClient.WorkV1alpha1().ManifestWorkReplicaSets(mwrSet.Namespace).Update(ctx, mwrSet, metav1.UpdateOptions{})
		if err != nil {
			return mwrSet, reconcileContinue, err
		}
	}

	return mwrSet, reconcileStop, nil
}

func (m *finalizeReconciler) finalizeManifestWorkReplicaSet(ctx context.Context, manifestWorkReplicaSet *workapiv1alpha1.ManifestWorkReplicaSet) error {
	req, err := labels.NewRequirement(ManifestWorkReplicaSetControllerNameLabelKey, selection.In, []string{manifestWorkReplicaSet.Name})
	if err != nil {
		return err
	}

	selector := labels.NewSelector().Add(*req)
	manifestWorks, err := m.manifestWorkLister.List(selector)
	if err != nil {
		return err
	}

	errs := []error{}
	for _, mw := range manifestWorks {
		err = m.workApplier.Delete(ctx, mw.Namespace, mw.Name)
		if err != nil {
			errs = append(errs, err)
		}
	}

	return utilerrors.NewAggregate(errs)
}
