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
	clsRolloutStatus := clustersdkv1alpha1.ClusterRolloutStatus{
		ClusterName:        clusterName,
		LastTransitionTime: &manifestWork.CreationTimestamp,
		// Default status is ToApply
		Status: clustersdkv1alpha1.ToApply,
	}

	appliedCondition := apimeta.FindStatusCondition(manifestWork.Status.Conditions, workv1.WorkApplied)

	// Applied condition not exist return status as ToApply.
	if appliedCondition == nil { //nolint:gocritic
		return clsRolloutStatus, nil
	} else if appliedCondition.Status == metav1.ConditionTrue ||
		apimeta.IsStatusConditionTrue(manifestWork.Status.Conditions, workv1.WorkProgressing) {
		// Applied OR Progressing conditions status true return status as Progressing
		// ManifestWork Progressing status is not defined however the check is made for future work availability.
		clsRolloutStatus.Status = clustersdkv1alpha1.Progressing
	} else if appliedCondition.Status == metav1.ConditionFalse {
		// Applied Condition status false return status as failed
		clsRolloutStatus.Status = clustersdkv1alpha1.Failed
		clsRolloutStatus.LastTransitionTime = &appliedCondition.LastTransitionTime
		return clsRolloutStatus, nil
	}

	// Available condition return status as Succeeded
	if apimeta.IsStatusConditionTrue(manifestWork.Status.Conditions, workv1.WorkAvailable) {
		clsRolloutStatus.Status = clustersdkv1alpha1.Succeeded
		return clsRolloutStatus, nil
	}

	// Degraded condition return status as Failed
	// ManifestWork Degraded status is not defined however the check is made for future work availability.
	if apimeta.IsStatusConditionTrue(manifestWork.Status.Conditions, workv1.WorkDegraded) {
		clsRolloutStatus.Status = clustersdkv1alpha1.Failed
		clsRolloutStatus.LastTransitionTime = &appliedCondition.LastTransitionTime
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

	// TODO consider how to trace the manifestworks spec changes for cloudevents work client

	return &workv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mwrSet.Name,
			Namespace: clusterNS,
			Labels: map[string]string{
				ManifestWorkReplicaSetControllerNameLabelKey: manifestWorkReplicaSetKey(mwrSet),
				ManifestWorkReplicaSetPlacementNameLabelKey:  placementRefName,
			},
		},
		Spec: mwrSet.Spec.ManifestWorkTemplate,
	}, nil
}

func getAvailableDecisionGroupProgressMessage(groupNum int, existingClsCount int, totalCls int32) string {
	return fmt.Sprintf("%d (%d / %d clusters applied)", groupNum, existingClsCount, totalCls)
}
