package agentdeploy

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/constants"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/basecontroller/factory"
	"open-cluster-management.io/addon-framework/pkg/utils"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformerv1alpha1 "open-cluster-management.io/api/client/addon/informers/externalversions/addon/v1alpha1"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterlister "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	workv1client "open-cluster-management.io/api/client/work/clientset/versioned"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions/work/v1"
	worklister "open-cluster-management.io/api/client/work/listers/work/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"
)

// addonDeployController deploy addon agent resources on the managed cluster.
type addonDeployController struct {
	workClient                workv1client.Interface
	addonClient               addonv1alpha1client.Interface
	managedClusterLister      clusterlister.ManagedClusterLister
	managedClusterAddonLister addonlisterv1alpha1.ManagedClusterAddOnLister
	workLister                worklister.ManifestWorkLister
	agentAddons               map[string]agent.AgentAddon
	cache                     *workCache
}

func NewAddonDeployController(
	workClient workv1client.Interface,
	addonClient addonv1alpha1client.Interface,
	clusterInformers clusterinformers.ManagedClusterInformer,
	addonInformers addoninformerv1alpha1.ManagedClusterAddOnInformer,
	workInformers workinformers.ManifestWorkInformer,
	agentAddons map[string]agent.AgentAddon,
) factory.Controller {
	c := &addonDeployController{
		workClient:                workClient,
		addonClient:               addonClient,
		managedClusterLister:      clusterInformers.Lister(),
		managedClusterAddonLister: addonInformers.Lister(),
		workLister:                workInformers.Lister(),
		agentAddons:               agentAddons,
		cache:                     newWorkCache(),
	}

	return factory.New().WithFilteredEventsInformersQueueKeysFunc(
		func(obj runtime.Object) []string {
			key, _ := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
			return []string{key}
		},
		func(obj interface{}) bool {
			accessor, _ := meta.Accessor(obj)
			if _, ok := c.agentAddons[accessor.GetName()]; !ok {
				return false
			}

			return true
		},
		addonInformers.Informer()).
		WithFilteredEventsInformersQueueKeysFunc(
			func(obj runtime.Object) []string {
				accessor, _ := meta.Accessor(obj)
				return []string{fmt.Sprintf("%s/%s", accessor.GetNamespace(), accessor.GetLabels()[constants.AddonLabel])}
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
		WithSync(c.sync).ToController("addon-deploy-controller")
}

func (c *addonDeployController) sync(ctx context.Context, syncCtx factory.SyncContext, key string) error {
	klog.V(4).Infof("Reconciling addon deploy %q", key)

	clusterName, addonName, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		// ignore addon whose key is not in format: namespace/name
		return nil
	}

	agentAddon, ok := c.agentAddons[addonName]
	if !ok {
		return nil
	}

	// Get ManagedCluster
	managedCluster, err := c.managedClusterLister.Get(clusterName)
	if errors.IsNotFound(err) {
		c.cache.removeCache(constants.DeployWorkName(addonName), clusterName)
		return nil
	}
	if err != nil {
		return err
	}

	if !managedCluster.DeletionTimestamp.IsZero() {
		// managed cluster is deleting, do nothing
		return nil
	}

	managedClusterAddon, err := c.managedClusterAddonLister.ManagedClusterAddOns(clusterName).Get(addonName)
	if errors.IsNotFound(err) {
		c.cache.removeCache(constants.DeployWorkName(addonName), clusterName)
		return nil
	}
	if err != nil {
		return err
	}

	if !managedClusterAddon.DeletionTimestamp.IsZero() {
		c.cache.removeCache(constants.DeployWorkName(addonName), clusterName)
		return nil
	}

	owner := metav1.NewControllerRef(managedClusterAddon, addonapiv1alpha1.GroupVersion.WithKind("ManagedClusterAddOn"))

	managedClusterAddonCopy := managedClusterAddon.DeepCopy()
	objects, err := agentAddon.Manifests(managedCluster, managedClusterAddon)
	if err != nil {
		meta.SetStatusCondition(&managedClusterAddonCopy.Status.Conditions, metav1.Condition{
			Type:    constants.AddonManifestApplied,
			Status:  metav1.ConditionFalse,
			Reason:  constants.AddonManifestAppliedReasonWorkApplyFailed,
			Message: fmt.Sprintf("failed to get manifest from agent interface: %v", err),
		})
		if updateErr := utils.PatchAddonCondition(ctx, c.addonClient, managedClusterAddonCopy, managedClusterAddon); updateErr != nil {
			return fmt.Errorf("failed to update managedclusteraddon status: %v; the err should be %v", updateErr, err)
		}
		return err
	}
	if len(objects) == 0 {
		err = deleteWork(ctx, c.workClient, clusterName, constants.DeployWorkName(addonName))
		if err != nil {
			return err
		}
		c.cache.removeCache(constants.DeployWorkName(addonName), clusterName)
		return nil
	}

	work, _, err := newManagedManifestWorkBuilder(agentAddon.GetAgentAddonOptions().HostedModeEnabled).
		buildManifestWorkFromObject(clusterName, managedClusterAddon, objects)
	if err != nil {
		meta.SetStatusCondition(&managedClusterAddonCopy.Status.Conditions, metav1.Condition{
			Type:    constants.AddonManifestApplied,
			Status:  metav1.ConditionFalse,
			Reason:  constants.AddonManifestAppliedReasonWorkApplyFailed,
			Message: fmt.Sprintf("failed to build manifestwork: %v", err),
		})
		if updateErr := utils.PatchAddonCondition(ctx, c.addonClient, managedClusterAddonCopy, managedClusterAddon); updateErr != nil {
			return fmt.Errorf("failed to update managedclusteraddon status: %v; the err should be %v", updateErr, err)
		}
		return err
	}
	if work == nil {
		klog.V(4).Infof("No resource needs to deploy on the managed cluster %q", key)
		return nil
	}

	work.OwnerReferences = []metav1.OwnerReference{*owner}

	setStatusFeedbackRule(work, agentAddon)

	// apply work
	work, err = applyWork(ctx, c.workClient, c.workLister, c.cache, work)
	if err != nil {
		meta.SetStatusCondition(&managedClusterAddonCopy.Status.Conditions, metav1.Condition{
			Type:    constants.AddonManifestApplied,
			Status:  metav1.ConditionFalse,
			Reason:  constants.AddonManifestAppliedReasonWorkApplyFailed,
			Message: fmt.Sprintf("failed to apply manifestwork: %v", err),
		})
		if updateErr := utils.PatchAddonCondition(ctx, c.addonClient, managedClusterAddonCopy, managedClusterAddon); updateErr != nil {
			return fmt.Errorf("failed to update managedclusteraddon status: %v; the err should be %v", updateErr, err)
		}
		return err
	}

	// Update addon status based on work's status
	if meta.IsStatusConditionTrue(work.Status.Conditions, workapiv1.WorkApplied) {
		meta.SetStatusCondition(&managedClusterAddonCopy.Status.Conditions, metav1.Condition{
			Type:    constants.AddonManifestApplied,
			Status:  metav1.ConditionTrue,
			Reason:  constants.AddonManifestApplied,
			Message: "manifest of addon applied successfully",
		})
	} else {
		meta.SetStatusCondition(&managedClusterAddonCopy.Status.Conditions, metav1.Condition{
			Type:    constants.AddonManifestApplied,
			Status:  metav1.ConditionFalse,
			Reason:  constants.AddonManifestAppliedReasonManifestsApplyFailed,
			Message: fmt.Sprintf("work %s apply failed", work.Name),
		})
	}

	return utils.PatchAddonCondition(ctx, c.addonClient, managedClusterAddonCopy, managedClusterAddon)
}

func setStatusFeedbackRule(work *workapiv1.ManifestWork, agentAddon agent.AgentAddon) {
	if agentAddon.GetAgentAddonOptions().HealthProber == nil {
		return
	}

	if agentAddon.GetAgentAddonOptions().HealthProber.Type != agent.HealthProberTypeWork {
		return
	}

	if agentAddon.GetAgentAddonOptions().HealthProber.WorkProber == nil {
		return
	}

	probeRules := agentAddon.GetAgentAddonOptions().HealthProber.WorkProber.ProbeFields

	work.Spec.ManifestConfigs = []workapiv1.ManifestConfigOption{}

	for _, rule := range probeRules {
		work.Spec.ManifestConfigs = append(work.Spec.ManifestConfigs, workapiv1.ManifestConfigOption{
			ResourceIdentifier: rule.ResourceIdentifier,
			FeedbackRules:      rule.ProbeRules,
		})
	}
}
