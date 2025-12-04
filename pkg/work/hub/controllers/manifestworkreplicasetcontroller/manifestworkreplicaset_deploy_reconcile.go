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
	count, total, succeededCount := 0, 0, 0

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
		placementName, ok := mw.Labels[workapiv1alpha1.ManifestWorkReplicaSetPlacementNameLabelKey]
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
		succeededClusterNames := sets.New[string]()
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

			// Only count clusters that are done progressing (Succeeded status)
			if rolloutClusterStatus.Status == clustersdkv1alpha1.Succeeded {
				succeededClusterNames.Insert(mw.Namespace)
			}
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
		succeededCount += len(succeededClusterNames)
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

	if total == succeededCount {
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

	// Check if the work should be in ToApply status
	if shouldReturnToApply(manifestWork.Generation, appliedCond, progressingCond, degradedCond) {
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

// shouldReturnToApply determines if the ManifestWork should be in ToApply status
// based on the state of its conditions.
//
// Returns true if:
// - The Applied condition is not ready (missing, hasn't observed latest spec, or not True)
// - The Progressing condition is not ready (missing or hasn't observed latest spec)
// - The Degraded condition exists but hasn't observed the latest spec
//
// IMPORTANT: Applied condition is checked FIRST to ensure the work has been properly applied
// by the spoke agent before checking other agent-side conditions (Progressing/Degraded).
// This prevents using stale timestamps from previous generations when conditions update
// their ObservedGeneration without changing Status.
func shouldReturnToApply(generation int64, appliedCond, progressingCond, degradedCond *metav1.Condition) bool {
	// Check Applied condition first - work must be applied by spoke agent
	if !isConditionReady(appliedCond, generation, true) {
		return true
	}

	// Check Progressing condition - work must be reconciled by spoke agent
	if !isConditionReady(progressingCond, generation, false) {
		return true
	}

	// Check Degraded condition if it exists - it must have observed the latest spec
	// Degraded is optional, but if it exists, we wait for it to catch up to avoid
	// using stale status information
	if degradedCond != nil && degradedCond.ObservedGeneration != generation {
		return true
	}

	return false
}

// isConditionReady checks if a condition is ready for rollout status evaluation.
// A condition is ready if:
// - It exists (not nil)
// - It has observed the latest generation
// - If requireTrue is set, it must also have Status=True
func isConditionReady(cond *metav1.Condition, generation int64, requireTrue bool) bool {
	if cond == nil {
		return false
	}

	if cond.ObservedGeneration != generation {
		return false
	}

	if requireTrue && cond.Status != metav1.ConditionTrue {
		return false
	}

	return true
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

// GetPlacementRollout return only True status if all the clusters selected by the placement have succeeded
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

	mergedLabels[workapiv1alpha1.ManifestWorkReplicaSetControllerNameLabelKey] = manifestWorkReplicaSetKey(mwrSet)
	mergedLabels[workapiv1alpha1.ManifestWorkReplicaSetPlacementNameLabelKey] = placementRefName

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
