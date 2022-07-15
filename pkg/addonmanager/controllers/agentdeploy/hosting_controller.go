package agentdeploy

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
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

// addonHostingDeployController deploy addon agent resources on the hosting cluster in Hosted mode.
type addonHostingDeployController struct {
	workClient                workv1client.Interface
	addonClient               addonv1alpha1client.Interface
	managedClusterLister      clusterlister.ManagedClusterLister
	managedClusterAddonLister addonlisterv1alpha1.ManagedClusterAddOnLister
	workLister                worklister.ManifestWorkLister
	agentAddons               map[string]agent.AgentAddon
	cache                     *workCache
}

func NewAddonHostingDeployController(
	workClient workv1client.Interface,
	addonClient addonv1alpha1client.Interface,
	clusterInformers clusterinformers.ManagedClusterInformer,
	addonInformers addoninformerv1alpha1.ManagedClusterAddOnInformer,
	workInformers workinformers.ManifestWorkInformer,
	agentAddons map[string]agent.AgentAddon,
) factory.Controller {
	c := &addonHostingDeployController{
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
			key, _ := cache.MetaNamespaceKeyFunc(obj)
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
				return []string{
					fmt.Sprintf("%s/%s",
						accessor.GetLabels()[constants.AddonNamespaceLabel],
						accessor.GetLabels()[constants.AddonLabel],
					),
				}
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
				addonNamespace, ok := accessor.GetLabels()[constants.AddonNamespaceLabel]
				if !ok {
					return false
				}

				if _, ok := c.agentAddons[addonName]; !ok {
					return false
				}
				if accessor.GetName() != constants.DeployHostingWorkName(addonNamespace, addonName) {
					return false
				}
				return true
			},
			workInformers.Informer(),
		).
		WithSync(c.sync).ToController("addon-hosting-deploy-controller")
}

