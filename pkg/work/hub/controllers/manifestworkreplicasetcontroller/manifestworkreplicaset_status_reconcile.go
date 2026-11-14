package manifestworkreplicasetcontroller

import (
	"context"

	apimeta "k8s.io/apimachinery/pkg/api/meta"

	worklisterv1 "open-cluster-management.io/api/client/work/listers/work/v1"
	"open-cluster-management.io/api/utils/work/v1/workapplier"
	workapiv1 "open-cluster-management.io/api/work/v1"
	workapiv1alpha1 "open-cluster-management.io/api/work/v1alpha1"
)

// statusReconciler is to update manifestWorkReplicaSet status.
type statusReconciler struct {
	manifestWorkLister worklisterv1.ManifestWorkLister
}

func (d *statusReconciler) reconcile(ctx context.Context, mwrSet *workapiv1alpha1.ManifestWorkReplicaSet,
) (*workapiv1alpha1.ManifestWorkReplicaSet, reconcileState, error) {
	// Missing placements are absent from work summaries, so use deploy's condition
	// to prevent the valid subset of works from reporting overall success.
	verified := apimeta.FindStatusCondition(mwrSet.Status.Conditions, workapiv1alpha1.ManifestWorkReplicaSetConditionPlacementVerified)
	hasMissingPlacement := verified != nil && verified.ObservedGeneration == mwrSet.Generation &&
		verified.Reason == workapiv1alpha1.ReasonPlacementDecisionNotFound

	if mwrSet.Status.Summary.Total == 0 {
		if hasMissingPlacement {
			apimeta.SetStatusCondition(&mwrSet.Status.Conditions, getManifestworkApplied(mwrSet.Generation, workapiv1alpha1.ReasonNotAsExpected, verified.Message))
		} else if verified != nil && verified.Reason == workapiv1alpha1.ReasonPlacementDecisionEmpty {
			apimeta.SetStatusCondition(&mwrSet.Status.Conditions, getManifestworkApplied(mwrSet.Generation, workapiv1alpha1.ReasonPlacementDecisionEmpty, ""))
		} else {
			apimeta.SetStatusCondition(&mwrSet.Status.Conditions, getManifestworkApplied(mwrSet.Generation, workapiv1alpha1.ReasonNotAsExpected, ""))
		}

		return mwrSet, reconcileContinue, nil
	}

	appliedCount, availableCount, degradCount, processingCount := 0, 0, 0, 0

	allManifestWorks, _, err := getManifestWorkInReplicaSet(mwrSet, d.manifestWorkLister)
	if err != nil {
		return mwrSet, reconcileContinue, err
	}

	for id, plcSummary := range mwrSet.Status.PlacementsSummary {
		manifestWorks := allManifestWorks.workByPlacement[plcSummary.Name]
		applied, available, degrad, processing, counted := 0, 0, 0, 0, 0
		for _, mw := range manifestWorks {
			// Check if ManifestWorkTemplate changes, ManifestWork will need to be updated.
			newMW := &workapiv1.ManifestWork{}
			mw.ObjectMeta.DeepCopyInto(&newMW.ObjectMeta)
			mwrSet.Spec.ManifestWorkTemplate.DeepCopyInto(&newMW.Spec)
			if !workapplier.ManifestWorkEqual(newMW, mw) {
				continue
			}
			// Deploy keeps deleting current-template works in Total unless their cluster was removed.
			counted++
			if !mw.DeletionTimestamp.IsZero() {
				continue
			}

			appliedCond := apimeta.FindStatusCondition(mw.Status.Conditions, workapiv1.WorkApplied)
			progressingCond := apimeta.FindStatusCondition(mw.Status.Conditions, workapiv1.WorkProgressing)
			availableCond := apimeta.FindStatusCondition(mw.Status.Conditions, workapiv1.WorkAvailable)
			degradedCond := apimeta.FindStatusCondition(mw.Status.Conditions, workapiv1.WorkDegraded)
			// Progressing and Degraded may be absent, but Applied and Available must reflect
			// this ManifestWork generation before their status can be counted.
			if !isConditionReady(appliedCond, mw.Generation, false) ||
				(progressingCond != nil && !isConditionReady(progressingCond, mw.Generation, false)) ||
				!isConditionReady(availableCond, mw.Generation, false) ||
				(degradedCond != nil && !isConditionReady(degradedCond, mw.Generation, false)) {
				processing++
				continue
			}

			// applied condition
			if apimeta.IsStatusConditionTrue(mw.Status.Conditions, workapiv1.WorkApplied) {
				applied++
			}
			// Progressing condition
			if apimeta.IsStatusConditionTrue(mw.Status.Conditions, workapiv1.WorkProgressing) {
				processing++
			}
			// Available condition
			if apimeta.IsStatusConditionTrue(mw.Status.Conditions, workapiv1.WorkAvailable) {
				available++
			}
			// Degraded condition
			if apimeta.IsStatusConditionTrue(mw.Status.Conditions, workapiv1.WorkDegraded) {
				degrad++
			}
		}
		// Works in Total may not be cached with the current template yet (just created or updated).
		if missing := plcSummary.Summary.Total - counted; missing > 0 {
			processing += missing
		}
		mwrSet.Status.PlacementsSummary[id].Summary.Applied = applied
		mwrSet.Status.PlacementsSummary[id].Summary.Progressing = processing
		mwrSet.Status.PlacementsSummary[id].Summary.Available = available
		mwrSet.Status.PlacementsSummary[id].Summary.Degraded = degrad
		// Set the manifestWorkReplicaSet count
		appliedCount += applied
		processingCount += processing
		availableCount += available
		degradCount += degrad
	}

	mwrSet.Status.Summary.Available = availableCount
	mwrSet.Status.Summary.Degraded = degradCount
	mwrSet.Status.Summary.Progressing = processingCount
	mwrSet.Status.Summary.Applied = appliedCount

	if hasMissingPlacement {
		apimeta.SetStatusCondition(&mwrSet.Status.Conditions, getManifestworkApplied(mwrSet.Generation, workapiv1alpha1.ReasonNotAsExpected, verified.Message))
	} else if mwrSet.Status.Summary.Applied == mwrSet.Status.Summary.Total && //nolint:gocritic
		mwrSet.Status.Summary.Available == mwrSet.Status.Summary.Total &&
		mwrSet.Status.Summary.Progressing == 0 && mwrSet.Status.Summary.Degraded == 0 {
		apimeta.SetStatusCondition(&mwrSet.Status.Conditions, getManifestworkApplied(mwrSet.Generation, workapiv1alpha1.ReasonAsExpected, ""))
	} else if mwrSet.Status.Summary.Progressing > 0 && mwrSet.Status.Summary.Degraded == 0 {
		apimeta.SetStatusCondition(&mwrSet.Status.Conditions, getManifestworkApplied(mwrSet.Generation, workapiv1alpha1.ReasonProcessing, ""))
	} else {
		apimeta.SetStatusCondition(&mwrSet.Status.Conditions, getManifestworkApplied(mwrSet.Generation, workapiv1alpha1.ReasonNotAsExpected, ""))
	}

	return mwrSet, reconcileContinue, nil
}
