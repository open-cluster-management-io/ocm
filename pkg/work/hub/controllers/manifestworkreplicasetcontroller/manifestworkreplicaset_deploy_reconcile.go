package manifestworkreplicasetcontroller

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"

	clusterlister "open-cluster-management.io/api/client/cluster/listers/cluster/v1beta1"
	worklisterv1 "open-cluster-management.io/api/client/work/listers/work/v1"
	workv1 "open-cluster-management.io/api/work/v1"
	workapiv1alpha1 "open-cluster-management.io/api/work/v1alpha1"
	clustersdkv1alpha1 "open-cluster-management.io/sdk-go/pkg/apis/cluster/v1alpha1"
	workapplier "open-cluster-management.io/sdk-go/pkg/apis/work/v1/applier"

	"open-cluster-management.io/ocm/pkg/common/helpers"
	"open-cluster-management.io/ocm/pkg/work/helper"
)

// deployReconciler is to manage ManifestWork based on the placement.
type deployReconciler struct {
	workApplier         *workapplier.WorkApplier
	manifestWorkLister  worklisterv1.ManifestWorkLister
	placeDecisionLister clusterlister.PlacementDecisionLister
	placementLister     clusterlister.PlacementLister
}

func (d *deployReconciler) reconcile(ctx context.Context, mwrSet *workapiv1alpha1.ManifestWorkReplicaSet,
) (*workapiv1alpha1.ManifestWorkReplicaSet, reconcileState, error) {
	// Manifestwork create/update/delete logic.
	var errs []error
	var plcsSummary []workapiv1alpha1.PlacementSummary
	minRequeue := maxRequeueTime
	count, total := 0, 0

	// Clean up ManifestWorks from placements no longer in the spec
	currentPlacementNames := sets.New[string]()
	for _, placementRef := range mwrSet.Spec.PlacementRefs {
		currentPlacementNames.Insert(placementRef.Name)
	}

	// Get all ManifestWorks belonging to this ManifestWorkReplicaSet
	allManifestWorks, err := listManifestWorksByManifestWorkReplicaSet(mwrSet, d.manifestWorkLister)
	if err != nil {
		return mwrSet, reconcileContinue, fmt.Errorf("failed to list manifestworks: %w", err)
	}

	// Delete ManifestWorks that belong to placements no longer in the spec
	for _, mw := range allManifestWorks {
		placementName, ok := mw.Labels[ManifestWorkReplicaSetPlacementNameLabelKey]
		if !ok || !currentPlacementNames.Has(placementName) {
			// This ManifestWork belongs to a placement that's no longer in the spec, delete it
			err := d.workApplier.Delete(ctx, mw.Namespace, mw.Name)
			if err != nil {
				errs = append(errs, fmt.Errorf("failed to delete manifestwork %s/%s for removed placement %s: %w", mw.Namespace, mw.Name, placementName, err))
			}
		}
	}

	// Getting the placements and the created ManifestWorks related to each placement
	for _, placementRef := range mwrSet.Spec.PlacementRefs {
		var existingRolloutClsStatus []clustersdkv1alpha1.ClusterRolloutStatus
		existingClusterNames := sets.New[string]()
		placement, err := d.placementLister.Placements(mwrSet.Namespace).Get(placementRef.Name)

		if errors.IsNotFound(err) {
			apimeta.SetStatusCondition(&mwrSet.Status.Conditions, GetPlacementDecisionVerified(workapiv1alpha1.ReasonPlacementDecisionNotFound, ""))
			return mwrSet, reconcileStop, nil
		}

		if err != nil {
			return mwrSet, reconcileContinue, fmt.Errorf("failed get placement %w", err)
		}

		manifestWorks, err := listManifestWorksByMWRSetPlacementRef(mwrSet, placementRef.Name, d.manifestWorkLister)
		if err != nil {
			return mwrSet, reconcileContinue, err
		}

		for _, mw := range manifestWorks {
			// Check if ManifestWorkTemplate changes, ManifestWork will need to be updated.
			newMW := &workv1.ManifestWork{}
			mw.ObjectMeta.DeepCopyInto(&newMW.ObjectMeta)
			mwrSet.Spec.ManifestWorkTemplate.DeepCopyInto(&newMW.Spec)

			// TODO: Create NeedToApply function by workApplier to check the manifestWork->spec hash value from the cache.
			if !workapplier.ManifestWorkEqual(newMW, mw) {
				continue
			}

			existingClusterNames.Insert(mw.Namespace)
			rolloutClusterStatus, err := d.clusterRolloutStatusFunc(mw.Namespace, *mw)

			if err != nil {
				errs = append(errs, err)
				continue
			}
			existingRolloutClsStatus = append(existingRolloutClsStatus, rolloutClusterStatus)
		}

		placeTracker := helper.GetPlacementTracker(d.placeDecisionLister, placement, existingClusterNames)
		rolloutHandler, err := clustersdkv1alpha1.NewRolloutHandler(placeTracker, d.clusterRolloutStatusFunc)
		if err != nil {
			apimeta.SetStatusCondition(&mwrSet.Status.Conditions, GetPlacementDecisionVerified(workapiv1alpha1.ReasonNotAsExpected, ""))

			return mwrSet, reconcileContinue, utilerrors.NewAggregate(errs)
		}

		err = placeTracker.Refresh()
		if err != nil {
			errs = append(errs, err)
			continue
		}

		_, rolloutResult, err := rolloutHandler.GetRolloutCluster(placementRef.RolloutStrategy, existingRolloutClsStatus)

		if err != nil {
			errs = append(errs, err)
			apimeta.SetStatusCondition(&mwrSet.Status.Conditions, GetPlacementDecisionVerified(workapiv1alpha1.ReasonNotAsExpected, ""))

			continue
		}

		if rolloutResult.RecheckAfter != nil && *rolloutResult.RecheckAfter < minRequeue {
			minRequeue = *rolloutResult.RecheckAfter
		}

		// Create ManifestWorks
		for _, rolloutStatue := range rolloutResult.ClustersToRollout {
			if rolloutStatue.Status == clustersdkv1alpha1.ToApply {
				mw, err := CreateManifestWork(mwrSet, rolloutStatue.ClusterName, placementRef.Name)
				if err != nil {
					errs = append(errs, err)
					continue
				}

				_, err = d.workApplier.Apply(ctx, mw)
				if err != nil {
					fmt.Printf("err is %v\n", err)
					errs = append(errs, err)
					continue
				}
				existingClusterNames.Insert(rolloutStatue.ClusterName)
			}
		}

		for _, cls := range rolloutResult.ClustersRemoved {
			// Delete manifestWork for removed clusters
			err = d.workApplier.Delete(ctx, cls.ClusterName, mwrSet.Name)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			existingClusterNames.Delete(cls.ClusterName)
		}

		total += int(placement.Status.NumberOfSelectedClusters)
		plcSummary := workapiv1alpha1.PlacementSummary{
			Name: placementRef.Name,
			AvailableDecisionGroups: getAvailableDecisionGroupProgressMessage(len(placement.Status.DecisionGroups),
				len(existingClusterNames), placement.Status.NumberOfSelectedClusters),
		}
		mwrSetSummary := workapiv1alpha1.ManifestWorkReplicaSetSummary{
			Total: len(existingClusterNames),
		}
		plcSummary.Summary = mwrSetSummary
		plcsSummary = append(plcsSummary, plcSummary)

		count += len(existingClusterNames)
	}
	// Set the placements summary
	mwrSet.Status.PlacementsSummary = plcsSummary

	// Set the Summary
	if mwrSet.Status.Summary == (workapiv1alpha1.ManifestWorkReplicaSetSummary{}) {
		mwrSet.Status.Summary = workapiv1alpha1.ManifestWorkReplicaSetSummary{}
	}

	mwrSet.Status.Summary.Total = count
	if count == 0 {
		mwrSet.Status.Summary.Applied = 0
		mwrSet.Status.Summary.Available = 0
		mwrSet.Status.Summary.Degraded = 0
		mwrSet.Status.Summary.Progressing = 0
		apimeta.SetStatusCondition(&mwrSet.Status.Conditions, GetPlacementDecisionVerified(workapiv1alpha1.ReasonPlacementDecisionEmpty, ""))
	} else {
		apimeta.SetStatusCondition(&mwrSet.Status.Conditions, GetPlacementDecisionVerified(workapiv1alpha1.ReasonAsExpected, ""))
	}

	if total == count {
		apimeta.SetStatusCondition(&mwrSet.Status.Conditions, GetPlacementRollOut(workapiv1alpha1.ReasonComplete, ""))
	} else {
		apimeta.SetStatusCondition(&mwrSet.Status.Conditions, GetPlacementRollOut(workapiv1alpha1.ReasonProgressing, ""))
	}

	if len(errs) > 0 {
		return mwrSet, reconcileContinue, utilerrors.NewAggregate(errs)
	}

	if minRequeue < maxRequeueTime {
		return mwrSet, reconcileContinue, helpers.NewRequeueError("Rollout requeue", minRequeue)
	}

	return mwrSet, reconcileContinue, nil
}

