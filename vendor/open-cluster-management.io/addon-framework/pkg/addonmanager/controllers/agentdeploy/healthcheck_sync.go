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
	"open-cluster-management.io/addon-framework/pkg/utils"
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
	case agent.HealthProberTypeWork, agent.HealthProberTypeNone, agent.HealthProberTypeDeploymentAvailability:
		expectedHealthCheckMode = addonapiv1alpha1.HealthCheckModeCustomized
	case agent.HealthProberTypeLease:
		expectedHealthCheckMode = addonapiv1alpha1.HealthCheckModeLease
	default:
		expectedHealthCheckMode = addonapiv1alpha1.HealthCheckModeLease
	}

	if expectedHealthCheckMode != addon.Status.HealthCheck.Mode {
		addon.Status.HealthCheck.Mode = expectedHealthCheckMode
	}

	err := s.probeAddonStatus(cluster, addon)
	return addon, err
}

func (s *healthCheckSyncer) probeAddonStatus(
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn) error {
	switch s.agentAddon.GetAgentAddonOptions().HealthProber.Type {
	case agent.HealthProberTypeWork:
		return s.probeWorkAddonStatus(cluster, addon)
	case agent.HealthProberTypeDeploymentAvailability:
		return s.probeDeploymentAvailabilityAddonStatus(cluster, addon)
	default:
		return nil
	}
}
func (s *healthCheckSyncer) probeWorkAddonStatus(
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn) error {
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

	return s.probeAddonStatusByWorks(cluster, addon)
}

func (s *healthCheckSyncer) probeDeploymentAvailabilityAddonStatus(
	cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) error {

	if s.agentAddon.GetAgentAddonOptions().HealthProber.Type != agent.HealthProberTypeDeploymentAvailability {
		return nil
	}

	// wait for the addon manifest applied
	if meta.FindStatusCondition(addon.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnManifestApplied) == nil {
		return nil
	}

	return s.probeAddonStatusByWorks(cluster, addon)
}

func (s *healthCheckSyncer) probeAddonStatusByWorks(
	cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) error {

	if cluster != nil {
		clusterAvailableCondition := meta.FindStatusCondition(cluster.Status.Conditions,
			clusterv1.ManagedClusterConditionAvailable)
		if clusterAvailableCondition != nil && clusterAvailableCondition.Status == metav1.ConditionUnknown {
			// if the managed cluster availability is unknown, skip the health check
			// and the registration agent will set all addon status to unknown
			// nolint see: https://github.com/open-cluster-management-io/ocm/blob/9dc8f104cf51439b6bb1f738894e75aabdf5f8dc/pkg/registration/hub/addon/healthcheck_controller.go#L68-L78
			return nil
		}
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

	probeFields, healthChecker, err := s.analyzeWorkProber(s.agentAddon, cluster, addon)
	if err != nil {
		// should not happen, return
		return err
	}

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

		err := healthChecker(field.ResourceIdentifier, *result)
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
		Message: fmt.Sprintf("%s add-on is available.", addon.Name),
	})
	return nil
}

func (s *healthCheckSyncer) analyzeWorkProber(
	agentAddon agent.AgentAddon,
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn,
) ([]agent.ProbeField, agent.AddonHealthCheckFunc, error) {

	switch agentAddon.GetAgentAddonOptions().HealthProber.Type {
	case agent.HealthProberTypeWork:
		workProber := agentAddon.GetAgentAddonOptions().HealthProber.WorkProber
		if workProber != nil {
			return workProber.ProbeFields, workProber.HealthCheck, nil
		}
		return nil, nil, fmt.Errorf("work prober is not configured")
	case agent.HealthProberTypeDeploymentAvailability:
		return s.analyzeDeploymentWorkProber(agentAddon, cluster, addon)
	default:
		return nil, nil, fmt.Errorf("unsupported health prober type %s", agentAddon.GetAgentAddonOptions().HealthProber.Type)
	}
}

func (s *healthCheckSyncer) analyzeDeploymentWorkProber(
	agentAddon agent.AgentAddon,
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn,
) ([]agent.ProbeField, agent.AddonHealthCheckFunc, error) {
	probeFields := []agent.ProbeField{}

	manifests, err := agentAddon.Manifests(cluster, addon)
	if err != nil {
		return nil, nil, err
	}

	deployments := utils.FilterDeployments(manifests)
	for _, deployment := range deployments {
		manifestConfig := utils.DeploymentWellKnowManifestConfig(deployment.Namespace, deployment.Name)
		// only probe the deployment with non-zero replicas
		if deployment.Spec.Replicas != nil && *deployment.Spec.Replicas == 0 {
			continue
		}
		probeFields = append(probeFields, agent.ProbeField{
			ResourceIdentifier: manifestConfig.ResourceIdentifier,
			ProbeRules:         manifestConfig.FeedbackRules,
		})
	}

	return probeFields, utils.DeploymentAvailabilityHealthCheck, nil
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
