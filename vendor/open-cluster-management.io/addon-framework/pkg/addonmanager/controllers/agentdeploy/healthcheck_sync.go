package agentdeploy

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	addonapiv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"

	"open-cluster-management.io/addon-framework/pkg/addonmanager/constants"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/utils"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"
)

type healthCheckSyncer struct {
	getWorkByAddon       func(addonName, addonNamespace string) ([]*workapiv1.ManifestWork, error)
	getWorkByHostedAddon func(addonName, addonNamespace string) ([]*workapiv1.ManifestWork, error)
	agentAddon           agent.AgentAddon
}

func (s *healthCheckSyncer) sync(ctx context.Context,
	syncCtx factory.SyncContext,
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1beta1.ManagedClusterAddOn) (*addonapiv1beta1.ManagedClusterAddOn, error) {
	// reconcile health check mode
	var expectedHealthCheckMode addonapiv1beta1.HealthCheckMode

	if s.agentAddon.GetAgentAddonOptions().HealthProber == nil {
		return addon, nil
	}

	switch s.agentAddon.GetAgentAddonOptions().HealthProber.Type {
	case agent.HealthProberTypeWork, agent.HealthProberTypeNone,
		agent.HealthProberTypeDeploymentAvailability, agent.HealthProberTypeWorkloadAvailability:
		expectedHealthCheckMode = addonapiv1beta1.HealthCheckModeCustomized
	case agent.HealthProberTypeLease:
		expectedHealthCheckMode = addonapiv1beta1.HealthCheckModeLease
	default:
		expectedHealthCheckMode = addonapiv1beta1.HealthCheckModeLease
	}

	if expectedHealthCheckMode != addon.Status.HealthCheck.Mode {
		addon.Status.HealthCheck.Mode = expectedHealthCheckMode
	}

	err := s.probeAddonStatus(ctx, cluster, addon)
	return addon, err
}

func (s *healthCheckSyncer) probeAddonStatus(
	ctx context.Context,
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1beta1.ManagedClusterAddOn) error {
	switch s.agentAddon.GetAgentAddonOptions().HealthProber.Type {
	case agent.HealthProberTypeWork:
		return s.probeWorkAddonStatus(ctx, cluster, addon)
	case agent.HealthProberTypeDeploymentAvailability:
		return s.probeDeploymentAvailabilityAddonStatus(ctx, cluster, addon)
	case agent.HealthProberTypeWorkloadAvailability:
		return s.probeWorkloadAvailabilityAddonStatus(ctx, cluster, addon)
	default:
		return nil
	}
}
func (s *healthCheckSyncer) probeWorkAddonStatus(
	ctx context.Context,
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1beta1.ManagedClusterAddOn) error {
	if s.agentAddon.GetAgentAddonOptions().HealthProber.Type != agent.HealthProberTypeWork {
		return nil
	}

	if s.agentAddon.GetAgentAddonOptions().HealthProber.WorkProber == nil {
		meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
			Type:    addonapiv1beta1.ManagedClusterAddOnConditionAvailable,
			Status:  metav1.ConditionTrue,
			Reason:  addonapiv1beta1.AddonAvailableReasonWorkApply,
			Message: "Addon manifestWork is applied",
		})
		return nil
	}

	// update Available condition after addon manifestWorks are applied
	if meta.FindStatusCondition(addon.Status.Conditions, addonapiv1beta1.ManagedClusterAddOnManifestApplied) == nil {
		return nil
	}

	return s.probeAddonStatusByWorks(ctx, cluster, addon)
}

func (s *healthCheckSyncer) probeDeploymentAvailabilityAddonStatus(
	ctx context.Context,
	cluster *clusterv1.ManagedCluster, addon *addonapiv1beta1.ManagedClusterAddOn) error {
	return s.probeWorkloadAvailabilityAddonStatus(ctx, cluster, addon)
}

func (s *healthCheckSyncer) probeWorkloadAvailabilityAddonStatus(
	ctx context.Context,
	cluster *clusterv1.ManagedCluster, addon *addonapiv1beta1.ManagedClusterAddOn) error {

	proberType := s.agentAddon.GetAgentAddonOptions().HealthProber.Type
	if proberType != agent.HealthProberTypeDeploymentAvailability &&
		proberType != agent.HealthProberTypeWorkloadAvailability {
		return nil
	}

	// wait for the addon manifest applied
	if meta.FindStatusCondition(addon.Status.Conditions, addonapiv1beta1.ManagedClusterAddOnManifestApplied) == nil {
		return nil
	}

	return s.probeAddonStatusByWorks(ctx, cluster, addon)
}

