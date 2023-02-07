package placemanifestworkcontroller

import (
	"context"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	worklisterv1 "open-cluster-management.io/api/client/work/listers/work/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"
	workapiv1alpha1 "open-cluster-management.io/api/work/v1alpha1"
)

// statusReconciler is to update placeManifestwork status.
type statusReconciler struct {
	manifestWorkLister worklisterv1.ManifestWorkLister
}

func (d *statusReconciler) reconcile(ctx context.Context, pw *workapiv1alpha1.PlaceManifestWork) (*workapiv1alpha1.PlaceManifestWork, reconcileState, error) {
	// The logic for update placeManifestwork status
	if pw.Status.PlacedManifestWorkSummary.Total == 0 {
		condition := apimeta.FindStatusCondition(pw.Status.Conditions, string(workapiv1alpha1.PlacementDecisionVerified))
		if condition != nil && condition.Reason == workapiv1alpha1.PlacementDecisionEmpty {
			apimeta.SetStatusCondition(&pw.Status.Conditions, GetManifestworkApplied(workapiv1alpha1.PlacementDecisionEmpty, ""))
		} else {
			apimeta.SetStatusCondition(&pw.Status.Conditions, GetManifestworkApplied(workapiv1alpha1.NotAsExpected, ""))
		}

		return pw, reconcileContinue, nil
	}

	req, err := labels.NewRequirement(PlaceManifestWorkControllerNameLabelKey, selection.In, []string{pw.Name})
	if err != nil {
		return pw, reconcileContinue, err
	}

	selector := labels.NewSelector().Add(*req)
	manifestWorks, err := d.manifestWorkLister.List(selector)
	if err != nil {
		return pw, reconcileContinue, err
	}

	appliedCount, availableCount, degradCount, processingCount := 0, 0, 0, 0
	for _, mw := range manifestWorks {
		if !mw.DeletionTimestamp.IsZero() {
			continue
		}

		// applied condition
		condition := apimeta.FindStatusCondition(mw.Status.Conditions, string(workapiv1.ManifestApplied))
		if condition != nil && condition.Status == metav1.ConditionTrue {
			appliedCount++
		}
		// Progressing condition
		condition = apimeta.FindStatusCondition(mw.Status.Conditions, string(workapiv1.ManifestProgressing))
		if condition != nil && condition.Status == metav1.ConditionTrue {
			processingCount++
		}
		// Available condition
		condition = apimeta.FindStatusCondition(mw.Status.Conditions, string(workapiv1.ManifestAvailable))
		if condition != nil && condition.Status == metav1.ConditionTrue {
			availableCount++
		}
		// Degraded condition
		condition = apimeta.FindStatusCondition(mw.Status.Conditions, string(workapiv1.ManifestDegraded))
		if condition != nil && condition.Status == metav1.ConditionTrue {
			degradCount++
		}
	}

	pw.Status.PlacedManifestWorkSummary.Available = availableCount
	pw.Status.PlacedManifestWorkSummary.Degraded = degradCount
	pw.Status.PlacedManifestWorkSummary.Progressing = processingCount
	pw.Status.PlacedManifestWorkSummary.Applied = appliedCount

	if pw.Status.PlacedManifestWorkSummary.Available == pw.Status.PlacedManifestWorkSummary.Total &&
		pw.Status.PlacedManifestWorkSummary.Progressing == 0 && pw.Status.PlacedManifestWorkSummary.Degraded == 0 {
		apimeta.SetStatusCondition(&pw.Status.Conditions, GetManifestworkApplied(workapiv1alpha1.AsExpected, ""))
	} else if pw.Status.PlacedManifestWorkSummary.Progressing > 0 && pw.Status.PlacedManifestWorkSummary.Degraded == 0 {
		apimeta.SetStatusCondition(&pw.Status.Conditions, GetManifestworkApplied(workapiv1alpha1.Processing, ""))
	} else {
		apimeta.SetStatusCondition(&pw.Status.Conditions, GetManifestworkApplied(workapiv1alpha1.NotAsExpected, ""))
	}

	return pw, reconcileContinue, nil
}
