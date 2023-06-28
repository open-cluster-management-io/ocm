package addontemplate

import (
	"context"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/dynamic/dynamicinformer"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"
	clusterv1informers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	workv1informers "open-cluster-management.io/api/client/work/informers/externalversions"

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

	stopFunc, ok := c.addonManagers[addOnName]
	if ok {
		stopFunc()
		klog.Infof("Stop the manager for addon %s", addOnName)
	}
}

func (c *addonTemplateController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	key := syncCtx.QueueKey()
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

	if !templateagent.SupportAddOnTemplate(cma) {
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
		err := c.runControllerFunc(ctx, addonName)
		if err != nil {
			klog.Errorf("run controller for addon %s error: %v", addonName, err)
			utilruntime.HandleError(err)
		}

		// use the parent context to start all shared informers, otherwise once the context is cancelled,
		// the informers will stop and all other shared go routines will be impacted.
		c.workInformers.Start(pctx.Done())
		c.addonInformers.Start(pctx.Done())
		c.clusterInformers.Start(pctx.Done())
		c.dynamicInformers.Start(pctx.Done())

		<-ctx.Done()
		klog.Infof("Addon %s Manager stopped", addonName)
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

	agentAddon := templateagent.NewCRDTemplateAgentAddon(
		addonName,
		// TODO: agentName should not be changed after restarting the agent
		utilrand.String(5),
		c.kubeClient,
		c.addonClient,
		c.addonInformers,
		kubeInformers.Rbac().V1().RoleBindings().Lister(),
		addonfactory.GetAddOnDeploymentConfigValues(
			addonfactory.NewAddOnDeploymentConfigGetter(c.addonClient),
			addonfactory.ToAddOnCustomizedVariableValues,
			templateagent.ToAddOnNodePlacementPrivateValues,
			templateagent.ToAddOnRegistriesPrivateValues,
		),
	)
	err = mgr.AddAgent(agentAddon)
	if err != nil {
		return err
	}

	err = mgr.StartWithInformers(ctx, kubeInformers, c.workInformers, c.addonInformers, c.clusterInformers, c.dynamicInformers)
	if err != nil {
		return err
	}

	kubeInformers.Start(ctx.Done())

	return nil
}
