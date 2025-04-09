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
	// The logic for update manifestWorkReplicaSet status
	if mwrSet.Status.Summary.Total == 0 {
		condition := apimeta.FindStatusCondition(mwrSet.Status.Conditions, workapiv1alpha1.ManifestWorkReplicaSetConditionPlacementVerified)
		if condition != nil && condition.Reason == workapiv1alpha1.ReasonPlacementDecisionEmpty {
			apimeta.SetStatusCondition(&mwrSet.Status.Conditions, GetManifestworkApplied(workapiv1alpha1.ReasonPlacementDecisionEmpty, ""))
		} else {
			apimeta.SetStatusCondition(&mwrSet.Status.Conditions, GetManifestworkApplied(workapiv1alpha1.ReasonNotAsExpected, ""))
		}

		return mwrSet, reconcileContinue, nil
	}

	appliedCount, availableCount, degradCount, processingCount := 0, 0, 0, 0
	for id, plcSummary := range mwrSet.Status.PlacementsSummary {
		manifestWorks, err := listManifestWorksByMWRSetPlacementRef(mwrSet, plcSummary.Name, d.manifestWorkLister)
		if err != nil {
			return mwrSet, reconcileContinue, err
		}

		applied, available, degrad, processing := 0, 0, 0, 0
		for _, mw := range manifestWorks {
			if !mw.DeletionTimestamp.IsZero() {
				continue
			}

			// Check if ManifestWorkTemplate changes, ManifestWork will need to be updated.
			newMW := &workapiv1.ManifestWork{}
			mw.ObjectMeta.DeepCopyInto(&newMW.ObjectMeta)
			mwrSet.Spec.ManifestWorkTemplate.DeepCopyInto(&newMW.Spec)
			if !workapplier.ManifestWorkEqual(newMW, mw) {
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

	if mwrSet.Status.Summary.Available == mwrSet.Status.Summary.Total && //nolint:gocritic
		mwrSet.Status.Summary.Progressing == 0 && mwrSet.Status.Summary.Degraded == 0 {
		apimeta.SetStatusCondition(&mwrSet.Status.Conditions, GetManifestworkApplied(workapiv1alpha1.ReasonAsExpected, ""))
	} else if mwrSet.Status.Summary.Progressing > 0 && mwrSet.Status.Summary.Degraded == 0 {
		apimeta.SetStatusCondition(&mwrSet.Status.Conditions, GetManifestworkApplied(workapiv1alpha1.ReasonProcessing, ""))
	} else {
		apimeta.SetStatusCondition(&mwrSet.Status.Conditions, GetManifestworkApplied(workapiv1alpha1.ReasonNotAsExpected, ""))
	}

	return mwrSet, reconcileContinue, nil
}
