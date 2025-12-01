package addonprogressing

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"open-cluster-management.io/addon-framework/pkg/addonmanager/constants"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/controllers/agentdeploy"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformerv1alpha1 "open-cluster-management.io/api/client/addon/informers/externalversions/addon/v1alpha1"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions/work/v1"
	worklister "open-cluster-management.io/api/client/work/listers/work/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	addonindex "open-cluster-management.io/ocm/pkg/addon/index"
	"open-cluster-management.io/ocm/pkg/common/queue"
)

// addonProgressingController reconciles instances of managedclusteraddon on the hub
// based to update the status progressing condition and last applied config
type addonProgressingController struct {
	addonClient                  addonv1alpha1client.Interface
	managedClusterAddonLister    addonlisterv1alpha1.ManagedClusterAddOnLister
	clusterManagementAddonLister addonlisterv1alpha1.ClusterManagementAddOnLister
	workLister                   worklister.ManifestWorkLister
	addonFilterFunc              factory.EventFilterFunc
}

func NewAddonProgressingController(
	addonClient addonv1alpha1client.Interface,
	addonInformers addoninformerv1alpha1.ManagedClusterAddOnInformer,
	clusterManagementAddonInformers addoninformerv1alpha1.ClusterManagementAddOnInformer,
	workInformers workinformers.ManifestWorkInformer,
	addonFilterFunc factory.EventFilterFunc,
) factory.Controller {
	c := &addonProgressingController{
		addonClient:                  addonClient,
		managedClusterAddonLister:    addonInformers.Lister(),
		clusterManagementAddonLister: clusterManagementAddonInformers.Lister(),
		workLister:                   workInformers.Lister(),
		addonFilterFunc:              addonFilterFunc,
	}

	return factory.New().WithInformersQueueKeysFunc(
		queue.QueueKeyByMetaNamespaceName, addonInformers.Informer()).
		WithInformersQueueKeysFunc(
			addonindex.ManagedClusterAddonByNameQueueKey(addonInformers),
			clusterManagementAddonInformers.Informer(),
		).
		WithFilteredEventsInformersQueueKeysFunc(
			func(obj runtime.Object) []string {
				accessor, _ := meta.Accessor(obj)
				namespace := accessor.GetNamespace()
				if len(accessor.GetLabels()[addonapiv1alpha1.AddonNamespaceLabelKey]) > 0 {
					namespace = accessor.GetLabels()[addonapiv1alpha1.AddonNamespaceLabelKey]
				}
				return []string{fmt.Sprintf("%s/%s", namespace, accessor.GetLabels()[addonapiv1alpha1.AddonLabelKey])}
			},
			func(obj interface{}) bool {
				accessor, _ := meta.Accessor(obj)
				return len(accessor.GetLabels()) > 0 && len(accessor.GetLabels()[addonapiv1alpha1.AddonLabelKey]) > 0
			},
			workInformers.Informer()).
		WithSync(c.sync).ToController("addon-progressing-controller")
}

func (c *addonProgressingController) sync(ctx context.Context, syncCtx factory.SyncContext, key string) error {
	logger := klog.FromContext(ctx).WithValues("addonName", key)
	logger.V(4).Info("Reconciling addon")

	namespace, addonName, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		// ignore addon whose key is invalid
		return nil
	}

	addon, err := c.managedClusterAddonLister.ManagedClusterAddOns(namespace).Get(addonName)
	switch {
	case errors.IsNotFound(err):
		return nil
	case err != nil:
		return err
	}

	clusterManagementAddon, err := c.clusterManagementAddonLister.Get(addonName)
	if errors.IsNotFound(err) {
		return nil
	}

	if err != nil {
		return err
	}

	if !c.addonFilterFunc(clusterManagementAddon) {
		return nil
	}

	// update progressing condition and last applied config
	_, err = c.updateAddonProgressingAndLastApplied(ctx, addon.DeepCopy(), addon)
	return err
}

