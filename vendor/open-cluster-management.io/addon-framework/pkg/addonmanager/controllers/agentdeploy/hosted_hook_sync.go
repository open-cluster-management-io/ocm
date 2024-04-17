package agentdeploy

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"

	"open-cluster-management.io/addon-framework/pkg/addonmanager/constants"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/basecontroller/factory"
)

type hostedHookSyncer struct {
	buildWorks buildDeployHookFunc

	applyWork func(ctx context.Context, appliedType string,
		work *workapiv1.ManifestWork, addon *addonapiv1alpha1.ManagedClusterAddOn) (*workapiv1.ManifestWork, error)

	deleteWork func(ctx context.Context, workNamespace, workName string) error

	getWorkByAddon func(addonName, addonNamespace string) ([]*workapiv1.ManifestWork, error)

	getCluster func(clusterName string) (*clusterv1.ManagedCluster, error)

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

	if s.agentAddon.GetAgentAddonOptions().HostedModeInfoFunc == nil {
		return addon, nil
	}
	installMode, hostingClusterName := s.agentAddon.GetAgentAddonOptions().HostedModeInfoFunc(addon, cluster)
	if installMode != constants.InstallModeHosted {
		return addon, nil
	}

	// Get Hosting Cluster, check whether the hosting cluster is a managed cluster of the hub
	// TODO: check whether the hosting cluster of the addon is the same hosting cluster of the klusterlet
	hostingCluster, err := s.getCluster(hostingClusterName)
	if errors.IsNotFound(err) {
		if err = s.cleanupHookWork(ctx, addon); err != nil {
			return addon, err
		}

		addonRemoveFinalizer(addon, addonapiv1alpha1.AddonHostingPreDeleteHookFinalizer)
		return addon, nil
	}
	if err != nil {
		return addon, err
	}

	if !hostingCluster.DeletionTimestamp.IsZero() {
		if err = s.cleanupHookWork(ctx, addon); err != nil {
			return addon, err
		}
		addonRemoveFinalizer(addon, addonapiv1alpha1.AddonHostingPreDeleteHookFinalizer)
		return addon, nil
	}
	hookWork, err := s.buildWorks(hostingClusterName, cluster, addon)
	if err != nil {
		return addon, err
	}

	if hookWork == nil {
		addonRemoveFinalizer(addon, addonapiv1alpha1.AddonHostingPreDeleteHookFinalizer)
		return addon, nil
	}

	if addon.DeletionTimestamp.IsZero() {
		addonAddFinalizer(addon, addonapiv1alpha1.AddonHostingPreDeleteHookFinalizer)
		return addon, nil
	}

	if !addonHasFinalizer(addon, addonapiv1alpha1.AddonHostingPreDeleteHookFinalizer) {
		return addon, nil
	}

	// apply the pre-delete hook manifestWork when the addon is deleting and HookManifestCompleted condition is not true.
	// there are 2 cases:
	// 1. the HookManifestCompleted condition is false.
	// 2. there is no this condition.
	if !meta.IsStatusConditionTrue(addon.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnHookManifestCompleted) {
		hookWork, err = s.applyWork(ctx, addonapiv1alpha1.ManagedClusterAddOnHostingManifestApplied, hookWork, addon)
		if err != nil {
			return addon, err
		}
	} else {
		// cleanup is safe here since there is no case which HookManifestCompleted condition is changed from true to false.
		if err = s.cleanupHookWork(ctx, addon); err != nil {
			return addon, err
		}
		if addonRemoveFinalizer(addon, addonapiv1alpha1.AddonHostingPreDeleteHookFinalizer) {
			return addon, err
		}
		return addon, nil
	}

	// TODO: will surface more message here
	if hookWorkIsCompleted(hookWork) {
		meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
			Type:    addonapiv1alpha1.ManagedClusterAddOnHookManifestCompleted,
			Status:  metav1.ConditionTrue,
			Reason:  "HookManifestIsCompleted",
			Message: fmt.Sprintf("hook manifestWork %v is completed.", hookWork.Name),
		})
	} else {
		meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
			Type:    addonapiv1alpha1.ManagedClusterAddOnHookManifestCompleted,
			Status:  metav1.ConditionFalse,
			Reason:  "HookManifestIsNotCompleted",
			Message: fmt.Sprintf("hook manifestWork %v is not completed.", hookWork.Name),
		})
	}

	return addon, nil
}

// cleanupHookWork will delete the hosting pre-delete hook manifestWork and remove the finalizer,
// if the hostingClusterName is empty, will try to find out the hosting cluster by manifestWork labels and do the cleanup
func (s *hostedHookSyncer) cleanupHookWork(ctx context.Context,
	addon *addonapiv1alpha1.ManagedClusterAddOn) (err error) {
	if !addonHasFinalizer(addon, addonapiv1alpha1.AddonHostingPreDeleteHookFinalizer) {
		return nil
	}

	currentWorks, err := s.getWorkByAddon(addon.Name, addon.Namespace)
	if err != nil {
		return err
	}

	var errs []error
	for _, work := range currentWorks {
		err = s.deleteWork(ctx, work.Namespace, work.Name)
		if err != nil {
			errs = append(errs, err)
		}
	}

	return utilerrors.NewAggregate(errs)
}
