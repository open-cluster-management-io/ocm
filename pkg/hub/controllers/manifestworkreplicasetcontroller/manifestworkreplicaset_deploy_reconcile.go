package manifestworkreplicasetcontroller

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
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
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

func (d *deployReconciler) reconcile(ctx context.Context, mwrSet *workapiv1alpha1.ManifestWorkReplicaSet) (*workapiv1alpha1.ManifestWorkReplicaSet, reconcileState, error) {
	// Manifestwork create/update/delete logic.
	var placements []*clusterv1beta1.Placement
	for _, placementRef := range mwrSet.Spec.PlacementRefs {
		placement, err := d.placementLister.Placements(mwrSet.Namespace).Get(placementRef.Name)
		if err != nil {
			if errors.IsNotFound(err) {
				apimeta.SetStatusCondition(&mwrSet.Status.Conditions, GetPlacementDecisionVerified(workapiv1alpha1.ReasonPlacementDecisionNotFound, ""))
			}
			return mwrSet, reconcileContinue, fmt.Errorf("Failed get placement %w", err)
		}
		placements = append(placements, placement)
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

	errs := []error{}
	addedClusters, deletedClusters, existingClusters := sets.NewString(), sets.NewString(), sets.NewString()
	for _, mw := range manifestWorks {
		existingClusters.Insert(mw.Namespace)
	}

	for _, placement := range placements {
		added, deleted, err := helper.GetClusters(d.placeDecisionLister, placement, existingClusters)
		if err != nil {
			apimeta.SetStatusCondition(&mwrSet.Status.Conditions, GetPlacementDecisionVerified(workapiv1alpha1.ReasonNotAsExpected, ""))

			return mwrSet, reconcileContinue, utilerrors.NewAggregate(errs)
		}

		addedClusters = addedClusters.Union(added)
		deletedClusters = deletedClusters.Union(deleted)
	}

	// Create manifestWork for added clusters
	for cls := range addedClusters {
		mw, err := CreateManifestWork(mwrSet, cls)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		_, err = d.workApplier.Apply(ctx, mw)
		if err != nil {
			errs = append(errs, err)
		}
	}

	// Update manifestWorks in case there are changes at ManifestWork or ManifestWorkReplicaSet
	for cls := range existingClusters {
		// Delete manifestWork for deleted clusters
		if deletedClusters.Has(cls) {
			err = d.workApplier.Delete(ctx, cls, mwrSet.Name)
			if err != nil {
				errs = append(errs, err)
			}
			continue
		}

		mw, err := CreateManifestWork(mwrSet, cls)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		_, err = d.workApplier.Apply(ctx, mw)
		if err != nil {
			errs = append(errs, err)
		}
	}

	// Set the Summary
	if mwrSet.Status.Summary == (workapiv1alpha1.ManifestWorkReplicaSetSummary{}) {
		mwrSet.Status.Summary = workapiv1alpha1.ManifestWorkReplicaSetSummary{}
	}
	total := len(existingClusters) - len(deletedClusters) + len(addedClusters)
	if total < 0 {
		total = 0
	}

	mwrSet.Status.Summary.Total = total
	if total == 0 {
		mwrSet.Status.Summary.Applied = 0
		mwrSet.Status.Summary.Available = 0
		mwrSet.Status.Summary.Degraded = 0
		mwrSet.Status.Summary.Progressing = 0
		apimeta.SetStatusCondition(&mwrSet.Status.Conditions, GetPlacementDecisionVerified(workapiv1alpha1.ReasonPlacementDecisionEmpty, ""))
	} else {
		apimeta.SetStatusCondition(&mwrSet.Status.Conditions, GetPlacementDecisionVerified(workapiv1alpha1.ReasonAsExpected, ""))
	}

	return mwrSet, reconcileContinue, utilerrors.NewAggregate(errs)
}

// Return only True status if there all clusters have manifests applied as expected
func GetManifestworkApplied(reason string, message string) metav1.Condition {
	if reason == workapiv1alpha1.ReasonAsExpected {
		return getCondition(workapiv1alpha1.ManifestWorkReplicaSetConditionManifestworkApplied, reason, message, metav1.ConditionTrue)
	}

	return getCondition(workapiv1alpha1.ManifestWorkReplicaSetConditionManifestworkApplied, reason, message, metav1.ConditionFalse)

}

// Return only True status if there are clusters selected
func GetPlacementDecisionVerified(reason string, message string) metav1.Condition {
	if reason == workapiv1alpha1.ReasonAsExpected {
		return getCondition(workapiv1alpha1.ManifestWorkReplicaSetConditionPlacementVerified, reason, message, metav1.ConditionTrue)
	}

	return getCondition(workapiv1alpha1.ManifestWorkReplicaSetConditionPlacementVerified, reason, message, metav1.ConditionFalse)
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

func CreateManifestWork(mwrSet *workapiv1alpha1.ManifestWorkReplicaSet, clusterNS string) (*workv1.ManifestWork, error) {
	if clusterNS == "" {
		return nil, fmt.Errorf("Invalid cluster namespace")
	}

	return &workv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mwrSet.Name,
			Namespace: clusterNS,
			Labels:    map[string]string{ManifestWorkReplicaSetControllerNameLabelKey: mwrSet.Name},
		},
		Spec: mwrSet.Spec.ManifestWorkTemplate}, nil
}
