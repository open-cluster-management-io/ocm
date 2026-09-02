package agentdeploy

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"

	addonapiv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"

	"open-cluster-management.io/addon-framework/pkg/addonmanager/constants"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"
)

const hostingClusterClaimName = constants.HostingClusterClaimName

// selfReportedHostingCluster reads the reserved hosting-cluster ClusterClaim off the target
// managed cluster's status, and whether a non-empty value was found at all. Absence is never an
// error: it just means the target's klusterlet either doesn't have self-report turned on, or
// hasn't reconciled yet.
func selfReportedHostingCluster(cluster *clusterv1.ManagedCluster) (string, bool) {
	for _, claim := range cluster.Status.ClusterClaims {
		if claim.Name == constants.HostingClusterClaimName {
			return claim.Value, len(claim.Value) > 0
		}
	}
	return "", false
}

type hostedSyncer struct {
	buildWorks buildDeployWorkFunc

	applyWork func(ctx context.Context, appliedType string,
		work *workapiv1.ManifestWork, addon *addonapiv1beta1.ManagedClusterAddOn) (*workapiv1.ManifestWork, error)

	deleteWork func(ctx context.Context, workNamespace, workName string) error

	getWorkByAddon func(addonName, addonNamespace string) ([]*workapiv1.ManifestWork, error)

	getCluster func(clusterName string) (*clusterv1.ManagedCluster, error)

	agentAddon agent.AgentAddon
}

