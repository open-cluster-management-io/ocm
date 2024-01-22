package addonconfiguration

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	"open-cluster-management.io/sdk-go/pkg/patcher"
)

type clusterManagementAddonProgressingReconciler struct {
	patcher patcher.Patcher[
		*addonv1alpha1.ClusterManagementAddOn, addonv1alpha1.ClusterManagementAddOnSpec, addonv1alpha1.ClusterManagementAddOnStatus]
}

func (d *clusterManagementAddonProgressingReconciler) reconcile(
	ctx context.Context, cma *addonv1alpha1.ClusterManagementAddOn, graph *configurationGraph) (*addonv1alpha1.ClusterManagementAddOn, reconcileState, error) {
	var errs []error
	cmaCopy := cma.DeepCopy()
	placementNodes := graph.getPlacementNodes()

	// go through addons and update condition per install progression
	for i, installProgression := range cmaCopy.Status.InstallProgressions {
		placementNode, exist := placementNodes[installProgression.PlacementRef]
		if !exist {
			continue
		}

		isUpgrade := false

		for _, configReference := range installProgression.ConfigReferences {
			if configReference.LastAppliedConfig != nil {
				isUpgrade = true
				break
			}
		}

		setAddOnInstallProgressionsAndLastApplied(&cmaCopy.Status.InstallProgressions[i],
			isUpgrade,
			placementNode.countAddonUpgrading(),
			placementNode.countAddonUpgradeSucceed(),
			placementNode.countAddonUpgradeFailed(),
			placementNode.countAddonTimeOut(),
			len(placementNode.clusters),
		)
	}

	_, err := d.patcher.PatchStatus(ctx, cmaCopy, cmaCopy.Status, cma.Status)
	if err != nil {
		errs = append(errs, err)
	}
	return cmaCopy, reconcileContinue, utilerrors.NewAggregate(errs)
}

func setAddOnInstallProgressionsAndLastApplied(
	installProgression *addonv1alpha1.InstallProgression,
	isUpgrade bool,
	progressing, done, failed, timeout, total int) {
	// always update progressing condition when there is no config
	// skip update progressing condition when last applied config already the same as desired
	skip := len(installProgression.ConfigReferences) > 0
	for _, configReference := range installProgression.ConfigReferences {
		if !equality.Semantic.DeepEqual(configReference.LastAppliedConfig, configReference.DesiredConfig) &&
			!equality.Semantic.DeepEqual(configReference.LastKnownGoodConfig, configReference.DesiredConfig) {
			skip = false
		}
	}
	if skip {
		return
	}
	condition := metav1.Condition{
		Type: addonv1alpha1.ManagedClusterAddOnConditionProgressing,
	}
	if (total == 0 && done == 0) || (done != total) {
		condition.Status = metav1.ConditionTrue
		if isUpgrade {
			condition.Reason = addonv1alpha1.ProgressingReasonUpgrading
			condition.Message = fmt.Sprintf("%d/%d upgrading..., %d failed %d timeout.", progressing+done, total, failed, timeout)
		} else {
			condition.Reason = addonv1alpha1.ProgressingReasonInstalling
			condition.Message = fmt.Sprintf("%d/%d installing..., %d failed %d timeout.", progressing+done, total, failed, timeout)
		}
	} else {
		for i, configRef := range installProgression.ConfigReferences {
			installProgression.ConfigReferences[i].LastAppliedConfig = configRef.DesiredConfig.DeepCopy()
			installProgression.ConfigReferences[i].LastKnownGoodConfig = configRef.DesiredConfig.DeepCopy()
		}
		condition.Status = metav1.ConditionFalse
		if isUpgrade {
			condition.Reason = addonv1alpha1.ProgressingReasonUpgradeSucceed
			condition.Message = fmt.Sprintf("%d/%d upgrade completed with no errors, %d failed %d timeout.", done, total, failed, timeout)
		} else {
			condition.Reason = addonv1alpha1.ProgressingReasonInstallSucceed
			condition.Message = fmt.Sprintf("%d/%d install completed with no errors, %d failed %d timeout.", done, total, failed, timeout)
		}
	}
	meta.SetStatusCondition(&installProgression.Conditions, condition)
}