// TODO: refactoring to reduce duplication of code
func (c *addonHostingDeployController) sync(ctx context.Context, syncCtx factory.SyncContext, key string) error {
	klog.V(4).Infof("Reconciling addon hosting deploy %q", key)

	clusterName, addonName, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		// ignore addon whose key is not in format: namespace/name
		return nil
	}

	agentAddon, ok := c.agentAddons[addonName]
	if !ok {
		return nil
	}

	// Hosted mode is not enabled, will not deploy any resource on the hosting cluster
	if !agentAddon.GetAgentAddonOptions().HostedModeEnabled {
		return nil
	}

	// Get ManagedCluster
	managedCluster, err := c.managedClusterLister.Get(clusterName)
	if errors.IsNotFound(err) {
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
		return nil
	}
	if err != nil {
		return err
	}

	managedClusterAddonCopy := managedClusterAddon.DeepCopy()

	installMode, hostingClusterName := constants.GetHostedModeInfo(managedClusterAddon.GetAnnotations())
	if installMode != constants.InstallModeHosted {
		// the deploy mode changed from hosted to default, cleanup the hosting resources
		return c.cleanup(ctx, managedClusterAddonCopy, "")
	}

	// Get Hosting Cluster, check whether the hosting cluster is a managed cluster of the hub
	// TODO: check whether the hosting cluster of the addon is the same hosting cluster of the klusterlet
	hostingCluster, err := c.managedClusterLister.Get(hostingClusterName)
	if errors.IsNotFound(err) {
		if err := c.cleanup(ctx, managedClusterAddonCopy, ""); err != nil {
			return err
		}
		meta.SetStatusCondition(&managedClusterAddonCopy.Status.Conditions, metav1.Condition{
			Type:    constants.HostingClusterValidity,
			Status:  metav1.ConditionFalse,
			Reason:  constants.HostingClusterValidityReasonInvalid,
			Message: fmt.Sprintf("hosting cluster %s is not a managed cluster of the hub", hostingClusterName),
		})
		if pErr := utils.PatchAddonCondition(
			ctx, c.addonClient, managedClusterAddonCopy, managedClusterAddon); pErr != nil {
			return fmt.Errorf("failed to update managedclusteraddon status: %v; the err should be %v", pErr, err)
		}

		return nil
	}
	if err != nil {
		return err
	}
	meta.SetStatusCondition(&managedClusterAddonCopy.Status.Conditions, metav1.Condition{
		Type:    constants.HostingClusterValidity,
		Status:  metav1.ConditionTrue,
		Reason:  constants.HostingClusterValidityReasonValid,
		Message: fmt.Sprintf("hosting cluster %s is a managed cluster of the hub", hostingClusterName),
	})

	if !managedClusterAddon.DeletionTimestamp.IsZero() {
		return c.cleanup(ctx, managedClusterAddonCopy, hostingClusterName)
	}

	if !hostingCluster.DeletionTimestamp.IsZero() {
		return c.cleanup(ctx, managedClusterAddonCopy, hostingClusterName)
	}

	if !hasFinalizer(managedClusterAddonCopy.Finalizers, constants.HostingManifestFinalizer) {
		finalizers := addFinalizer(managedClusterAddonCopy.Finalizers, constants.HostingManifestFinalizer)
		managedClusterAddonCopy.SetFinalizers(finalizers)
		_, err = c.addonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterAddonCopy.Namespace).Update(
			ctx, managedClusterAddonCopy, metav1.UpdateOptions{})
		return err
	}

	objects, err := agentAddon.Manifests(managedCluster, managedClusterAddon)
	if err != nil {
		meta.SetStatusCondition(&managedClusterAddonCopy.Status.Conditions, metav1.Condition{
			Type:    constants.AddonHostingManifestApplied,
			Status:  metav1.ConditionFalse,
			Reason:  constants.AddonManifestAppliedReasonWorkApplyFailed,
			Message: fmt.Sprintf("failed to get manifest from agent interface: %v", err),
		})
		if pErr := utils.PatchAddonCondition(
			ctx, c.addonClient, managedClusterAddonCopy, managedClusterAddon); pErr != nil {
			return fmt.Errorf("failed to update managedclusteraddon status: %v; the err should be %v", pErr, err)
		}
		return err
	}

	if len(objects) == 0 {
		return nil
	}

	work, _, err := newHostingManifestWorkBuilder(agentAddon.GetAgentAddonOptions().HostedModeEnabled).
		buildManifestWorkFromObject(hostingClusterName, managedClusterAddon, objects)
	if err != nil {
		meta.SetStatusCondition(&managedClusterAddonCopy.Status.Conditions, metav1.Condition{
			Type:    constants.AddonHostingManifestApplied,
			Status:  metav1.ConditionFalse,
			Reason:  constants.AddonManifestAppliedReasonWorkApplyFailed,
			Message: fmt.Sprintf("failed to build manifestwork: %v", err),
		})
		if pErr := utils.PatchAddonCondition(
			ctx, c.addonClient, managedClusterAddonCopy, managedClusterAddon); pErr != nil {
			return fmt.Errorf("failed to update managedclusteraddon status: %v; the err should be %v", pErr, err)
		}
		return err
	}
	if work == nil {
		klog.V(4).Infof("No resource needs to deploy on the hosting cluster %q", key)
		return nil
	}

	setStatusFeedbackRule(work, agentAddon)

	// apply work
	work, err = applyWork(ctx, c.workClient, c.workLister, c.cache, work)
	if err != nil {
		meta.SetStatusCondition(&managedClusterAddonCopy.Status.Conditions, metav1.Condition{
			Type:    constants.AddonHostingManifestApplied,
			Status:  metav1.ConditionFalse,
			Reason:  constants.AddonManifestAppliedReasonWorkApplyFailed,
			Message: fmt.Sprintf("failed to apply manifestwork: %v", err),
		})
		if pErr := utils.PatchAddonCondition(
			ctx, c.addonClient, managedClusterAddonCopy, managedClusterAddon); pErr != nil {
			return fmt.Errorf("failed to update managedclusteraddon status: %v; the err should be %v", pErr, err)
		}
		return err
	}

	// Update addon status based on work's status
	if meta.IsStatusConditionTrue(work.Status.Conditions, workapiv1.WorkApplied) {
		meta.SetStatusCondition(&managedClusterAddonCopy.Status.Conditions, metav1.Condition{
			Type:    constants.AddonHostingManifestApplied,
			Status:  metav1.ConditionTrue,
			Reason:  constants.AddonManifestApplied,
			Message: "manifest of addon applied successfully",
		})
	} else {
		meta.SetStatusCondition(&managedClusterAddonCopy.Status.Conditions, metav1.Condition{
			Type:    constants.AddonHostingManifestApplied,
			Status:  metav1.ConditionFalse,
			Reason:  constants.AddonManifestAppliedReasonManifestsApplyFailed,
			Message: fmt.Sprintf("work %s apply failed", work.Name),
		})
	}

	return utils.PatchAddonCondition(ctx, c.addonClient, managedClusterAddonCopy, managedClusterAddon)
}

