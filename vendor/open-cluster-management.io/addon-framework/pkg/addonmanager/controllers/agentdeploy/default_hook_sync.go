package agentdeploy

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"

	"open-cluster-management.io/addon-framework/pkg/addonmanager/constants"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/basecontroller/factory"
)

type defaultHookSyncer struct {
	buildWorks func(installMode, workNamespace string, cluster *clusterv1.ManagedCluster,
		addon *addonapiv1alpha1.ManagedClusterAddOn) (*workapiv1.ManifestWork, error)
	applyWork func(ctx context.Context, appliedType string,
		work *workapiv1.ManifestWork, addon *addonapiv1alpha1.ManagedClusterAddOn) (*workapiv1.ManifestWork, error)
	agentAddon agent.AgentAddon
}

func (s *defaultHookSyncer) sync(ctx context.Context,
	syncCtx factory.SyncContext,
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn) (*addonapiv1alpha1.ManagedClusterAddOn, error) {
	deployWorkNamespace := addon.Namespace

	hookWork, err := s.buildWorks(constants.InstallModeDefault, deployWorkNamespace, cluster, addon)
	if err != nil {
		return addon, err
	}

	if hookWork == nil {
		addonRemoveFinalizer(addon, addonapiv1alpha1.AddonPreDeleteHookFinalizer)
		return addon, nil
	}

	if addonAddFinalizer(addon, addonapiv1alpha1.AddonPreDeleteHookFinalizer) {
		return addon, nil
	}

	if addon.DeletionTimestamp.IsZero() {
		return addon, nil
	}

	// will deploy the pre-delete hook manifestWork when the addon is deleting
	hookWork, err = s.applyWork(ctx, addonapiv1alpha1.ManagedClusterAddOnManifestApplied, hookWork, addon)
	if err != nil {
		return addon, err
	}

	// TODO: will surface more message here
	if hookWorkIsCompleted(hookWork) {
		meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
			Type:    addonapiv1alpha1.ManagedClusterAddOnHookManifestCompleted,
			Status:  metav1.ConditionTrue,
			Reason:  "HookManifestIsCompleted",
			Message: fmt.Sprintf("hook manifestWork %v is completed.", hookWork.Name),
		})

		addonRemoveFinalizer(addon, addonapiv1alpha1.AddonPreDeleteHookFinalizer)
		return addon, nil
	}

	meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
		Type:    addonapiv1alpha1.ManagedClusterAddOnHookManifestCompleted,
		Status:  metav1.ConditionFalse,
		Reason:  "HookManifestIsNotCompleted",
		Message: fmt.Sprintf("hook manifestWork %v is not completed.", hookWork.Name),
	})

	return addon, nil
}
