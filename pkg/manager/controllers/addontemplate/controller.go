package addontemplate

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
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
	// templateAddonManagers holds all addon managers that will be deployed with addon template.
	// every addon template in-used in the cluster, in other words, a manager will be started
	// for the template recorded in manageClusterAddon status configReferences desiredConfigSpecHash
	// or lastAppliedConfigSpecHash
	templateAddonManagers map[string]context.CancelFunc

	kubeConfig                *rest.Config
	addonClient               addonv1alpha1client.Interface
	hubKubeClient             kubernetes.Interface
	managedClusterAddOnLister addonlisterv1alpha1.ManagedClusterAddOnLister
}

// NewAddonTemplateController returns an instance of addonTemplateController
func NewAddonTemplateController(
	hubKubeconfig *rest.Config,
	hubKubeClient kubernetes.Interface,
	addonClient addonv1alpha1client.Interface,
	managedClusterAddOnInformer addoninformerv1alpha1.ManagedClusterAddOnInformer,
) factory.Controller {
	c := &addonTemplateController{
		managedClusterAddOnLister: managedClusterAddOnInformer.Lister(),
		templateAddonManagers:     make(map[string]context.CancelFunc),
		kubeConfig:                hubKubeconfig,
		addonClient:               addonClient,
		hubKubeClient:             hubKubeClient,
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
		managedClusterAddOnInformer.Informer()).
		WithSync(c.sync).
		ResyncEvery(10 * time.Minute).
		ToController("addon-template-controller")
}

func (c *addonTemplateController) stopUnusedManagers(
	ctx context.Context, syncCtx factory.SyncContext, addOnName string) error {

	// TODO: ADD an index to record the relationship between
	// the addon template spec hash and the managed cluster addon name

	return nil
}

func (c *addonTemplateController) sync(ctx context.Context, syncCtx factory.SyncContext, key string) error {
	if key == factory.DefaultQueueKey {
		return c.stopUnusedManagers(ctx, syncCtx, key)
	}

	clusterName, addonName, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		// ignore addon whose key is not in format: namespace/name
		return nil
	}

	mca, err := c.managedClusterAddOnLister.ManagedClusterAddOns(clusterName).Get(addonName)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}

	ok, templateRef := c.addonTemplateConfigRef(mca.Status.ConfigReferences)
	if !ok {
		return nil
	}

	tdc := templateRef.DesiredConfig
	if tdc == nil || tdc.SpecHash == "" {
		klog.Infof("Addon %s template spec hash is empty", addonName)
		return nil
	}

	_, exist := c.templateAddonManagers[tdc.SpecHash]
	if exist {
		klog.Infof("There already is a manager started for addon %s, template %s hash %s, skip.",
			addonName, tdc.Name, tdc.SpecHash)
		return nil
	}

	klog.Infof("Start an addon manager for addon %s, template %s spec hash %s",
		addonName, tdc.Name, tdc.SpecHash)

	template, err := c.addonClient.AddonV1alpha1().AddOnTemplates().Get(
		context.TODO(), tdc.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	currentTemplateSpecHash, err := hashTemplateSpec(template)
	if err != nil {
		return err
	}
	if currentTemplateSpecHash != tdc.SpecHash {
		return fmt.Errorf("template %s spec changed, current spec hash: %s, desired spec hash: %s",
			tdc.Name, currentTemplateSpecHash, tdc.SpecHash)
	}

	stopFunc := c.startManager(ctx, addonName, tdc.SpecHash, *template)
	c.templateAddonManagers[tdc.SpecHash] = stopFunc
	return nil
}

func hashTemplateSpec(template *addonapiv1alpha1.AddOnTemplate) (string, error) {
	unstructuredTemplate, err := runtime.DefaultUnstructuredConverter.ToUnstructured(template)
	if err != nil {
		return "", err
	}
	specHash, err := utils.GetSpecHash(&unstructured.Unstructured{
		Object: unstructuredTemplate,
	})
	if err != nil {
		return specHash, err
	}
	return specHash, nil
}

func (c *addonTemplateController) startManager(
	pctx context.Context,
	addonName, templateSpecHash string,
	template addonapiv1alpha1.AddOnTemplate) context.CancelFunc {
	ctx, stopFunc := context.WithCancel(pctx)
	go func() {
		err := c.runController(ctx, addonName, templateSpecHash, template)
		if err != nil {
			klog.Errorf("run controller for addon %s template %s error: %v", addonName, templateSpecHash, err)
			utilruntime.HandleError(err)
		}
	}()
	return stopFunc
}

// addonTemplateConfigRef return the first addon template config
func (a *addonTemplateController) addonTemplateConfigRef(
	configReferences []addonapiv1alpha1.ConfigReference) (bool, addonapiv1alpha1.ConfigReference) {
	for _, config := range configReferences {
		if config.Group == utils.AddOnTemplateGVR.Group && config.Resource == utils.AddOnTemplateGVR.Resource {
			return true, config
		}
	}
	return false, addonapiv1alpha1.ConfigReference{}
}

func (c *addonTemplateController) runController(
	ctx context.Context, addonName, templateSpecHash string, template addonapiv1alpha1.AddOnTemplate) error {
	mgr, err := addonmanager.New(c.kubeConfig)
	if err != nil {
		return err
	}

	agentAddon := addonfactory.NewCRDTemplateAgentAddon(
		c.hubKubeClient,
		c.addonClient,
		template.Spec,
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

	klog.Infof("Addon %s Manager for template %s stopped", addonName, templateSpecHash)
	return nil
}
