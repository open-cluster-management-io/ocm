package addontemplate

import (
	"context"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/dynamic/dynamicinformer"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"
	"open-cluster-management.io/addon-framework/pkg/utils"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"
	clusterv1informers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	workv1client "open-cluster-management.io/api/client/work/clientset/versioned"
	workv1informers "open-cluster-management.io/api/client/work/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

	"open-cluster-management.io/ocm/pkg/addon/templateagent"
	"open-cluster-management.io/ocm/pkg/common/queue"
)

// addonTemplateController monitors ManagedClusterAddOns on hub to get all the in-used addon templates,
// and starts an addon manager for every addon template to handle agent requests deployed by this template
type addonTemplateController struct {
	// addonManagers holds all addon managers that will be deployed with template type addons.
	// The key is the name of the template type addon.
	addonManagers map[string]context.CancelFunc

	kubeConfig        *rest.Config
	addonClient       addonv1alpha1client.Interface
	workClient        workv1client.Interface
	kubeClient        kubernetes.Interface
	cmaLister         addonlisterv1alpha1.ClusterManagementAddOnLister
	addonInformers    addoninformers.SharedInformerFactory
	clusterInformers  clusterv1informers.SharedInformerFactory
	dynamicInformers  dynamicinformer.DynamicSharedInformerFactory
	workInformers     workv1informers.SharedInformerFactory
	runControllerFunc runController
}

type runController func(ctx context.Context, addonName string) error

// NewAddonTemplateController returns an instance of addonTemplateController
func NewAddonTemplateController(
	hubKubeconfig *rest.Config,
	hubKubeClient kubernetes.Interface,
	addonClient addonv1alpha1client.Interface,
	workClient workv1client.Interface,
	addonInformers addoninformers.SharedInformerFactory,
	clusterInformers clusterv1informers.SharedInformerFactory,
	dynamicInformers dynamicinformer.DynamicSharedInformerFactory,
	workInformers workv1informers.SharedInformerFactory,
	recorder events.Recorder,
	runController ...runController,
) factory.Controller {
	c := &addonTemplateController{
		kubeConfig:       hubKubeconfig,
		kubeClient:       hubKubeClient,
		addonClient:      addonClient,
		workClient:       workClient,
		cmaLister:        addonInformers.Addon().V1alpha1().ClusterManagementAddOns().Lister(),
		addonManagers:    make(map[string]context.CancelFunc),
		addonInformers:   addonInformers,
		clusterInformers: clusterInformers,
		dynamicInformers: dynamicInformers,
		workInformers:    workInformers,
	}

	if len(runController) > 0 {
		c.runControllerFunc = runController[0]
	} else {
		// easy to mock in unit tests
		c.runControllerFunc = c.runController
	}
	return factory.New().WithInformersQueueKeysFunc(
		queue.QueueKeyByMetaNamespaceName,
		addonInformers.Addon().V1alpha1().ClusterManagementAddOns().Informer()).
		WithSync(c.sync).
		ToController("addon-template-controller", recorder)
}

func (c *addonTemplateController) stopUnusedManagers(
	ctx context.Context, syncCtx factory.SyncContext, addOnName string) {
	// TODO: check if all managed cluster addon instances are deleted
	stopFunc, ok := c.addonManagers[addOnName]
	if ok {
		stopFunc()
		delete(c.addonManagers, addOnName)
		klog.FromContext(ctx).Info("Stopping the manager for addon", "addonName", addOnName)
	}
}

func (c *addonTemplateController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	logger := klog.FromContext(ctx)
	key := syncCtx.QueueKey()
	_, addonName, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		// ignore addon whose key is not in format: namespace/name
		return nil
	}

	cma, err := c.cmaLister.Get(addonName)
	if err != nil {
		if errors.IsNotFound(err) {
			c.stopUnusedManagers(ctx, syncCtx, addonName)
			return nil
		}
		return err
	}

	if !templateagent.SupportAddOnTemplate(cma) {
		c.stopUnusedManagers(ctx, syncCtx, cma.Name)
		return nil
	}

	_, exist := c.addonManagers[addonName]
	if exist {
		logger.Info("There already is a manager started for addon, skipping", "addonName", addonName)
		return nil
	}

	logger.Info("Starting an addon manager for addon", "addonName", addonName)

	stopFunc := c.startManager(ctx, addonName)
	c.addonManagers[addonName] = stopFunc
	return nil
}

func (c *addonTemplateController) startManager(
	pctx context.Context,
	addonName string) context.CancelFunc {
	ctx, stopFunc := context.WithCancel(pctx)
	logger := klog.FromContext(ctx)
	go func() {
		err := c.runControllerFunc(ctx, addonName)
		if err != nil {
			logger.Error(err, "Error running controller for addon", "addonName", addonName)
			utilruntime.HandleError(err)
		}

		// use the parent context to start all shared informers, otherwise once the context is cancelled,
		// the informers will stop and all other shared go routines will be impacted.
		c.workInformers.Start(pctx.Done())
		c.addonInformers.Start(pctx.Done())
		c.clusterInformers.Start(pctx.Done())
		c.dynamicInformers.Start(pctx.Done())

		<-ctx.Done()
		logger.Info("Addon Manager stopped", "addonName", addonName)
	}()
	return stopFunc
}

func (c *addonTemplateController) runController(
	ctx context.Context, addonName string) error {
	mgr, err := addonmanager.New(c.kubeConfig)
	if err != nil {
		return err
	}

	kubeInformers := kubeinformers.NewSharedInformerFactoryWithOptions(c.kubeClient, 10*time.Minute,
		kubeinformers.WithTweakListOptions(func(listOptions *metav1.ListOptions) {
			selector := &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      addonv1alpha1.AddonLabelKey,
						Operator: metav1.LabelSelectorOpIn,
						Values:   []string{addonName},
					},
				},
			}
			listOptions.LabelSelector = metav1.FormatLabelSelector(selector)
		}),
	)
	getValuesClosure := func(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) (addonfactory.Values, error) {
		return templateagent.GetAddOnRegistriesPrivateValuesFromClusterAnnotation(klog.FromContext(ctx), cluster, addon)
	}
	agentAddon := templateagent.NewCRDTemplateAgentAddon(
		ctx,
		addonName,
		c.kubeClient,
		c.addonClient,
		c.addonInformers,
		kubeInformers.Rbac().V1().RoleBindings().Lister(),
		// image overrides from cluster annotation has lower priority than from the addonDeploymentConfig
		getValuesClosure,
		addonfactory.GetAddOnDeploymentConfigValues(
			utils.NewAddOnDeploymentConfigGetter(c.addonClient),
			addonfactory.ToAddOnCustomizedVariableValues,
			templateagent.ToAddOnNodePlacementPrivateValues,
			templateagent.ToAddOnRegistriesPrivateValues,
			templateagent.ToAddOnInstallNamespacePrivateValues,
		),
	)
	err = mgr.AddAgent(agentAddon)
	if err != nil {
		return err
	}

	err = mgr.StartWithInformers(ctx, c.workClient, c.workInformers.Work().V1().ManifestWorks(),
		kubeInformers, c.addonInformers, c.clusterInformers, c.dynamicInformers)
	if err != nil {
		return err
	}

	kubeInformers.Start(ctx.Done())

	return nil
}