func (s *healthCheckSyncer) probeAddonStatusByWorks(
	ctx context.Context,
	cluster *clusterv1.ManagedCluster, addon *addonapiv1beta1.ManagedClusterAddOn) error {

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

	var addonManifestWorks []*workapiv1.ManifestWork
	var err error
	installMode := constants.InstallModeDefault
	if s.agentAddon.GetAgentAddonOptions().HostedModeInfoFunc != nil {
		installMode, _ = s.agentAddon.GetAgentAddonOptions().HostedModeInfoFunc(addon, cluster)
	}
	if installMode == constants.InstallModeHosted {
		addonManifestWorks, err = s.getWorkByHostedAddon(addon.Name, addon.Namespace)
	} else {
		addonManifestWorks, err = s.getWorkByAddon(addon.Name, addon.Namespace)
	}

	if err != nil || len(addonManifestWorks) == 0 {
		meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
			Type:    addonapiv1beta1.ManagedClusterAddOnConditionAvailable,
			Status:  metav1.ConditionUnknown,
			Reason:  addonapiv1beta1.AddonAvailableReasonWorkNotFound,
			Message: "Addon manifestWork is not found",
		})
		return err
	}

	manifestConditions := []workapiv1.ManifestCondition{}
	for _, work := range addonManifestWorks {
		if !strings.HasPrefix(work.Name, constants.DeployWorkNamePrefix(addon.Name)) {
			continue
		}
		// Check the overall work available condition at first.
		workCond := meta.FindStatusCondition(work.Status.Conditions, workapiv1.WorkAvailable)
		switch {
		case workCond == nil:
			meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
				Type:    addonapiv1beta1.ManagedClusterAddOnConditionAvailable,
				Status:  metav1.ConditionUnknown,
				Reason:  addonapiv1beta1.AddonAvailableReasonWorkNotApply,
				Message: "Addon manifestWork is not applied yet",
			})
			return nil
		case workCond.Status == metav1.ConditionFalse:
			meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
				Type:    addonapiv1beta1.ManagedClusterAddOnConditionAvailable,
				Status:  metav1.ConditionFalse,
				Reason:  addonapiv1beta1.AddonAvailableReasonWorkNotApply,
				Message: workCond.Message,
			})
			return nil
		}

		manifestConditions = append(manifestConditions, work.Status.ResourceStatus.Manifests...)
	}

	probeFields, healthChecker, err := s.analyzeWorkProber(ctx, s.agentAddon, cluster, addon)
	if err != nil {
		// should not happen, return
		return err
	}

	var fieldResults []agent.FieldResult

	for _, field := range probeFields {
		results := findResultsByIdentifier(field.ResourceIdentifier, manifestConditions)
		// if no results are returned. it is possible that work agent has not returned the feedback value.
		// collect these fields and check if all probes are empty later
		if len(results) == 0 {
			continue
		}

		fieldResults = append(fieldResults, results...)
		// healthCheck will be ignored if healthChecker is set
		if healthChecker != nil {
			continue
		}
	}

	// If all probe fields have no results, mark condition to unknown
	if len(probeFields) > 0 && len(fieldResults) == 0 {
		meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
			Type:    addonapiv1beta1.ManagedClusterAddOnConditionAvailable,
			Status:  metav1.ConditionUnknown,
			Reason:  addonapiv1beta1.AddonAvailableReasonNoProbeResult,
			Message: "Probe results are not returned",
		})
		return nil
	}

	// If we have fieldResults but some probes are empty, still proceed with healthChecker
	// This allows partial probe results to be considered valid

	if healthChecker != nil {
		if err := healthChecker(fieldResults, cluster, addon); err != nil {
			meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
				Type:    addonapiv1beta1.ManagedClusterAddOnConditionAvailable,
				Status:  metav1.ConditionFalse,
				Reason:  addonapiv1beta1.AddonAvailableReasonProbeUnavailable,
				Message: fmt.Sprintf("Probe addon unavailable with err %v", err),
			})
			return nil
		}
	}

	meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
		Type:    addonapiv1beta1.ManagedClusterAddOnConditionAvailable,
		Status:  metav1.ConditionTrue,
		Reason:  addonapiv1beta1.AddonAvailableReasonProbeAvailable,
		Message: fmt.Sprintf("%s add-on is available.", addon.Name),
	})
	return nil
}

