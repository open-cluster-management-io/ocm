package agentdeploy

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/constants"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/basecontroller/factory"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

type hostedHookSyncer struct {
	controller *addonDeployController
	agentAddon agent.AgentAddon
}

func (s *hostedHookSyncer) sync(ctx context.Context,
	syncCtx factory.SyncContext,
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn) (*addonapiv1alpha1.ManagedClusterAddOn, error) {

	// Hosted mode is not enabled, will not deploy any resource on the hosting cluster
	if !s.agentAddon.GetAgentAddonOptions().HostedModeEnabled {
		return addon, nil
	}

	installMode, hostingClusterName := constants.GetHostedModeInfo(addon.GetAnnotations())
	if installMode != constants.InstallModeHosted {
		return addon, nil
	}

	// Get Hosting Cluster, check whether the hosting cluster is a managed cluster of the hub
	// TODO: check whether the hosting cluster of the addon is the same hosting cluster of the klusterlet
	hostingCluster, err := s.controller.managedClusterLister.Get(hostingClusterName)
	if errors.IsNotFound(err) {
		if err = s.cleanupHookWork(ctx, addon, ""); err != nil {
			return addon, err
		}

		addonRemoveFinalizer(addon, constants.HostingPreDeleteHookFinalizer)
		return addon, nil
	}
	if err != nil {
		return addon, err
	}

	if !hostingCluster.DeletionTimestamp.IsZero() {
		if err = s.cleanupHookWork(ctx, addon, hostingClusterName); err != nil {
			return addon, err
		}
		addonRemoveFinalizer(addon, constants.HostingPreDeleteHookFinalizer)
		return addon, nil
	}

	_, hookWork, err := s.controller.buildManifestWorks(ctx, s.agentAddon, installMode, hostingClusterName, cluster, addon)
	if err != nil {
		return addon, err
	}

	if hookWork == nil {
		addonRemoveFinalizer(addon, constants.HostingPreDeleteHookFinalizer)
		return addon, nil
	}

	// will deploy the pre-delete hook manifestWork when the addon is deleting
	if addon.DeletionTimestamp.IsZero() {
		addonAddFinalizer(addon, constants.HostingPreDeleteHookFinalizer)
		return addon, nil
	}

	// the hook work is completed if there is no HostingPreDeleteHookFinalizer when the addon is deleting.
	if !addonHasFinalizer(addon, constants.HostingPreDeleteHookFinalizer) {
		return addon, nil
	}

	hookWork, err = s.controller.applyWork(ctx, constants.AddonHostingManifestApplied, hookWork, addon)
	if err != nil {
		return addon, err
	}

	// TODO: will surface more message here
	if hookWorkIsCompleted(hookWork) {
		meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
			Type:    constants.AddonHookManifestCompleted,
			Status:  metav1.ConditionTrue,
			Reason:  "HookManifestIsCompleted",
			Message: fmt.Sprintf("hook manifestWork %v is completed.", hookWork.Name),
		})

		if err = s.cleanupHookWork(ctx, addon, hostingClusterName); err != nil {
			return addon, err
		}
		if addonRemoveFinalizer(addon, constants.HostingPreDeleteHookFinalizer) {
			return addon, err
		}
		return addon, nil
	}

	meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
		Type:    constants.AddonHookManifestCompleted,
		Status:  metav1.ConditionFalse,
		Reason:  "HookManifestIsNotCompleted",
		Message: fmt.Sprintf("hook manifestWork %v is not completed.", hookWork.Name),
	})

	return addon, nil

}

// cleanupHookWork will delete the hosting pre-delete hook manifestWork and remove the finalizer,
// if the hostingClusterName is empty, will try to find out the hosting cluster by manifestWork labels and do the cleanup
func (s *hostedHookSyncer) cleanupHookWork(ctx context.Context,
	addon *addonapiv1alpha1.ManagedClusterAddOn,
	hostingClusterName string) (err error) {
	if !addonHasFinalizer(addon, constants.HostingPreDeleteHookFinalizer) {
		return nil
	}

	if len(hostingClusterName) == 0 {
		if hostingClusterName, err = s.controller.findHostingCluster(addon.Namespace, addon.Name); err != nil {
			return err
		}
	}

	hookWorkName := constants.PreDeleteHookHostingWorkName(addon.Namespace, addon.Name)
	if err = deleteWork(ctx, s.controller.workClient, hostingClusterName, hookWorkName); err != nil {
		return err
	}

	s.controller.cache.removeCache(hookWorkName, hostingClusterName)
	return nil
}
