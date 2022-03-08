package addonhealthcheck

import (
	"context"
	"fmt"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/constants"
	"open-cluster-management.io/addon-framework/pkg/agent"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformerv1alpha1 "open-cluster-management.io/api/client/addon/informers/externalversions/addon/v1alpha1"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions/work/v1"
	worklister "open-cluster-management.io/api/client/work/listers/work/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"
)

// addonHealthCheckController reconciles instances of ManagedClusterAddon on the hub.
type addonHealthCheckController struct {
	addonClient               addonv1alpha1client.Interface
	managedClusterAddonLister addonlisterv1alpha1.ManagedClusterAddOnLister
	workLister                worklister.ManifestWorkLister
	agentAddons               map[string]agent.AgentAddon
	eventRecorder             events.Recorder
}

func NewAddonHealthCheckController(
	addonClient addonv1alpha1client.Interface,
	addonInformers addoninformerv1alpha1.ManagedClusterAddOnInformer,
	workInformers workinformers.ManifestWorkInformer,
	agentAddons map[string]agent.AgentAddon,
	recorder events.Recorder,
) factory.Controller {
	c := &addonHealthCheckController{
		addonClient:               addonClient,
		managedClusterAddonLister: addonInformers.Lister(),
		workLister:                workInformers.Lister(),
		agentAddons:               agentAddons,
		eventRecorder:             recorder.WithComponentSuffix("addon-healthcheck-controller"),
	}

	return factory.New().WithFilteredEventsInformersQueueKeyFunc(
		func(obj runtime.Object) string {
			key, _ := cache.MetaNamespaceKeyFunc(obj)
			return key
		},
		func(obj interface{}) bool {
			accessor, _ := meta.Accessor(obj)
			if _, ok := c.agentAddons[accessor.GetName()]; !ok {
				return false
			}
			return true
		},
		addonInformers.Informer()).
		WithFilteredEventsInformersQueueKeyFunc(
			func(obj runtime.Object) string {
				accessor, _ := meta.Accessor(obj)
				return fmt.Sprintf("%s/%s", accessor.GetNamespace(), accessor.GetLabels()[constants.AddonLabel])
			},
			func(obj interface{}) bool {
				accessor, _ := meta.Accessor(obj)
				if accessor.GetLabels() == nil {
					return false
				}

				addonName, ok := accessor.GetLabels()[constants.AddonLabel]
				if !ok {
					return false
				}

				if _, ok := c.agentAddons[addonName]; !ok {
					return false
				}
				if accessor.GetName() != constants.DeployWorkName(addonName) {
					return false
				}
				return true
			},
			workInformers.Informer(),
		).
		WithSync(c.sync).
		ToController("addon-healthcheck-controller", recorder)
}

func (c *addonHealthCheckController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	clusterName, addonName, err := cache.SplitMetaNamespaceKey(syncCtx.QueueKey())
	if err != nil {
		// ignore addon whose key is not in format: namespace/name
		return nil
	}

	klog.V(4).Infof("Reconciling addon health checker on cluster %q", clusterName)
	managedClusterAddon, err := c.managedClusterAddonLister.ManagedClusterAddOns(clusterName).Get(addonName)
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	agentAddon := c.agentAddons[addonName]
	if agentAddon == nil {
		return nil
	}

	return c.syncAddonHealthChecker(ctx, managedClusterAddon, agentAddon)
}

