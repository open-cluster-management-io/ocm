package manifestworkreplicasetcontroller

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

// statusReconciler is to update manifestWorkReplicaSet status.
type statusReconciler struct {
	manifestWorkLister worklisterv1.ManifestWorkLister
}

func (d *statusReconciler) reconcile(ctx context.Context, mwrSet *workapiv1alpha1.ManifestWorkReplicaSet) (*workapiv1alpha1.ManifestWorkReplicaSet, reconcileState, error) {
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

	req, err := labels.NewRequirement(ManifestWorkReplicaSetControllerNameLabelKey, selection.In, []string{mwrSet.Name})
	if err != nil {
		return mwrSet, reconcileContinue, err
	}

	selector := labels.NewSelector().Add(*req)
	manifestWorks, err := d.manifestWorkLister.List(selector)
	if err != nil {
		return mwrSet, reconcileContinue, err
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

	mwrSet.Status.Summary.Available = availableCount
	mwrSet.Status.Summary.Degraded = degradCount
	mwrSet.Status.Summary.Progressing = processingCount
	mwrSet.Status.Summary.Applied = appliedCount

	if mwrSet.Status.Summary.Available == mwrSet.Status.Summary.Total &&
		mwrSet.Status.Summary.Progressing == 0 && mwrSet.Status.Summary.Degraded == 0 {
		apimeta.SetStatusCondition(&mwrSet.Status.Conditions, GetManifestworkApplied(workapiv1alpha1.ReasonAsExpected, ""))
	} else if mwrSet.Status.Summary.Progressing > 0 && mwrSet.Status.Summary.Degraded == 0 {
		apimeta.SetStatusCondition(&mwrSet.Status.Conditions, GetManifestworkApplied(workapiv1alpha1.ReasonProcessing, ""))
	} else {
		apimeta.SetStatusCondition(&mwrSet.Status.Conditions, GetManifestworkApplied(workapiv1alpha1.ReasonNotAsExpected, ""))
	}

	return mwrSet, reconcileContinue, nil
}