func (d *deployReconciler) clusterRolloutStatusFunc(clusterName string, manifestWork workv1.ManifestWork) (clustersdkv1alpha1.ClusterRolloutStatus, error) {
	// Initialize default status as ToApply, LastTransitionTime is not needed for ToApply status.
	clsRolloutStatus := clustersdkv1alpha1.ClusterRolloutStatus{
		ClusterName: clusterName,
		Status:      clustersdkv1alpha1.ToApply,
	}

	// Get all relevant conditions
	progressingCond := apimeta.FindStatusCondition(manifestWork.Status.Conditions, workv1.WorkProgressing)
	degradedCond := apimeta.FindStatusCondition(manifestWork.Status.Conditions, workv1.WorkDegraded)
	appliedCond := apimeta.FindStatusCondition(manifestWork.Status.Conditions, workv1.WorkApplied)

	// Return ToApply if:
	// - No Applied condition exists yet (work hasn't been applied by hub controller)
	// - Applied condition hasn't observed the latest spec
	// - No Progressing condition exists yet (work hasn't been reconciled by agent)
	// - Progressing condition hasn't observed the latest spec
	// - Degraded condition exists but hasn't observed the latest spec
	//   (Degraded is optional, but if it exists, we wait for it to catch up)
	//
	// IMPORTANT: Check Applied condition FIRST to ensure the work has been properly applied
	// before checking agent-side conditions. This prevents using stale timestamps from
	// previous generations when conditions update their ObservedGeneration without changing Status.
	if appliedCond == nil ||
		appliedCond.ObservedGeneration != manifestWork.Generation ||
		progressingCond == nil ||
		progressingCond.ObservedGeneration != manifestWork.Generation ||
		(degradedCond != nil && degradedCond.ObservedGeneration != manifestWork.Generation) {
		return clsRolloutStatus, nil
	}

	// Agent has observed the latest spec, determine status based on Progressing and Degraded conditions.
	// Degraded is an optional condition only used to determine Failed case.
	// - Progressing=True + Degraded=True → Failed (work is progressing but degraded)
	// - Progressing=True (not degraded) → Progressing
	// - Progressing=False → Succeeded
	// - Unknown state → Progressing (conservative fallback)
	//
	// LastTransitionTime is used by rollout strategies to calculate:
	// - Timeout for Progressing and Failed statuses
	// - Minimum success time (soak time) for Succeeded status
	switch {
	case progressingCond.Status == metav1.ConditionTrue && degradedCond != nil && degradedCond.Status == metav1.ConditionTrue:
		clsRolloutStatus.Status = clustersdkv1alpha1.Failed
		clsRolloutStatus.LastTransitionTime = &degradedCond.LastTransitionTime

	case progressingCond.Status == metav1.ConditionTrue:
		clsRolloutStatus.Status = clustersdkv1alpha1.Progressing
		clsRolloutStatus.LastTransitionTime = &progressingCond.LastTransitionTime

	case progressingCond.Status == metav1.ConditionFalse:
		clsRolloutStatus.Status = clustersdkv1alpha1.Succeeded
		clsRolloutStatus.LastTransitionTime = &progressingCond.LastTransitionTime

	default:
		// Unknown state, conservatively treat as still progressing
		clsRolloutStatus.Status = clustersdkv1alpha1.Progressing
		clsRolloutStatus.LastTransitionTime = &progressingCond.LastTransitionTime
	}

	return clsRolloutStatus, nil
}

