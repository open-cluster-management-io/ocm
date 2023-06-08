package addontemplate

import (
	"context"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformerv1alpha1 "open-cluster-management.io/api/client/addon/informers/externalversions/addon/v1alpha1"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"

	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"
	"open-cluster-management.io/addon-framework/pkg/basecontroller/factory"
	"open-cluster-management.io/addon-framework/pkg/utils"
)

// addonTemplateController monitors ManagedClusterAddOns on hub to get all the in-used addon templates,
// and starts an addon manager for every addon template to handle agent requests deployed by this template
type addonTemplateController struct {
	// addonManagers holds all addon managers that will be deployed with template type addons.
	// The key is the name of the template type addon.
	addonManagers map[string]context.CancelFunc

	kubeConfig    *rest.Config
	addonClient   addonv1alpha1client.Interface
	hubKubeClient kubernetes.Interface
	cmaLister     addonlisterv1alpha1.ClusterManagementAddOnLister
}

// NewAddonTemplateController returns an instance of addonTemplateController
func NewAddonTemplateController(
	hubKubeconfig *rest.Config,
	hubKubeClient kubernetes.Interface,
	addonClient addonv1alpha1client.Interface,
	cmaInformer addoninformerv1alpha1.ClusterManagementAddOnInformer,
) factory.Controller {
	c := &addonTemplateController{
		kubeConfig:    hubKubeconfig,
		hubKubeClient: hubKubeClient,
		addonClient:   addonClient,
		cmaLister:     cmaInformer.Lister(),
		addonManagers: make(map[string]context.CancelFunc),
	}

	return factory.New().WithFilteredEventsInformersQueueKeysFunc(
		func(obj runtime.Object) []string {
			key, _ := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
			return []string{key}
		},
		func(obj interface{}) bool {
			// TODO: filter managed cluster addon that does have addon template configReferences in status.
			return true
		},
		cmaInformer.Informer()).
		WithSync(c.sync).
		ToController("addon-template-controller")
}

func (c *addonTemplateController) stopUnusedManagers(
	ctx context.Context, syncCtx factory.SyncContext, addOnName string) {

	stopFunc, ok := c.addonManagers[addOnName]
	if ok {
		stopFunc()
	}
}

func (c *addonTemplateController) sync(ctx context.Context, syncCtx factory.SyncContext, key string) error {
	_, addonName, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		// ignore addon whose key is not in format: namespace/name
		return nil
	}

	cma, err := c.cmaLister.Get(addonName)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}

	if !utils.SupportAddOnTemplate(cma) {
		c.stopUnusedManagers(ctx, syncCtx, cma.Name)
		return nil
	}

	_, exist := c.addonManagers[addonName]
	if exist {
		klog.Infof("There already is a manager started for addon %s, skip.", addonName)
		return nil
	}

	klog.Infof("Start an addon manager for addon %s", addonName)

	stopFunc := c.startManager(ctx, addonName)
	c.addonManagers[addonName] = stopFunc
	return nil
}

func (c *addonTemplateController) startManager(
	pctx context.Context,
	addonName string) context.CancelFunc {
	ctx, stopFunc := context.WithCancel(pctx)
	go func() {
		err := c.runController(ctx, addonName)
		if err != nil {
			klog.Errorf("run controller for addon %s error: %v", addonName, err)
			utilruntime.HandleError(err)
		}
	}()
	return stopFunc
}

func (c *addonTemplateController) runController(
	ctx context.Context, addonName string) error {
	mgr, err := addonmanager.New(c.kubeConfig)
	if err != nil {
		return err
	}

	agentAddon := addonfactory.NewCRDTemplateAgentAddon(
		addonName,
		c.hubKubeClient,
		c.addonClient,
		addonfactory.GetAddOnDeploymentConfigValues(
			addonfactory.NewAddOnDeploymentConfigGetter(c.addonClient),
			addonfactory.ToAddOnCustomizedVariableValues,
			addonfactory.ToAddOnNodePlacementPrivateValues,
			addonfactory.ToAddOnRegistriesPrivateValues,
		),
	)
	err = mgr.AddAgent(agentAddon)
	if err != nil {
		return err
	}

	err = mgr.Start(ctx)
	if err != nil {
		return err
	}
	<-ctx.Done()

	klog.Infof("Addon %s Manager stopped", addonName)
	return nil
}
