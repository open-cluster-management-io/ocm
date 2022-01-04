package addonhealthcheck

import (
	"context"
	"fmt"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"open-cluster-management.io/addon-framework/pkg/agent"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformerv1alpha1 "open-cluster-management.io/api/client/addon/informers/externalversions/addon/v1alpha1"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"
)

// addonHealthCheckController reconciles instances of ManagedClusterAddon on the hub.
type addonHealthCheckController struct {
	addonClient               addonv1alpha1client.Interface
	managedClusterAddonLister addonlisterv1alpha1.ManagedClusterAddOnLister
	agentAddons               map[string]agent.AgentAddon
	eventRecorder             events.Recorder
}

func NewAddonHealthCheckController(
	addonClient addonv1alpha1client.Interface,
	addonInformers addoninformerv1alpha1.ManagedClusterAddOnInformer,
	agentAddons map[string]agent.AgentAddon,
	recorder events.Recorder,
) factory.Controller {
	c := &addonHealthCheckController{
		addonClient:               addonClient,
		managedClusterAddonLister: addonInformers.Lister(),
		agentAddons:               agentAddons,
		eventRecorder:             recorder.WithComponentSuffix(fmt.Sprintf("addon-healthcheck-controller")),
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
		WithSync(c.sync).
		ToController(fmt.Sprintf("addon-healthcheck-controller"), recorder)
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

	for addonName, addon := range c.agentAddons {
		if addon.GetAgentAddonOptions().HealthProber == nil {
			continue
		}
		return c.syncAddonHealthChecker(ctx, managedClusterAddon, addonName, clusterName)
	}

	return nil
}

func (c *addonHealthCheckController) syncAddonHealthChecker(ctx context.Context, addon *addonapiv1alpha1.ManagedClusterAddOn, addonName, clusterName string) error {
	// for in-place edit
	addon = addon.DeepCopy()
	// reconcile health check mode
	var expectedHealthCheckMode addonapiv1alpha1.HealthCheckMode
	agentAddon := c.agentAddons[addonName]
	if agentAddon != nil && agentAddon.GetAgentAddonOptions().HealthProber != nil {
		switch c.agentAddons[addonName].GetAgentAddonOptions().HealthProber.Type {
		// TODO(yue9944882): implement work api health checker
		//case agent.HealthProberTypeWork:
		//fallthrough
		case agent.HealthProberTypeNone:
			expectedHealthCheckMode = addonapiv1alpha1.HealthCheckModeCustomized
		case agent.HealthProberTypeLease:
			fallthrough
		default:
			expectedHealthCheckMode = addonapiv1alpha1.HealthCheckModeLease
		}
	}
	if expectedHealthCheckMode != addon.Status.HealthCheck.Mode {
		addon.Status.HealthCheck.Mode = expectedHealthCheckMode
		c.eventRecorder.Eventf("HealthCheckModeUpdated", "Updated health check mode to %s", expectedHealthCheckMode)
		_, err := c.addonClient.AddonV1alpha1().ManagedClusterAddOns(clusterName).
			UpdateStatus(ctx, addon, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
	}

	return nil
}
