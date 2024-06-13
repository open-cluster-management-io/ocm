package addonconfiguration

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	"open-cluster-management.io/sdk-go/pkg/patcher"
)

type cmaProgressingReconciler struct {
	patcher patcher.Patcher[
		*addonv1alpha1.ClusterManagementAddOn, addonv1alpha1.ClusterManagementAddOnSpec, addonv1alpha1.ClusterManagementAddOnStatus]
}

func (d *cmaProgressingReconciler) reconcile(
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

		setAddOnInstallProgressionsAndLastApplied(&cmaCopy.Status.InstallProgressions[i],
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
	progressing, done, failed, timeout, total int) {

	condition := metav1.Condition{
		Type: addonv1alpha1.ManagedClusterAddOnConditionProgressing,
	}
	if (total == 0 && done == 0) || (done != total) {
		condition.Status = metav1.ConditionTrue
		condition.Reason = addonv1alpha1.ProgressingReasonProgressing
		condition.Message = fmt.Sprintf("%d/%d progressing..., %d failed %d timeout.", progressing+done, total, failed, timeout)
	} else {
		for i, configRef := range installProgression.ConfigReferences {
			installProgression.ConfigReferences[i].LastAppliedConfig = configRef.DesiredConfig.DeepCopy()
			installProgression.ConfigReferences[i].LastKnownGoodConfig = configRef.DesiredConfig.DeepCopy()
		}
		condition.Status = metav1.ConditionFalse
		condition.Reason = addonv1alpha1.ProgressingReasonCompleted
		condition.Message = fmt.Sprintf("%d/%d completed with no errors, %d failed %d timeout.", done, total, failed, timeout)
	}
	meta.SetStatusCondition(&installProgression.Conditions, condition)
}
