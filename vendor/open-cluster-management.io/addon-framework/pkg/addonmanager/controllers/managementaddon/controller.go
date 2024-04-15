package managementaddon

import (
	"context"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformerv1alpha1 "open-cluster-management.io/api/client/addon/informers/externalversions/addon/v1alpha1"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/basecontroller/factory"
)

const (
	controllerName = "management-addon-controller"
)

// clusterManagementAddonController reconciles cma on the hub.
type clusterManagementAddonController struct {
	addonClient                  addonv1alpha1client.Interface
	clusterManagementAddonLister addonlisterv1alpha1.ClusterManagementAddOnLister
	agentAddons                  map[string]agent.AgentAddon
	addonFilterFunc              factory.EventFilterFunc
	addonPatcher                 patcher.Patcher[*addonapiv1alpha1.ClusterManagementAddOn,
		addonapiv1alpha1.ClusterManagementAddOnSpec,
		addonapiv1alpha1.ClusterManagementAddOnStatus]
}

func NewManagementAddonController(
	addonClient addonv1alpha1client.Interface,
	clusterManagementAddonInformers addoninformerv1alpha1.ClusterManagementAddOnInformer,
	agentAddons map[string]agent.AgentAddon,
	addonFilterFunc factory.EventFilterFunc,
) factory.Controller {
	syncCtx := factory.NewSyncContext(controllerName)

	c := &clusterManagementAddonController{
		addonClient:                  addonClient,
		clusterManagementAddonLister: clusterManagementAddonInformers.Lister(),
		agentAddons:                  agentAddons,
		addonFilterFunc:              addonFilterFunc,
		addonPatcher: patcher.NewPatcher[*addonapiv1alpha1.ClusterManagementAddOn,
			addonapiv1alpha1.ClusterManagementAddOnSpec,
			addonapiv1alpha1.ClusterManagementAddOnStatus](addonClient.AddonV1alpha1().ClusterManagementAddOns()),
	}

	return factory.New().
		WithSyncContext(syncCtx).
		WithFilteredEventsInformersQueueKeysFunc(
			func(obj runtime.Object) []string {
				key, _ := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
				return []string{key}
			},
			c.addonFilterFunc, clusterManagementAddonInformers.Informer()).
		WithSync(c.sync).ToController(controllerName)
}

func (c *clusterManagementAddonController) sync(ctx context.Context, syncCtx factory.SyncContext, key string) error {
	_, addonName, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		// ignore addon whose key is invalid
		return nil
	}

	cma, err := c.clusterManagementAddonLister.Get(addonName)
	if errors.IsNotFound(err) {
		// addon cloud be deleted, ignore
		return nil
	}
	if err != nil {
		return err
	}

	addon := c.agentAddons[cma.GetName()]
	if addon.GetAgentAddonOptions().InstallStrategy == nil {
		return nil
	}

	// If the addon defines install strategy via WithInstallStrategy(), force add annotation "addon.open-cluster-management.io/lifecycle: self" to cma.
	// The annotation with value "self" will be removed when remove WithInstallStrategy() in addon-framework.
	// The migration plan refer to https://github.com/open-cluster-management-io/ocm/issues/355.
	cmaCopy := cma.DeepCopy()
	if cmaCopy.Annotations == nil {
		cmaCopy.Annotations = map[string]string{}
	}
	cmaCopy.Annotations[addonapiv1alpha1.AddonLifecycleAnnotationKey] = addonapiv1alpha1.AddonLifecycleSelfManageAnnotationValue

	_, err = c.addonPatcher.PatchLabelAnnotations(ctx, cmaCopy, cmaCopy.ObjectMeta, cma.ObjectMeta)
	return err
}
