package placemanifestworkcontroller

import (
	"context"
	"fmt"
	"k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	clusterlister "open-cluster-management.io/api/client/cluster/listers/cluster/v1beta1"
	worklisterv1 "open-cluster-management.io/api/client/work/listers/work/v1"
	"open-cluster-management.io/api/utils/work/v1/workapplier"
	workv1 "open-cluster-management.io/api/work/v1"
	workapiv1alpha1 "open-cluster-management.io/api/work/v1alpha1"
	"open-cluster-management.io/work/pkg/helper"
)

// deployReconciler is to manage ManifestWork based on the placement.
type deployReconciler struct {
	workApplier         *workapplier.WorkApplier
	manifestWorkLister  worklisterv1.ManifestWorkLister
	placeDecisionLister clusterlister.PlacementDecisionLister
	placementLister     clusterlister.PlacementLister
}

func (d *deployReconciler) reconcile(ctx context.Context, pw *workapiv1alpha1.PlaceManifestWork) (*workapiv1alpha1.PlaceManifestWork, reconcileState, error) {
	// Manifestwork create/update/delete logic.
	placement, err := d.placementLister.Placements(pw.Namespace).Get(pw.Spec.PlacementRef.Name)
	if err != nil {
		if errors.IsNotFound(err) {
			apimeta.SetStatusCondition(&pw.Status.Conditions, GetPlacementDecisionVerified(workapiv1alpha1.PlacementDecisionNotFound, ""))
		}
		return pw, reconcileContinue, fmt.Errorf("Failed get placement %w", err)
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

	errs := []error{}
	existingClusters := sets.NewString()
	for _, mw := range manifestWorks {
		existingClusters.Insert(mw.Namespace)
	}

	addedClusters, deletedClusters, err := helper.GetClusters(d.placeDecisionLister, placement, existingClusters)
	if err != nil {
		apimeta.SetStatusCondition(&pw.Status.Conditions, GetPlacementDecisionVerified(workapiv1alpha1.NotAsExpected, ""))

		return pw, reconcileContinue, utilerrors.NewAggregate(errs)
	}

	// Create manifestWork for added clusters
	for cls := range addedClusters {
		mw, err := CreateManifestWork(pw, cls)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		_, err = d.workApplier.Apply(ctx, mw)
		if err != nil {
			errs = append(errs, err)
		}
	}

	// Update manifestWorks in case there are changes at ManifestWork or PlaceManifestWork
	for cls := range existingClusters {
		// Delete manifestWork for deleted clusters
		if deletedClusters.Has(cls) {
			err = d.workApplier.Delete(ctx, cls, pw.Name)
			if err != nil {
				errs = append(errs, err)
			}
			continue
		}

		mw, err := CreateManifestWork(pw, cls)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		_, err = d.workApplier.Apply(ctx, mw)
		if err != nil {
			errs = append(errs, err)
		}
	}

	// Set the PlacedManifestWorkSummary
	if pw.Status.PlacedManifestWorkSummary == (workapiv1alpha1.PlacedManifestWorkSummary{}) {
		pw.Status.PlacedManifestWorkSummary = workapiv1alpha1.PlacedManifestWorkSummary{}
	}
	total := len(existingClusters) - len(deletedClusters) + len(addedClusters)
	if total < 0 {
		total = 0
	}

	pw.Status.PlacedManifestWorkSummary.Total = total
	if total == 0 {
		pw.Status.PlacedManifestWorkSummary.Applied = 0
		pw.Status.PlacedManifestWorkSummary.Available = 0
		pw.Status.PlacedManifestWorkSummary.Degraded = 0
		pw.Status.PlacedManifestWorkSummary.Progressing = 0
		apimeta.SetStatusCondition(&pw.Status.Conditions, GetPlacementDecisionVerified(workapiv1alpha1.PlacementDecisionEmpty, ""))
	} else {
		apimeta.SetStatusCondition(&pw.Status.Conditions, GetPlacementDecisionVerified(workapiv1alpha1.AsExpected, ""))
	}

	return pw, reconcileContinue, utilerrors.NewAggregate(errs)
}

// Return only True status if there all clusters have manifests applied as expected
func GetManifestworkApplied(reason string, message string) metav1.Condition {
	if reason == workapiv1alpha1.AsExpected {
		return getCondition(string(workapiv1alpha1.ManifestworkApplied), reason, message, metav1.ConditionTrue)
	}

	return getCondition(string(workapiv1alpha1.ManifestworkApplied), reason, message, metav1.ConditionFalse)

}

// Return only True status if there are clusters selected
func GetPlacementDecisionVerified(reason string, message string) metav1.Condition {
	if reason == workapiv1alpha1.AsExpected {
		return getCondition(string(workapiv1alpha1.PlacementDecisionVerified), reason, message, metav1.ConditionTrue)
	}

	return getCondition(string(workapiv1alpha1.PlacementDecisionVerified), reason, message, metav1.ConditionFalse)
}

func getCondition(conditionType string, reason string, message string, status metav1.ConditionStatus) metav1.Condition {
	return metav1.Condition{
		Type:               conditionType,
		Reason:             reason,
		Message:            message,
		Status:             status,
		LastTransitionTime: metav1.Now(),
	}
}

func CreateManifestWork(pw *workapiv1alpha1.PlaceManifestWork, clusterNS string) (*workv1.ManifestWork, error) {
	if clusterNS == "" {
		return nil, fmt.Errorf("Invalid cluster namespace")
	}

	return &workv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pw.Name,
			Namespace: clusterNS,
			Labels:    map[string]string{PlaceManifestWorkControllerNameLabelKey: pw.Name},
		},
		Spec: pw.Spec.ManifestWorkTemplate}, nil
}