// TODO: use wildcard to refactor analyzeDeploymentWorkProber and analyzeWorkloadsWorkProber
func (s *healthCheckSyncer) analyzeWorkProber(
	ctx context.Context,
	agentAddon agent.AgentAddon,
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1beta1.ManagedClusterAddOn,
) ([]agent.ProbeField, agent.AddonHealthCheckerFunc, error) {

	switch agentAddon.GetAgentAddonOptions().HealthProber.Type {
	case agent.HealthProberTypeWork:
		workProber := agentAddon.GetAgentAddonOptions().HealthProber.WorkProber
		if workProber != nil {
			return workProber.ProbeFields, workProber.HealthChecker, nil
		}
		return nil, nil, fmt.Errorf("work prober is not configured")
	case agent.HealthProberTypeDeploymentAvailability:
		probeFields, heathChecker, err := s.analyzeDeploymentWorkProber(ctx, agentAddon, cluster, addon)
		return probeFields, heathChecker, err
	case agent.HealthProberTypeWorkloadAvailability:
		probeFields, heathChecker, err := s.analyzeWorkloadsWorkProber(ctx, agentAddon, cluster, addon)
		return probeFields, heathChecker, err
	default:
		return nil, nil, fmt.Errorf("unsupported health prober type %s", agentAddon.GetAgentAddonOptions().HealthProber.Type)
	}
}

func (s *healthCheckSyncer) analyzeDeploymentWorkProber(
	ctx context.Context,
	agentAddon agent.AgentAddon,
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1beta1.ManagedClusterAddOn,
) ([]agent.ProbeField, agent.AddonHealthCheckerFunc, error) {
	probeFields := []agent.ProbeField{}

	manifests, err := agentAddon.Manifests(ctx, cluster, addon)
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

	return probeFields, utils.DeploymentAvailabilityHealthChecker, nil
}

func (s *healthCheckSyncer) analyzeWorkloadsWorkProber(
	ctx context.Context,
	agentAddon agent.AgentAddon,
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1beta1.ManagedClusterAddOn,
) ([]agent.ProbeField, agent.AddonHealthCheckerFunc, error) {
	probeFields := []agent.ProbeField{}

	manifests, err := agentAddon.Manifests(ctx, cluster, addon)
	if err != nil {
		return nil, nil, err
	}

	workloads := utils.FilterWorkloads(manifests)
	for _, workload := range workloads {
		// Not probe the deployment with zero replicas
		if workload.GroupResource.Group == appsv1.GroupName &&
			workload.GroupResource.Resource == "deployments" &&
			workload.DeploymentSpec != nil &&
			workload.DeploymentSpec.Replicas == 0 {
			continue
		}

		manifestConfig := utils.WellKnowManifestConfig(workload.Group, workload.Resource,
			workload.Namespace, workload.Name)

		probeFields = append(probeFields, agent.ProbeField{
			ResourceIdentifier: manifestConfig.ResourceIdentifier,
			ProbeRules:         manifestConfig.FeedbackRules,
		})
	}

	return probeFields, utils.WorkloadAvailabilityHealthChecker, nil
}

func findResultsByIdentifier(identifier workapiv1.ResourceIdentifier,
	manifestConditions []workapiv1.ManifestCondition) []agent.FieldResult {
	var results []agent.FieldResult
	for _, status := range manifestConditions {
		if resourceMatch(status.ResourceMeta, identifier) && len(status.StatusFeedbacks.Values) != 0 {
			results = append(results, agent.FieldResult{
				ResourceIdentifier: workapiv1.ResourceIdentifier{
					Group:     status.ResourceMeta.Group,
					Resource:  status.ResourceMeta.Resource,
					Name:      status.ResourceMeta.Name,
					Namespace: status.ResourceMeta.Namespace,
				},
				FeedbackResult: status.StatusFeedbacks,
			})
		}
	}

	return results
}

// compare two string, target may include *
func wildcardMatch(resource, target string) bool {
	if resource == target || target == "*" {
		return true
	}

	pattern := "^" + regexp.QuoteMeta(target) + "$"
	pattern = strings.ReplaceAll(pattern, "\\*", ".*")

	re, err := regexp.Compile(pattern)
	if err != nil {
		return false
	}

	return re.MatchString(resource)
}

func resourceMatch(resourceMeta workapiv1.ManifestResourceMeta, resource workapiv1.ResourceIdentifier) bool {
	return resourceMeta.Group == resource.Group &&
		resourceMeta.Resource == resource.Resource &&
		wildcardMatch(resourceMeta.Namespace, resource.Namespace) &&
		wildcardMatch(resourceMeta.Name, resource.Name)
}
