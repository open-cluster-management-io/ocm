package agentdeploy

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"

	"open-cluster-management.io/addon-framework/pkg/addonmanager/constants"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/basecontroller/factory"
)

type healthCheckSyncer struct {
	getWorkByAddon func(addonName, addonNamespace string) ([]*workapiv1.ManifestWork, error)
	agentAddon     agent.AgentAddon
}

func (s *healthCheckSyncer) sync(ctx context.Context,
	syncCtx factory.SyncContext,
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn) (*addonapiv1alpha1.ManagedClusterAddOn, error) {
	// reconcile health check mode
	var expectedHealthCheckMode addonapiv1alpha1.HealthCheckMode

	if s.agentAddon.GetAgentAddonOptions().HealthProber == nil {
		return addon, nil
	}

	switch s.agentAddon.GetAgentAddonOptions().HealthProber.Type {
	case agent.HealthProberTypeWork, agent.HealthProberTypeNone:
		expectedHealthCheckMode = addonapiv1alpha1.HealthCheckModeCustomized
	case agent.HealthProberTypeLease:
		expectedHealthCheckMode = addonapiv1alpha1.HealthCheckModeLease
	default:
		expectedHealthCheckMode = addonapiv1alpha1.HealthCheckModeLease
	}

	if expectedHealthCheckMode != addon.Status.HealthCheck.Mode {
		addon.Status.HealthCheck.Mode = expectedHealthCheckMode
	}

	err := s.probeAddonStatus(addon)
	return addon, err
}

func (s *healthCheckSyncer) probeAddonStatus(addon *addonapiv1alpha1.ManagedClusterAddOn) error {
	if s.agentAddon.GetAgentAddonOptions().HealthProber.Type != agent.HealthProberTypeWork {
		return nil
	}

	if s.agentAddon.GetAgentAddonOptions().HealthProber.WorkProber == nil {
		meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
			Type:    addonapiv1alpha1.ManagedClusterAddOnConditionAvailable,
			Status:  metav1.ConditionTrue,
			Reason:  addonapiv1alpha1.AddonAvailableReasonWorkApply,
			Message: "Addon manifestWork is applied",
		})
		return nil
	}

	// update Available condition after addon manifestWorks are applied
	if meta.FindStatusCondition(addon.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnManifestApplied) == nil {
		return nil
	}

	addonWorks, err := s.getWorkByAddon(addon.Name, addon.Namespace)
	if err != nil || len(addonWorks) == 0 {
		meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
			Type:    addonapiv1alpha1.ManagedClusterAddOnConditionAvailable,
			Status:  metav1.ConditionUnknown,
			Reason:  addonapiv1alpha1.AddonAvailableReasonWorkNotFound,
			Message: "Addon manifestWork is not found",
		})
		return err
	}

	manifestConditions := []workapiv1.ManifestCondition{}
	for _, work := range addonWorks {
		if !strings.HasPrefix(work.Name, constants.DeployWorkNamePrefix(addon.Name)) {
			continue
		}
		// Check the overall work available condition at first.
		workCond := meta.FindStatusCondition(work.Status.Conditions, workapiv1.WorkAvailable)
		switch {
		case workCond == nil:
			meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
				Type:    addonapiv1alpha1.ManagedClusterAddOnConditionAvailable,
				Status:  metav1.ConditionUnknown,
				Reason:  addonapiv1alpha1.AddonAvailableReasonWorkNotApply,
				Message: "Addon manifestWork is not applied yet",
			})
			return nil
		case workCond.Status == metav1.ConditionFalse:
			meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
				Type:    addonapiv1alpha1.ManagedClusterAddOnConditionAvailable,
				Status:  metav1.ConditionFalse,
				Reason:  addonapiv1alpha1.AddonAvailableReasonWorkNotApply,
				Message: workCond.Message,
			})
			return nil
		}

		manifestConditions = append(manifestConditions, work.Status.ResourceStatus.Manifests...)
	}

	probeFields := s.agentAddon.GetAgentAddonOptions().HealthProber.WorkProber.ProbeFields

	for _, field := range probeFields {
		result := findResultByIdentifier(field.ResourceIdentifier, manifestConditions)
		// if no results are returned. it is possible that work agent has not returned the feedback value.
		// mark condition to unknown
		if result == nil {
			meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
				Type:    addonapiv1alpha1.ManagedClusterAddOnConditionAvailable,
				Status:  metav1.ConditionUnknown,
				Reason:  addonapiv1alpha1.AddonAvailableReasonNoProbeResult,
				Message: "Probe results are not returned",
			})
			return nil
		}

		err := s.agentAddon.GetAgentAddonOptions().HealthProber.WorkProber.HealthCheck(field.ResourceIdentifier, *result)
		if err != nil {
			meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
				Type:    addonapiv1alpha1.ManagedClusterAddOnConditionAvailable,
				Status:  metav1.ConditionFalse,
				Reason:  addonapiv1alpha1.AddonAvailableReasonProbeUnavailable,
				Message: fmt.Sprintf("Probe addon unavailable with err %v", err),
			})
			return nil
		}
	}

	meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
		Type:    addonapiv1alpha1.ManagedClusterAddOnConditionAvailable,
		Status:  metav1.ConditionTrue,
		Reason:  addonapiv1alpha1.AddonAvailableReasonProbeAvailable,
		Message: "Addon is available",
	})
	return nil
}

func findResultByIdentifier(identifier workapiv1.ResourceIdentifier, manifestConditions []workapiv1.ManifestCondition) *workapiv1.StatusFeedbackResult {
	for _, status := range manifestConditions {
		if identifier.Group != status.ResourceMeta.Group {
			continue
		}
		if identifier.Resource != status.ResourceMeta.Resource {
			continue
		}
		if identifier.Name != status.ResourceMeta.Name {
			continue
		}
		if identifier.Namespace != status.ResourceMeta.Namespace {
			continue
		}

		if len(status.StatusFeedbacks.Values) == 0 {
			return nil
		}

		return &status.StatusFeedbacks
	}

	return nil
}
