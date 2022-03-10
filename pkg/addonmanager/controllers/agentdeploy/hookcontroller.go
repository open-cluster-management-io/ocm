package agentdeploy

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
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterlister "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	workv1client "open-cluster-management.io/api/client/work/clientset/versioned"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions/work/v1"
	worklister "open-cluster-management.io/api/client/work/listers/work/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"
)

// addonHookDeployController reconciles instances of managedClusterAddon and hook manifestWork on the hub.
type addonHookDeployController struct {
	workClient                workv1client.Interface
	addonClient               addonv1alpha1client.Interface
	managedClusterLister      clusterlister.ManagedClusterLister
	managedClusterAddonLister addonlisterv1alpha1.ManagedClusterAddOnLister
	workLister                worklister.ManifestWorkLister
	agentAddons               map[string]agent.AgentAddon
	cache                     *workCache
	eventRecorder             events.Recorder
}

func NewAddonHookDeployController(
	workClient workv1client.Interface,
	addonClient addonv1alpha1client.Interface,
	clusterInformers clusterinformers.ManagedClusterInformer,
	addonInformers addoninformerv1alpha1.ManagedClusterAddOnInformer,
	workInformers workinformers.ManifestWorkInformer,
	agentAddons map[string]agent.AgentAddon,
	recorder events.Recorder,
) factory.Controller {
	c := &addonHookDeployController{
		workClient:                workClient,
		addonClient:               addonClient,
		managedClusterLister:      clusterInformers.Lister(),
		managedClusterAddonLister: addonInformers.Lister(),
		workLister:                workInformers.Lister(),
		agentAddons:               agentAddons,
		cache:                     newWorkCache(),
		eventRecorder:             recorder.WithComponentSuffix("addon-hook-deploy-controller"),
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

				if accessor.GetName() != preDeleteHookWorkName(addonName) {
					return false
				}
				return true
			},
			workInformers.Informer(),
		).
		WithSync(c.sync).ToController("addon-hook-deploy-controller", recorder)
}

