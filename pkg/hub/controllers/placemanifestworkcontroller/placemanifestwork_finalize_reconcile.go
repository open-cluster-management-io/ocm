package placemanifestworkcontroller

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

// finalizeReconciler is to finalize the placeManifestWork by deleting all related manifestWorks.
type finalizeReconciler struct {
	workApplier        *workapplier.WorkApplier
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
		return pw, reconcileContinue, err
	}

	// Remove finalizer after delete all created Manifestworks
	if helper.RemoveFinalizer(pw, PlaceManifestWorkFinalizer) {
		_, err := f.workClient.WorkV1alpha1().PlaceManifestWorks(pw.Namespace).Update(ctx, pw, metav1.UpdateOptions{})
		if err != nil {
			return pw, reconcileContinue, err
		}
	}

	return pw, reconcileStop, nil
}

func (m *finalizeReconciler) finalizePlaceManifestWork(ctx context.Context, placeManifestWork *workapiv1alpha1.PlaceManifestWork) error {
	req, err := labels.NewRequirement(PlaceManifestWorkControllerNameLabelKey, selection.In, []string{placeManifestWork.Name})
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
