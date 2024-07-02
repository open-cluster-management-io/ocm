package agentdeploy

import (
	"context"

	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"

	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"
)

type defaultSyncer struct {
	buildWorks buildDeployWorkFunc

	applyWork func(ctx context.Context, appliedType string,
		work *workapiv1.ManifestWork, addon *addonapiv1alpha1.ManagedClusterAddOn) (*workapiv1.ManifestWork, error)

	getWorkByAddon func(addonName, addonNamespace string) ([]*workapiv1.ManifestWork, error)

	deleteWork func(ctx context.Context, workNamespace, workName string) error

	agentAddon agent.AgentAddon
}

func (s *defaultSyncer) sync(ctx context.Context,
	syncCtx factory.SyncContext,
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn) (*addonapiv1alpha1.ManagedClusterAddOn, error) {
	deployWorkNamespace := addon.Namespace

	var errs []error

	// Don't skip syncing if the addon is deleting and there is a predelete hook, since the deployment manifests may
	// need to be updated during the uninstall.
	if !addonHasFinalizer(addon, addonapiv1alpha1.AddonPreDeleteHookFinalizer) {
		if !addon.DeletionTimestamp.IsZero() {
			return addon, nil
		}

		// waiting for the addon to be deleted when cluster is deleting.
		// TODO: consider to delete addon in this scenario.
		if !cluster.DeletionTimestamp.IsZero() {
			return addon, nil
		}
	}

	currentWorks, err := s.getWorkByAddon(addon.Name, addon.Namespace)
	if err != nil {
		return addon, err
	}

	deployWorks, deleteWorks, err := s.buildWorks(deployWorkNamespace, cluster, currentWorks, addon)
	if err != nil {
		return addon, err
	}

	for _, deleteWork := range deleteWorks {
		err = s.deleteWork(ctx, deployWorkNamespace, deleteWork.Name)
		if err != nil {
			errs = append(errs, err)
		}
	}

	for _, deployWork := range deployWorks {
		_, err = s.applyWork(ctx, addonapiv1alpha1.ManagedClusterAddOnManifestApplied, deployWork, addon)
		if err != nil {
			errs = append(errs, err)
		}
	}

	return addon, utilerrors.NewAggregate(errs)
}