func (c *addonHookDeployController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	key := syncCtx.QueueKey()
	klog.V(4).Infof("Reconciling addon hook deploy %q", key)

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
		c.cache.removeCache(preDeleteHookWorkName(addonName), clusterName)
		return nil
	}
	if err != nil {
		return err
	}

	// should continue to apply pre-delete hook when the managedCluster is deleting.

	managedClusterAddon, err := c.managedClusterAddonLister.ManagedClusterAddOns(clusterName).Get(addonName)
	if errors.IsNotFound(err) {
		c.cache.removeCache(preDeleteHookWorkName(addonName), clusterName)
		return nil
	}
	if err != nil {
		return err
	}
	managedClusterAddonCopy := managedClusterAddon.DeepCopy()
	owner := metav1.NewControllerRef(managedClusterAddon, addonapiv1alpha1.GroupVersion.WithKind("ManagedClusterAddOn"))

	objects, err := agentAddon.Manifests(managedCluster, managedClusterAddon)
	if err != nil {
		return err
	}
	if len(objects) == 0 {
		return nil
	}
	_, hookWork, err := buildManifestWorkFromObject(clusterName, addonName, objects)
	if err != nil {
		return err
	}

	if hookWork == nil {
		if !hasFinalizer(managedClusterAddonCopy.GetFinalizers(), constants.PreDeleteHookFinalizer) {
			return nil
		}
		finalizer := removeFinalizer(managedClusterAddonCopy.Finalizers, constants.PreDeleteHookFinalizer)
		managedClusterAddonCopy.SetFinalizers(finalizer)
		_, err = c.addonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterAddonCopy.Namespace).Update(
			ctx, managedClusterAddonCopy, metav1.UpdateOptions{})
		return err
	}

	if managedClusterAddon.DeletionTimestamp.IsZero() {
		if !hasFinalizer(managedClusterAddonCopy.GetFinalizers(), constants.PreDeleteHookFinalizer) {
			managedClusterAddonCopy.Finalizers = append(managedClusterAddonCopy.Finalizers, constants.PreDeleteHookFinalizer)
			_, err = c.addonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterAddonCopy.Namespace).Update(
				ctx, managedClusterAddonCopy, metav1.UpdateOptions{})
			return err
		}
		return nil
	}

	// apply hookWork when addon is deleting
	hookWork.OwnerReferences = []metav1.OwnerReference{*owner}
	hookWork, applyErr := applyWork(c.workClient, c.workLister, c.cache, c.eventRecorder, ctx, hookWork)
	completed := hookWorkIsCompleted(hookWork)
	if completed && hasFinalizer(managedClusterAddonCopy.Finalizers, constants.PreDeleteHookFinalizer) {
		finalizer := removeFinalizer(managedClusterAddonCopy.Finalizers, constants.PreDeleteHookFinalizer)
		managedClusterAddonCopy.SetFinalizers(finalizer)
		_, err = c.addonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterAddonCopy.Namespace).Update(
			ctx, managedClusterAddonCopy, metav1.UpdateOptions{})
		return err
	}

	switch {
	case applyErr != nil:
		meta.SetStatusCondition(&managedClusterAddonCopy.Status.Conditions, metav1.Condition{
			Type:    "HookManifestCompleted",
			Status:  metav1.ConditionFalse,
			Reason:  "ManifestWorkApplyFailed",
			Message: fmt.Sprintf("failed to apply hook manifestwork: %v", err),
		})
	case completed:
		meta.SetStatusCondition(&managedClusterAddonCopy.Status.Conditions, metav1.Condition{
			Type:    "HookManifestCompleted",
			Status:  metav1.ConditionTrue,
			Reason:  "HookManifestIsCompleted",
			Message: fmt.Sprintf("hook manifestWork %v is completed.", hookWork.Name),
		})
	default:
		meta.SetStatusCondition(&managedClusterAddonCopy.Status.Conditions, metav1.Condition{
			Type:    "HookManifestCompleted",
			Status:  metav1.ConditionFalse,
			Reason:  "HookManifestIsNotCompleted",
			Message: fmt.Sprintf("hook manifestWork %v is not completed.", hookWork.Name),
		})
	}

	if !equality.Semantic.DeepEqual(managedClusterAddonCopy.Status, managedClusterAddon.Status) {
		_, err = c.addonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterAddonCopy.Namespace).UpdateStatus(
			ctx, managedClusterAddonCopy, metav1.UpdateOptions{})
		return err
	}

	return nil
}

// hookWorkIsCompleted checks the hook resources are completed.
// hookManifestWork is completed if all resources are completed.
// currently, we only support job and pod as hook manifest.
// job is completed if the Completed condition of status is true.
// pod is completed if the phase of status is Succeeded.
func hookWorkIsCompleted(hookWork *workapiv1.ManifestWork) bool {
	if hookWork == nil {
		return false
	}
	if !meta.IsStatusConditionTrue(hookWork.Status.Conditions, workapiv1.WorkApplied) {
		return false
	}
	if !meta.IsStatusConditionTrue(hookWork.Status.Conditions, workapiv1.WorkAvailable) {
		return false
	}

	if len(hookWork.Spec.ManifestConfigs) == 0 {
		klog.Errorf("the hook manifestWork should have manifest configs,but got 0.")
		return false
	}
	for _, manifestConfig := range hookWork.Spec.ManifestConfigs {
		switch manifestConfig.ResourceIdentifier.Resource {
		case "jobs":
			value := FindManifestValue(hookWork.Status.ResourceStatus, manifestConfig.ResourceIdentifier, "JobComplete")
			if value.Type == "" {
				return false
			}
			if value.String == nil {
				return false
			}
			if *value.String != "True" {
				return false
			}

		case "pods":
			value := FindManifestValue(hookWork.Status.ResourceStatus, manifestConfig.ResourceIdentifier, "PodPhase")
			if value.Type == "" {
				return false
			}
			if value.String == nil {
				return false
			}
			if *value.String != "Succeeded" {
				return false
			}
		default:
			return false
		}
	}

	return true
}