func (c *addonProgressingController) updateAddonProgressingAndLastApplied(
	ctx context.Context, newaddon, oldaddon *addonapiv1alpha1.ManagedClusterAddOn) (bool, error) {
	patcher := patcher.NewPatcher[
		*addonapiv1alpha1.ManagedClusterAddOn,
		addonapiv1alpha1.ManagedClusterAddOnSpec,
		addonapiv1alpha1.ManagedClusterAddOnStatus](
		c.addonClient.AddonV1alpha1().ManagedClusterAddOns(newaddon.Namespace))
	// check config references
	if supported, config := isConfigurationSupported(newaddon); !supported {
		meta.SetStatusCondition(&newaddon.Status.Conditions, metav1.Condition{
			Type:   addonapiv1alpha1.ManagedClusterAddOnConditionProgressing,
			Status: metav1.ConditionFalse,
			Reason: addonapiv1alpha1.ProgressingReasonConfigurationUnsupported,
			Message: fmt.Sprintf("Configuration with gvr %s/%s is not supported for this addon",
				config.Group, config.Resource),
		})

		return patcher.PatchStatus(ctx, newaddon, newaddon.Status, oldaddon.Status)
	}

	// wait until addon has ManifestApplied condition
	if cond := meta.FindStatusCondition(
		newaddon.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnManifestApplied); cond == nil {
		meta.SetStatusCondition(&newaddon.Status.Conditions, metav1.Condition{
			Type:    addonapiv1alpha1.ManagedClusterAddOnConditionProgressing,
			Status:  metav1.ConditionFalse,
			Reason:  "WaitingForManifestApplied",
			Message: "Waiting for ManagedClusterAddOn ManifestApplied condition",
		})
		return patcher.PatchStatus(ctx, newaddon, newaddon.Status, oldaddon.Status)
	}

	var hostingClusterName string
	if newaddon.Annotations != nil && len(newaddon.Annotations[addonapiv1alpha1.HostingClusterNameAnnotationKey]) > 0 {
		hostingClusterName = newaddon.Annotations[addonapiv1alpha1.HostingClusterNameAnnotationKey]
	}

	if len(hostingClusterName) > 0 {
		// wait until addon has HostingManifestApplied condition
		if cond := meta.FindStatusCondition(
			newaddon.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnHostingManifestApplied); cond == nil {
			meta.SetStatusCondition(&newaddon.Status.Conditions, metav1.Condition{
				Type:    addonapiv1alpha1.ManagedClusterAddOnConditionProgressing,
				Status:  metav1.ConditionFalse,
				Reason:  "WaitingForHostingManifestApplied",
				Message: "Waiting for ManagedClusterAddOn HostingManifestApplied condition",
			})
			return patcher.PatchStatus(ctx, newaddon, newaddon.Status, oldaddon.Status)
		}
	}

	// get addon works
	// first get all works for addon in default mode.
	requirement, _ := labels.NewRequirement(addonapiv1alpha1.AddonLabelKey, selection.Equals, []string{newaddon.Name})
	namespaceNotExistRequirement, _ := labels.NewRequirement(addonapiv1alpha1.AddonNamespaceLabelKey, selection.DoesNotExist, []string{})
	selector := labels.NewSelector().Add(*requirement).Add(*namespaceNotExistRequirement)
	addonWorks, err := c.workLister.ManifestWorks(newaddon.Namespace).List(selector)
	if err != nil {
		setAddOnProgressingAndLastApplied(addonapiv1alpha1.ProgressingReasonFailed, err.Error(), newaddon)
		return patcher.PatchStatus(ctx, newaddon, newaddon.Status, oldaddon.Status)
	}

	// next get all works for addon in hosted mode
	if len(hostingClusterName) > 0 {
		namespaceRequirement, _ := labels.NewRequirement(addonapiv1alpha1.AddonNamespaceLabelKey, selection.Equals, []string{newaddon.Namespace})
		selector = labels.NewSelector().Add(*requirement).Add(*namespaceRequirement)
		hostedAddonWorks, err := c.workLister.ManifestWorks(hostingClusterName).List(selector)
		if err != nil {
			setAddOnProgressingAndLastApplied(addonapiv1alpha1.ProgressingReasonFailed, err.Error(), newaddon)
			return patcher.PatchStatus(ctx, newaddon, newaddon.Status, oldaddon.Status)
		}
		addonWorks = append(addonWorks, hostedAddonWorks...)
	}

	if len(addonWorks) == 0 {
		setAddOnProgressingAndLastApplied(addonapiv1alpha1.ProgressingReasonProgressing, "no addon works", newaddon)
		return patcher.PatchStatus(ctx, newaddon, newaddon.Status, oldaddon.Status)
	}

	// check addon manifestworks
	for _, work := range addonWorks {
		// skip pre-delete manifestwork
		if strings.HasPrefix(work.Name, constants.PreDeleteHookWorkName(newaddon.Name)) {
			continue
		}

		// check if work configs matches addon configs
		if !workConfigsMatchesAddon(klog.FromContext(ctx), work, newaddon) {
			setAddOnProgressingAndLastApplied(addonapiv1alpha1.ProgressingReasonProgressing, "mca and work configs mismatch", newaddon)
			return patcher.PatchStatus(ctx, newaddon, newaddon.Status, oldaddon.Status)
		}

		// check if work is ready
		if !workIsReady(work) {
			setAddOnProgressingAndLastApplied(addonapiv1alpha1.ProgressingReasonProgressing, "work is not ready", newaddon)
			return patcher.PatchStatus(ctx, newaddon, newaddon.Status, oldaddon.Status)
		}
	}

	// set lastAppliedConfig when all the work matches addon and are ready.
	setAddOnProgressingAndLastApplied(addonapiv1alpha1.ProgressingReasonCompleted, "", newaddon)
	return patcher.PatchStatus(ctx, newaddon, newaddon.Status, oldaddon.Status)
}

