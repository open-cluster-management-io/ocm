package agentdeploy

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	addonapiv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"

	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"
)

type defaultHookSyncer struct {
	buildWorks buildDeployHookFunc
	applyWork  func(ctx context.Context, appliedType string,
		work *workapiv1.ManifestWork, addon *addonapiv1beta1.ManagedClusterAddOn) (*workapiv1.ManifestWork, error)
	deleteWork func(ctx context.Context, workNamespace, workName string) error
	agentAddon agent.AgentAddon
}

func (s *defaultHookSyncer) sync(ctx context.Context,
	syncCtx factory.SyncContext,
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1beta1.ManagedClusterAddOn) (*addonapiv1beta1.ManagedClusterAddOn, error) {
	deployWorkNamespace := addon.Namespace

	hookWork, err := s.buildWorks(ctx, deployWorkNamespace, cluster, addon)
	if err != nil {
		return addon, err
	}

	if hookWork == nil {
		addonRemoveFinalizer(addon, addonapiv1beta1.AddonPreDeleteHookFinalizer)
		return addon, nil
	}

	if addonAddFinalizer(addon, addonapiv1beta1.AddonPreDeleteHookFinalizer) {
		return addon, nil
	}

	if addon.DeletionTimestamp.IsZero() {
		return addon, nil
	}

	// will deploy the pre-delete hook manifestWork when the addon is deleting
	hookWork, err = s.applyWork(ctx, addonapiv1beta1.ManagedClusterAddOnManifestApplied, hookWork, addon)
	if err != nil {
		return addon, err
	}

	// TODO: will surface more message here
	if hookWorkIsCompleted(hookWork) {
		meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
			Type:    addonapiv1beta1.ManagedClusterAddOnHookManifestCompleted,
			Status:  metav1.ConditionTrue,
			Reason:  "HookManifestIsCompleted",
			Message: fmt.Sprintf("hook manifestWork %v is completed.", hookWork.Name),
		})

		addonRemoveFinalizer(addon, addonapiv1beta1.AddonPreDeleteHookFinalizer)
		return addon, nil
	}

	// The hook has not completed. If the hook resource has reached a terminal
	// failed state (e.g. the pod was evicted due to node pressure, its node
	// became unreachable, or the job exhausted its backoffLimit), the work-agent
	// will not recreate it on its own: the resource still exists with an
	// unchanged spec, so a plain re-apply is a no-op. Delete the hook
	// manifestWork so it is rebuilt and re-applied on a subsequent reconcile,
	// which recreates a fresh hook pod/job. This retries indefinitely until the
	// hook eventually succeeds, so the pre-delete hook finalizer is never left
	// dangling because of a transient eviction.
	if hookWorkIsFailed(hookWork) && hookWork.DeletionTimestamp.IsZero() {
		if err = s.deleteWork(ctx, hookWork.Namespace, hookWork.Name); err != nil {
			return addon, err
		}
		meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
			Type:    addonapiv1beta1.ManagedClusterAddOnHookManifestCompleted,
			Status:  metav1.ConditionFalse,
			Reason:  "HookManifestFailedRetrying",
			Message: fmt.Sprintf("hook manifestWork %v failed and is being recreated to retry.", hookWork.Name),
		})
		return addon, nil
	}

	meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
		Type:    addonapiv1beta1.ManagedClusterAddOnHookManifestCompleted,
		Status:  metav1.ConditionFalse,
		Reason:  "HookManifestIsNotCompleted",
		Message: fmt.Sprintf("hook manifestWork %v is not completed.", hookWork.Name),
	})

	return addon, nil
}