// GetManifestworkApplied return only True status if there all clusters have manifests applied as expected
func GetManifestworkApplied(reason string, message string) metav1.Condition {
	if reason == workapiv1alpha1.ReasonAsExpected {
		return getCondition(workapiv1alpha1.ManifestWorkReplicaSetConditionManifestworkApplied, reason, message, metav1.ConditionTrue)
	}

	return getCondition(workapiv1alpha1.ManifestWorkReplicaSetConditionManifestworkApplied, reason, message, metav1.ConditionFalse)

}

// GetPlacementDecisionVerified return only True status if there are clusters selected
func GetPlacementDecisionVerified(reason string, message string) metav1.Condition {
	if reason == workapiv1alpha1.ReasonAsExpected {
		return getCondition(workapiv1alpha1.ManifestWorkReplicaSetConditionPlacementVerified, reason, message, metav1.ConditionTrue)
	}

	return getCondition(workapiv1alpha1.ManifestWorkReplicaSetConditionPlacementVerified, reason, message, metav1.ConditionFalse)
}

// GetPlacementRollout return only True status if there are clusters selected
func GetPlacementRollOut(reason string, message string) metav1.Condition {
	if reason == workapiv1alpha1.ReasonComplete {
		return getCondition(workapiv1alpha1.ManifestWorkReplicaSetConditionPlacementRolledOut, reason, message, metav1.ConditionTrue)
	}

	return getCondition(workapiv1alpha1.ManifestWorkReplicaSetConditionPlacementRolledOut, reason, message, metav1.ConditionFalse)
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

func CreateManifestWork(
	mwrSet *workapiv1alpha1.ManifestWorkReplicaSet,
	clusterNS string,
	placementRefName string,
) (*workv1.ManifestWork, error) {
	if clusterNS == "" {
		return nil, fmt.Errorf("invalid cluster namespace")
	}

	// Get ManifestWorkReplicaSet labels
	labels := mwrSet.Labels

	// TODO consider how to trace the manifestworks spec changes for cloudevents work client

	// Merge mwrSet.Labels with the required labels
	mergedLabels := make(map[string]string)
	for k, v := range labels {
		mergedLabels[k] = v
	}

	mergedLabels[ManifestWorkReplicaSetControllerNameLabelKey] = manifestWorkReplicaSetKey(mwrSet)
	mergedLabels[ManifestWorkReplicaSetPlacementNameLabelKey] = placementRefName

	return &workv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mwrSet.Name,
			Namespace: clusterNS,
			Labels:    mergedLabels,
		},
		Spec: mwrSet.Spec.ManifestWorkTemplate,
	}, nil
}

func getAvailableDecisionGroupProgressMessage(groupNum int, existingClsCount int, totalCls int32) string {
	return fmt.Sprintf("%d (%d / %d clusters applied)", groupNum, existingClsCount, totalCls)
}