func isConfigurationSupported(addon *addonapiv1alpha1.ManagedClusterAddOn) (bool, addonapiv1alpha1.ConfigGroupResource) {
	supportedConfigSet := map[addonapiv1alpha1.ConfigGroupResource]bool{}
	for _, config := range addon.Status.SupportedConfigs {
		supportedConfigSet[config] = true
	}

	for _, config := range addon.Spec.Configs {
		if _, ok := supportedConfigSet[config.ConfigGroupResource]; !ok {
			return false, config.ConfigGroupResource
		}
	}

	return true, addonapiv1alpha1.ConfigGroupResource{}
}

func workConfigsMatchesAddon(logger klog.Logger, work *workapiv1.ManifestWork, addon *addonapiv1alpha1.ManagedClusterAddOn) bool {
	// get work spec hash
	if _, ok := work.Annotations[workapiv1.ManifestConfigSpecHashAnnotationKey]; !ok {
		return len(addon.Status.ConfigReferences) == 0
	}

	// parse work spec hash
	workSpecHashMap := make(map[string]string)
	if err := json.Unmarshal([]byte(work.Annotations[workapiv1.ManifestConfigSpecHashAnnotationKey]), &workSpecHashMap); err != nil {
		logger.Info("Failed to parse work spec hash", "error", err)
		return false
	}

	// check work spec hash, all the config should have spec hash
	for _, v := range workSpecHashMap {
		if v == "" {
			return false
		}
	}

	// check addon desired config
	for _, configReference := range addon.Status.ConfigReferences {
		if configReference.DesiredConfig == nil || configReference.DesiredConfig.SpecHash == "" {
			return false
		}
	}
	addonSpecHashMap := agentdeploy.ConfigsToMap(addon.Status.ConfigReferences)

	// compare work and addon configs
	return equality.Semantic.DeepEqual(workSpecHashMap, addonSpecHashMap)
}

// work is ready when
// 1) condition Available status is true.
// 2) condition Available observedGeneration equals to generation.
// 3) If it is a fresh install since one addon can have multiple ManifestWorks, the ManifestWork condition ManifestApplied must also be true.
func workIsReady(work *workapiv1.ManifestWork) bool {
	cond := meta.FindStatusCondition(work.Status.Conditions, workapiv1.WorkAvailable)
	if cond == nil || cond.Status != metav1.ConditionTrue || cond.ObservedGeneration != work.Generation {
		return false
	}
	cond = meta.FindStatusCondition(work.Status.Conditions, workapiv1.WorkApplied)
	if cond == nil || cond.Status != metav1.ConditionTrue || cond.ObservedGeneration != work.Generation {
		return false
	}

	return true
}

// Set addon progressing condition and last applied
// Once completed, the LastAppliedConfig will set to DesiredConfig. Even the condition status
// changes later, eg, spoke cluster becomes unhealthy, status becomes progressing or failed after completed,
// the LastAppliedConfig and DesiredConfig won't change. this is to avoid back and forth in rollout.
// The setRolloutStatus() func in graph.go only look at the LastAppliedConfig and DesiredConfig.
func setAddOnProgressingAndLastApplied(reason string, message string, addon *addonapiv1alpha1.ManagedClusterAddOn) {
	condition := metav1.Condition{
		Type:   addonapiv1alpha1.ManagedClusterAddOnConditionProgressing,
		Reason: reason,
	}
	switch reason {
	case addonapiv1alpha1.ProgressingReasonProgressing:
		condition.Status = metav1.ConditionTrue
		condition.Message = fmt.Sprintf("progressing... %v", message)
	case addonapiv1alpha1.ProgressingReasonCompleted:
		condition.Status = metav1.ConditionFalse
		for i, configReference := range addon.Status.ConfigReferences {
			addon.Status.ConfigReferences[i].LastAppliedConfig = configReference.DesiredConfig.DeepCopy()
		}
		condition.Message = "completed with no errors."
	case addonapiv1alpha1.ProgressingReasonFailed:
		condition.Status = metav1.ConditionFalse
		condition.Message = message
	}
	meta.SetStatusCondition(&addon.Status.Conditions, condition)
}
