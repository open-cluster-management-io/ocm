package agentdeploy

import (
	"context"
	"fmt"

	"open-cluster-management.io/addon-framework/pkg/addonmanager/constants"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/basecontroller/factory"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

type defaultSyncer struct {
	controller *addonDeployController
	agentAddon agent.AgentAddon
}

func (s *defaultSyncer) sync(ctx context.Context,
	syncCtx factory.SyncContext,
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn) (*addonapiv1alpha1.ManagedClusterAddOn, error) {
	installMode := constants.InstallModeDefault
	deployWorkNamespace := addon.Namespace
	deployWorkName := constants.DeployWorkName(addon.Name)

	if !addon.DeletionTimestamp.IsZero() {
		s.controller.cache.removeCache(deployWorkName, deployWorkNamespace)
		return addon, nil
	}

	// waiting for the addon to be deleted when cluster is deleting.
	// TODO: consider to delete addon in this scenario.
	if !cluster.DeletionTimestamp.IsZero() {
		return addon, nil
	}

	deployWork, _, err := s.controller.buildManifestWorks(ctx, s.agentAddon, installMode, deployWorkNamespace, cluster, addon)
	if err != nil {
		return addon, err
	}

	// deployWork is nil only in the case that there is no object from addon manifests. need to delete the deployWork.
	if deployWork == nil {
		err = deleteWork(ctx, s.controller.workClient, deployWorkNamespace, deployWorkName)
		if err != nil {
			return addon, fmt.Errorf("failed to delete work %s/%s when got no object", deployWorkNamespace, deployWorkName)
		}
		s.controller.cache.removeCache(deployWorkNamespace, deployWorkName)
		return addon, nil
	}

	_, err = s.controller.applyWork(ctx, constants.AddonManifestApplied, deployWork, addon)
	return addon, err
}