func (s *hostedSyncer) sync(ctx context.Context,
	syncCtx factory.SyncContext,
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1beta1.ManagedClusterAddOn) (*addonapiv1beta1.ManagedClusterAddOn, error) {
	// Hosted mode is not enabled, will not deploy any resource on the hosting cluster
	if !s.agentAddon.GetAgentAddonOptions().HostedModeEnabled {
		return addon, nil
	}

	if s.agentAddon.GetAgentAddonOptions().HostedModeInfoFunc == nil {
		return addon, nil
	}
	installMode, hostingClusterName := s.agentAddon.GetAgentAddonOptions().HostedModeInfoFunc(addon, cluster)
	if installMode != constants.InstallModeHosted {
		// the installMode is changed from hosted to default, cleanup the hosting resources
		if err := s.cleanupDeployWork(ctx, addon); err != nil {
			return addon, err
		}
		addonRemoveFinalizer(addon, addonapiv1beta1.AddonHostingManifestFinalizer)
		meta.RemoveStatusCondition(&addon.Status.Conditions,
			addonapiv1beta1.ManagedClusterAddOnHostingClusterValidity)
		return addon, nil
	}

	// Get Hosting Cluster, check whether the hosting cluster is a managed cluster of the hub
	hostingCluster, err := s.getCluster(hostingClusterName)
	if errors.IsNotFound(err) {
		if err = s.cleanupDeployWork(ctx, addon); err != nil {
			return addon, err
		}

		meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
			Type:    addonapiv1beta1.ManagedClusterAddOnHostingClusterValidity,
			Status:  metav1.ConditionFalse,
			Reason:  addonapiv1beta1.HostingClusterValidityReasonInvalid,
			Message: fmt.Sprintf("hosting cluster %s is not a managed cluster of the hub", hostingClusterName),
		})

		addonRemoveFinalizer(addon, addonapiv1beta1.AddonHostingManifestFinalizer)
		return addon, nil
	}
	if err != nil {
		return addon, err
	}

	// Fetched here, ahead of the mismatch check below, because "deployed" has to mean an actual
	// ManifestWork exists - not merely that the finalizer has been added. addonAddFinalizer and
	// the first buildWorks/applyWork call are two separate reconciles (see below): the finalizer
	// add returns immediately, so there's at least one reconcile where the finalizer is present
	// but nothing has been built yet. Treating the finalizer alone as "already deployed" would let
	// a mismatch discovered in exactly that window through instead of holding the addon back.
	currentWorks, err := s.getWorkByAddon(addon.Name, addon.Namespace)
	if err != nil {
		return addon, err
	}
	declaredHostingWorks := deployWorksInHostingCluster(currentWorks, hostingClusterName, addon)

	// A hosting-cluster annotation change must not silently relocate a running addon. Works from
	// another namespace are retained until explicit deletion, but they are never passed to the
	// builder as if they belonged to the newly declared hosting cluster.
	if len(currentWorks) > 0 && len(declaredHostingWorks) == 0 &&
		addon.DeletionTimestamp.IsZero() && hostingCluster.DeletionTimestamp.IsZero() {
		meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
			Type:    addonapiv1beta1.ManagedClusterAddOnHostingClusterValidity,
			Status:  metav1.ConditionFalse,
			Reason:  addonapiv1beta1.HostingClusterValidityReasonMismatch,
			Message: fmt.Sprintf("declared hosting cluster %s does not match the existing addon deployment", hostingClusterName),
		})
		return addon, nil
	}

	// Validate the declared/resolved hosting cluster against whatever the target's klusterlet
	// self-reports (if anything). A mismatch is always just a condition, never a teardown: an
	// addon with no hosting manifests actually deployed yet is held back, while one that's already
	// deployed keeps running regardless of how long the mismatch persists - see KEP-188 Non-Goals.
	if claimed, ok := selfReportedHostingCluster(cluster); ok && claimed != hostingClusterName {
		meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
			Type:   addonapiv1beta1.ManagedClusterAddOnHostingClusterValidity,
			Status: metav1.ConditionFalse,
			Reason: addonapiv1beta1.HostingClusterValidityReasonMismatch,
			Message: fmt.Sprintf("declared hosting cluster %s does not match %s self-reported by managed cluster %s",
				hostingClusterName, claimed, cluster.Name),
		})
		// Hold back only while nothing is deleting: an explicit delete of the addon or the hosting
		// cluster must never be blocked by a mismatch, no matter how far deployment got - falling
		// through here lets the deletion handling below run and strip the finalizer.
		if len(declaredHostingWorks) == 0 && addon.DeletionTimestamp.IsZero() && hostingCluster.DeletionTimestamp.IsZero() {
			return addon, nil
		}
		// already deployed, or being deleted with nothing left to hold back: condition is loud, but
		// nothing here tears it down - fall through and keep reconciling normally.
	} else {
		meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
			Type:    addonapiv1beta1.ManagedClusterAddOnHostingClusterValidity,
			Status:  metav1.ConditionTrue,
			Reason:  addonapiv1beta1.HostingClusterValidityReasonValid,
			Message: fmt.Sprintf("hosting cluster %s is a managed cluster of the hub", hostingClusterName),
		})
	}

	// Don't skip syncing if the addon is deleting and there is a predelete hook, since the deployment manifests may
	// need to be updated during the uninstall.
	if !addonHasFinalizer(addon, addonapiv1beta1.AddonHostingPreDeleteHookFinalizer) {
		if !hostingCluster.DeletionTimestamp.IsZero() {
			if err = s.cleanupDeployWork(ctx, addon); err != nil {
				return addon, err
			}
			addonRemoveFinalizer(addon, addonapiv1beta1.AddonHostingManifestFinalizer)
			return addon, nil
		}

		if !addon.DeletionTimestamp.IsZero() {
			if err = s.cleanupDeployWork(ctx, addon); err != nil {
				return addon, err
			}
			addonRemoveFinalizer(addon, addonapiv1beta1.AddonHostingManifestFinalizer)
			return addon, nil
		}

		// waiting for the addon to be deleted when cluster is deleting.
		// TODO: consider to delete addon in this scenario.
		if !cluster.DeletionTimestamp.IsZero() {
			return addon, nil
		}
	}

	if addonAddFinalizer(addon, addonapiv1beta1.AddonHostingManifestFinalizer) {
		return addon, nil
	}

	deployWorks, deleteWorks, err := s.buildWorks(ctx, hostingClusterName, cluster, declaredHostingWorks, addon)
	if err != nil {
		return addon, err
	}

	var errs []error
	for _, deleteWork := range deleteWorks {
		err = s.deleteWork(ctx, deleteWork.Namespace, deleteWork.Name)
		if err != nil {
			errs = append(errs, err)
		}
	}

	for _, deployWork := range deployWorks {
		_, err = s.applyWork(ctx, addonapiv1beta1.ManagedClusterAddOnHostingManifestApplied, deployWork, addon)
		if err != nil {
			errs = append(errs, err)
		}
	}

	return addon, utilerrors.NewAggregate(errs)
}

// cleanupDeployWork will delete the hosting manifestWork and cache. if the hostingClusterName is empty, will try
// to find out the hosting cluster by manifestWork labels and do the cleanup.
func (s *hostedSyncer) cleanupDeployWork(ctx context.Context,
	addon *addonapiv1beta1.ManagedClusterAddOn) (err error) {
	if !addonHasFinalizer(addon, addonapiv1beta1.AddonHostingManifestFinalizer) {
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

func deployWorksInHostingCluster(
	works []*workapiv1.ManifestWork,
	hostingClusterName string,
	addon *addonapiv1beta1.ManagedClusterAddOn,
) []*workapiv1.ManifestWork {
	prefix := constants.DeployHostingWorkNamePrefix(addon.Namespace, addon.Name)
	var matched []*workapiv1.ManifestWork
	for _, work := range works {
		if work.Namespace == hostingClusterName && strings.HasPrefix(work.Name, prefix) {
			matched = append(matched, work)
		}
	}
	return matched
}
