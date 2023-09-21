package addonconfiguration

import (
	"context"
	"encoding/json"
	"fmt"

	jsonpatch "github.com/evanphx/json-patch"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/klog/v2"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
)

type clusterManagementAddonProgressingReconciler struct {
	addonClient addonv1alpha1client.Interface
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
			placementNode.countAddonTimeOut(),
			len(placementNode.clusters),
		)
	}

	err := d.patchMgmtAddonStatus(ctx, cmaCopy, cma)
	if err != nil {
		errs = append(errs, err)
	}
	return cmaCopy, reconcileContinue, utilerrors.NewAggregate(errs)
}

func (d *clusterManagementAddonProgressingReconciler) patchMgmtAddonStatus(ctx context.Context, new, old *addonv1alpha1.ClusterManagementAddOn) error {
	if equality.Semantic.DeepEqual(new.Status, old.Status) {
		return nil
	}

	oldData, err := json.Marshal(&addonv1alpha1.ClusterManagementAddOn{
		Status: addonv1alpha1.ClusterManagementAddOnStatus{
			InstallProgressions: old.Status.InstallProgressions,
		},
	})
	if err != nil {
		return err
	}

	newData, err := json.Marshal(&addonv1alpha1.ClusterManagementAddOn{
		ObjectMeta: metav1.ObjectMeta{
			UID:             new.UID,
			ResourceVersion: new.ResourceVersion,
		},
		Status: addonv1alpha1.ClusterManagementAddOnStatus{
			InstallProgressions: new.Status.InstallProgressions,
		},
	})
	if err != nil {
		return err
	}

	patchBytes, err := jsonpatch.CreateMergePatch(oldData, newData)
	if err != nil {
		return fmt.Errorf("failed to create patch for addon %s: %w", new.Name, err)
	}

	klog.V(2).Infof("Patching clustermanagementaddon %s status with %s", new.Name, string(patchBytes))
	_, err = d.addonClient.AddonV1alpha1().ClusterManagementAddOns().Patch(
		ctx, new.Name, types.MergePatchType, patchBytes, metav1.PatchOptions{}, "status")
	return err
}

func setAddOnInstallProgressionsAndLastApplied(
	installProgression *addonv1alpha1.InstallProgression,
	isUpgrade bool,
	progressing, done, timeout, total int) {
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
			condition.Message = fmt.Sprintf("%d/%d upgrading..., %d timeout.", progressing+done, total, timeout)
		} else {
			condition.Reason = addonv1alpha1.ProgressingReasonInstalling
			condition.Message = fmt.Sprintf("%d/%d installing..., %d timeout.", progressing+done, total, timeout)
		}
	} else {
		for i, configRef := range installProgression.ConfigReferences {
			installProgression.ConfigReferences[i].LastAppliedConfig = configRef.DesiredConfig.DeepCopy()
			installProgression.ConfigReferences[i].LastKnownGoodConfig = configRef.DesiredConfig.DeepCopy()
		}
		condition.Status = metav1.ConditionFalse
		if isUpgrade {
			condition.Reason = addonv1alpha1.ProgressingReasonUpgradeSucceed
			condition.Message = fmt.Sprintf("%d/%d upgrade completed with no errors, %d timeout.", done, total, timeout)
		} else {
			condition.Reason = addonv1alpha1.ProgressingReasonInstallSucceed
			condition.Message = fmt.Sprintf("%d/%d install completed with no errors, %d timeout.", done, total, timeout)
		}
	}
	meta.SetStatusCondition(&installProgression.Conditions, condition)
}