func (c *addonHealthCheckController) syncAddonHealthChecker(ctx context.Context, addon *addonapiv1alpha1.ManagedClusterAddOn, agentAddon agent.AgentAddon) error {
	// for in-place edit
	addon = addon.DeepCopy()
	// reconcile health check mode
	var expectedHealthCheckMode addonapiv1alpha1.HealthCheckMode

	if agentAddon.GetAgentAddonOptions().HealthProber == nil {
		return nil
	}

	switch agentAddon.GetAgentAddonOptions().HealthProber.Type {
	case agent.HealthProberTypeWork:
		fallthrough
	case agent.HealthProberTypeNone:
		expectedHealthCheckMode = addonapiv1alpha1.HealthCheckModeCustomized
	case agent.HealthProberTypeLease:
		fallthrough
	default:
		expectedHealthCheckMode = addonapiv1alpha1.HealthCheckModeLease
	}

	if expectedHealthCheckMode != addon.Status.HealthCheck.Mode {
		addon.Status.HealthCheck.Mode = expectedHealthCheckMode
		c.eventRecorder.Eventf("HealthCheckModeUpdated", "Updated health check mode to %s", expectedHealthCheckMode)
		_, err := c.addonClient.AddonV1alpha1().ManagedClusterAddOns(addon.Namespace).
			UpdateStatus(ctx, addon, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
	}

	return c.probeAddonStatus(ctx, addon, agentAddon)
}

func (c *addonHealthCheckController) probeAddonStatus(ctx context.Context, addon *addonapiv1alpha1.ManagedClusterAddOn, agentAddon agent.AgentAddon) error {
	if agentAddon.GetAgentAddonOptions().HealthProber == nil {
		return nil
	}

	if agentAddon.GetAgentAddonOptions().HealthProber.Type != agent.HealthProberTypeWork {
		return nil
	}

	addonWork, err := c.workLister.ManifestWorks(addon.Namespace).Get(constants.DeployWorkName(addon.Name))
	if err != nil {
		cond := metav1.Condition{
			Type:    "Available",
			Status:  metav1.ConditionUnknown,
			Reason:  "WorkNotFound",
			Message: "Work for addon is not found",
		}
		return c.updateConditions(ctx, addon, cond)
	}

	// Check the overall work available condition at first.
	workCond := meta.FindStatusCondition(addonWork.Status.Conditions, workapiv1.WorkAvailable)
	switch {
	case workCond == nil:
		cond := metav1.Condition{
			Type:    "Available",
			Status:  metav1.ConditionUnknown,
			Reason:  "WorkNotApplied",
			Message: "Work is not applied yet",
		}
		return c.updateConditions(ctx, addon, cond)
	case workCond.Status == metav1.ConditionFalse:
		cond := metav1.Condition{
			Type:    "Available",
			Status:  metav1.ConditionFalse,
			Reason:  "WorkApplyFailed",
			Message: workCond.Message,
		}
		return c.updateConditions(ctx, addon, cond)
	}

	if agentAddon.GetAgentAddonOptions().HealthProber.WorkProber == nil {
		cond := metav1.Condition{
			Type:    "Available",
			Status:  metav1.ConditionTrue,
			Reason:  "WorkApplied",
			Message: "Addon work is applied",
		}
		return c.updateConditions(ctx, addon, cond)
	}

	probeFields := agentAddon.GetAgentAddonOptions().HealthProber.WorkProber.ProbeFields

	for _, field := range probeFields {
		result := findResultByIdentifier(field.ResourceIdentifier, addonWork)
		// if no results are returned. it is possible that work agent has not returned the feedback value.
		// mark condition to unknown
		if result == nil {
			cond := metav1.Condition{
				Type:    "Available",
				Status:  metav1.ConditionUnknown,
				Reason:  "NoProbeResult",
				Message: "Probe results are not returned",
			}
			return c.updateConditions(ctx, addon, cond)
		}

		err := agentAddon.GetAgentAddonOptions().HealthProber.WorkProber.HealthCheck(field.ResourceIdentifier, *result)
		if err != nil {
			cond := metav1.Condition{
				Type:    "Available",
				Status:  metav1.ConditionFalse,
				Reason:  "ProbeUnavailable",
				Message: fmt.Sprintf("Probe addon unavailable with err %v", err),
			}
			return c.updateConditions(ctx, addon, cond)
		}
	}

	cond := metav1.Condition{
		Type:    "Available",
		Status:  metav1.ConditionTrue,
		Reason:  "ProbeAvailable",
		Message: "Addon is available",
	}
	return c.updateConditions(ctx, addon, cond)
}

func (c *addonHealthCheckController) updateConditions(ctx context.Context, addon *addonapiv1alpha1.ManagedClusterAddOn, conds ...metav1.Condition) error {
	addonCopy := addon.DeepCopy()

	for _, cond := range conds {
		meta.SetStatusCondition(&addonCopy.Status.Conditions, cond)
	}

	if equality.Semantic.DeepEqual(addon.Status.Conditions, addonCopy.Status.Conditions) {
		return nil
	}

	_, err := c.addonClient.AddonV1alpha1().ManagedClusterAddOns(addonCopy.Namespace).UpdateStatus(ctx, addonCopy, metav1.UpdateOptions{})
	return err
}

func findResultByIdentifier(identifier workapiv1.ResourceIdentifier, work *workapiv1.ManifestWork) *workapiv1.StatusFeedbackResult {
	for _, status := range work.Status.ResourceStatus.Manifests {
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