// cleanup will delete the hosting manifestwork and remove the finalizer, if the hostingClusterName is empty, will try
// to find out the hosting cluster by manifestwork labels and do the cleanup
func (c *addonHostingDeployController) cleanup(
	ctx context.Context,
	mca *addonapiv1alpha1.ManagedClusterAddOn,
	hostingClusterName string) (err error) {

	if !hasFinalizer(mca.Finalizers, constants.HostingManifestFinalizer) {
		return nil
	}

	if len(hostingClusterName) == 0 {
		hostingClusterName, err = c.findHostingCluster(mca.Namespace, mca.Name)
		if err != nil {
			return err
		}
	}

	err = deleteWork(ctx, c.workClient, hostingClusterName,
		constants.DeployHostingWorkName(mca.Namespace, mca.Name))
	if err != nil {
		return err
	}

	c.cache.removeCache(constants.DeployHostingWorkName(mca.Namespace, mca.Name), hostingClusterName)

	finalizer := removeFinalizer(mca.Finalizers, constants.HostingManifestFinalizer)
	mca.SetFinalizers(finalizer)
	_, err = c.addonClient.AddonV1alpha1().ManagedClusterAddOns(mca.Namespace).Update(
		ctx, mca, metav1.UpdateOptions{})
	return err
}

// findHostingCluster try to get the hosting cluster name by the manifestwork labels
// TODO: consider to use the index informor to get the manifestwork
/*
	const ByAddonKey = "IndexByAddon"
 	workInformers.Work().V1().ManifestWorks().Informer().AddIndexers(map[string]cache.IndexFunc{
		ByAddonKey: func(obj interface{}) ([]string, error) {
			for _, label := range obj.(*workapiv1.ManifestWork).Labels {
				...
			}
			return []string{}, nil
		},
	})

	workInformers.Work().V1().ManifestWorks().Informer().GetIndexer().ByIndex(ByAddonKey,"addonKeyValue")
*/
func (c *addonHostingDeployController) findHostingCluster(addonNamespace, addonName string) (string, error) {
	nsReq, err := labels.NewRequirement(constants.AddonNamespaceLabel, selection.Equals, []string{addonNamespace})
	if err != nil {
		return "", fmt.Errorf("new namespace requirement for addon %s/%s error: %s", addonNamespace, addonName, err)
	}
	nameReq, err := labels.NewRequirement(constants.AddonLabel, selection.Equals, []string{addonName})
	if err != nil {
		return "", fmt.Errorf("new name requirement for addon %s/%s error: %s", addonNamespace, addonName, err)
	}

	mws, err := c.workLister.ManifestWorks(metav1.NamespaceAll).List(labels.NewSelector().Add(*nsReq, *nameReq))
	if err != nil {
		return "", fmt.Errorf("list manifestwork for addon %s/%s error: %s", addonNamespace, addonName, err)
	}
	for _, mw := range mws {
		if mw.Name == constants.DeployHostingWorkName(addonNamespace, addonName) {
			return mw.Namespace, nil
		}
	}

	return "", fmt.Errorf("hosting cluster not found")
}
