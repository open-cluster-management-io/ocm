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
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type hostedSyncer struct {
	controller *addonDeployController
	agentAddon agent.AgentAddon
}

func (s *hostedSyncer) sync(ctx context.Context,
	syncCtx factory.SyncContext,
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn) (*addonapiv1alpha1.ManagedClusterAddOn, error) {

	// Hosted mode is not enabled, will not deploy any resource on the hosting cluster
	if !s.agentAddon.GetAgentAddonOptions().HostedModeEnabled {
		return addon, nil
	}

	installMode, hostingClusterName := constants.GetHostedModeInfo(addon.GetAnnotations())
	if installMode != constants.InstallModeHosted {
		// the installMode is changed from hosted to default, cleanup the hosting resources
		if err := s.cleanupDeployWork(ctx, addon, ""); err != nil {
			return addon, err
		}
		addonRemoveFinalizer(addon, constants.HostingManifestFinalizer)
		return addon, nil
	}

	// Get Hosting Cluster, check whether the hosting cluster is a managed cluster of the hub
	// TODO: check whether the hosting cluster of the addon is the same hosting cluster of the klusterlet
	hostingCluster, err := s.controller.managedClusterLister.Get(hostingClusterName)
	if errors.IsNotFound(err) {
		if err = s.cleanupDeployWork(ctx, addon, ""); err != nil {
			return addon, err
		}

		meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
			Type:    constants.HostingClusterValidity,
			Status:  metav1.ConditionFalse,
			Reason:  constants.HostingClusterValidityReasonInvalid,
			Message: fmt.Sprintf("hosting cluster %s is not a managed cluster of the hub", hostingClusterName),
		})

		addonRemoveFinalizer(addon, constants.HostingManifestFinalizer)
		return addon, nil
	}
	if err != nil {
		return addon, err
	}
	meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
		Type:    constants.HostingClusterValidity,
		Status:  metav1.ConditionTrue,
		Reason:  constants.HostingClusterValidityReasonValid,
		Message: fmt.Sprintf("hosting cluster %s is a managed cluster of the hub", hostingClusterName),
	})

	if !hostingCluster.DeletionTimestamp.IsZero() {
		if err = s.cleanupDeployWork(ctx, addon, hostingClusterName); err != nil {
			return addon, err
		}
		addonRemoveFinalizer(addon, constants.HostingManifestFinalizer)
		return addon, nil
	}

	if !addon.DeletionTimestamp.IsZero() {
		// clean up the deploy work until the hook work is completed
		if addonHasFinalizer(addon, constants.HostingPreDeleteHookFinalizer) {
			return addon, nil
		}

		if err = s.cleanupDeployWork(ctx, addon, hostingClusterName); err != nil {
			return addon, err
		}
		addonRemoveFinalizer(addon, constants.HostingManifestFinalizer)
		return addon, nil
	}

	if addonAddFinalizer(addon, constants.HostingManifestFinalizer) {
		return addon, nil
	}

	// waiting for the addon to be deleted when cluster is deleting.
	// TODO: consider to delete addon in this scenario.
	if !cluster.DeletionTimestamp.IsZero() {
		return addon, nil
	}

	deployWorkName := constants.DeployHostingWorkName(addon.Namespace, addon.Name)
	deployWork, _, err := s.controller.buildManifestWorks(ctx, s.agentAddon, installMode, hostingClusterName, cluster, addon)
	if err != nil {
		return addon, err
	}

	if deployWork == nil {
		if err = s.cleanupDeployWork(ctx, addon, hostingClusterName); err != nil {
			return addon, fmt.Errorf("failed to cleanup deploy work %s/%s when got no object", hostingClusterName, deployWorkName)
		}
		addonRemoveFinalizer(addon, constants.HostingManifestFinalizer)
		return addon, nil
	}

	_, err = s.controller.applyWork(ctx, constants.AddonHostingManifestApplied, deployWork, addon)
	return addon, err
}

// cleanupDeployWork will delete the hosting manifestWork and cache. if the hostingClusterName is empty, will try
// to find out the hosting cluster by manifestWork labels and do the cleanup.
func (s *hostedSyncer) cleanupDeployWork(ctx context.Context,
	addon *addonapiv1alpha1.ManagedClusterAddOn,
	hostingClusterName string) (err error) {
	if !addonHasFinalizer(addon, constants.HostingManifestFinalizer) {
		return nil
	}

	if len(hostingClusterName) == 0 {
		if hostingClusterName, err = s.controller.findHostingCluster(addon.Namespace, addon.Name); err != nil {
			return err
		}
	}

	deployWorkName := constants.DeployHostingWorkName(addon.Namespace, addon.Name)
	if err = deleteWork(ctx, s.controller.workClient, hostingClusterName, deployWorkName); err != nil {
		return client.IgnoreNotFound(err)
	}

	s.controller.cache.removeCache(deployWorkName, hostingClusterName)
	return nil
}
