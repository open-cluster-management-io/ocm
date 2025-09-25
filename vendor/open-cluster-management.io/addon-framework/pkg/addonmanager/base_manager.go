package addonmanager

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/dynamicinformer"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	clusterv1informers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	workv1informers "open-cluster-management.io/api/client/work/informers/externalversions/work/v1"

	"open-cluster-management.io/addon-framework/pkg/addonmanager/controllers/addonconfig"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/controllers/agentdeploy"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/controllers/certificate"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/controllers/cmaconfig"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/controllers/cmamanagedby"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/controllers/registration"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/utils"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"
)

// Option contains configuration options for BaseAddonManagerImpl.
type Option struct {
	// TemplateBasedAddOn configures whether the manager is handling template-based addons.
	//   - true: all ManagedClusterAddOn controllers except "addon-config-controller" will only process addons
	//     when the referenced AddOnTemplate resources in their status.configReferences are properly set;
	//     the "addon-config-controller" is responsible for setting these values
	//   - false: process all addons without waiting for template configuration
	//
	// This prevents premature processing of template-based addons before their configurations
	// are fully ready, avoiding unnecessary errors and retries.
	// See https://github.com/open-cluster-management-io/ocm/issues/1181 for more context.
	TemplateBasedAddOn bool
}

// OptionFunc is a function that modifies Option.
type OptionFunc func(*Option)

// WithTemplateMode returns an OptionFunc that sets the template mode.
func WithTemplateMode(enabled bool) OptionFunc {
	return func(option *Option) {
		option.TemplateBasedAddOn = enabled
	}
}

// WithOption returns an OptionFunc that applies the given Option struct.
func WithOption(opt *Option) OptionFunc {
	return func(option *Option) {
		if opt != nil {
			*option = *opt
		}
	}
}

// BaseAddonManagerImpl is the base implementation of BaseAddonManager
// that manages the addon agents and configs.
type BaseAddonManagerImpl struct {
	addonAgents        map[string]agent.AgentAddon
	addonConfigs       map[schema.GroupVersionResource]bool
	config             *rest.Config
	syncContexts       []factory.SyncContext
	templateBasedAddOn bool
}

// NewBaseAddonManagerImpl creates a new BaseAddonManagerImpl instance with the given config.
func NewBaseAddonManagerImpl(config *rest.Config) *BaseAddonManagerImpl {
	return &BaseAddonManagerImpl{
		config:       config,
		syncContexts: []factory.SyncContext{},
		addonConfigs: map[schema.GroupVersionResource]bool{},
		addonAgents:  map[string]agent.AgentAddon{},
	}
}

// ApplyOptionFuncs applies OptionFunc functions to create and configure options.
func (a *BaseAddonManagerImpl) ApplyOptionFuncs(optionFuncs ...OptionFunc) {
	option := &Option{}
	for _, fn := range optionFuncs {
		fn(option)
	}
	a.templateBasedAddOn = option.TemplateBasedAddOn
}

func (a *BaseAddonManagerImpl) GetConfig() *rest.Config {
	return a.config
}

func (a *BaseAddonManagerImpl) GetAddonAgents() map[string]agent.AgentAddon {
	return a.addonAgents
}

func (a *BaseAddonManagerImpl) AddAgent(addon agent.AgentAddon) error {
	addonOption := addon.GetAgentAddonOptions()
	if len(addonOption.AddonName) == 0 {
		return fmt.Errorf("addon name should be set")
	}
	if _, ok := a.addonAgents[addonOption.AddonName]; ok {
		return fmt.Errorf("an agent is added for the addon already")
	}
	a.addonAgents[addonOption.AddonName] = addon
	return nil
}

func (a *BaseAddonManagerImpl) Trigger(clusterName, addonName string) {
	for _, syncContex := range a.syncContexts {
		syncContex.Queue().Add(fmt.Sprintf("%s/%s", clusterName, addonName))
	}
}

func (a *BaseAddonManagerImpl) StartWithInformers(ctx context.Context,
	workClient workclientset.Interface,
	workInformers workv1informers.ManifestWorkInformer,
	kubeInformers kubeinformers.SharedInformerFactory,
	addonInformers addoninformers.SharedInformerFactory,
	clusterInformers clusterv1informers.SharedInformerFactory,
	dynamicInformers dynamicinformer.DynamicSharedInformerFactory,
) error {
	// Determine the appropriate filter function based on templateBasedAddOn field
	mcaFilterFunc := utils.AllowAllAddOns
	if a.templateBasedAddOn {
		mcaFilterFunc = utils.FilterTemplateBasedAddOns
	}

	kubeClient, err := kubernetes.NewForConfig(a.config)
	if err != nil {
		return err
	}

	addonClient, err := addonv1alpha1client.NewForConfig(a.config)
	if err != nil {
		return err
	}

	v1CSRSupported, v1beta1Supported, err := utils.IsCSRSupported(kubeClient)
	if err != nil {
		return err
	}

	for _, agentImpl := range a.addonAgents {
		for _, configGVR := range agentImpl.GetAgentAddonOptions().SupportedConfigGVRs {
			a.addonConfigs[configGVR] = true
		}
	}

	deployController := agentdeploy.NewAddonDeployController(
		workClient,
		addonClient,
		clusterInformers.Cluster().V1().ManagedClusters(),
		addonInformers.Addon().V1alpha1().ManagedClusterAddOns(),
		workInformers,
		a.addonAgents,
		mcaFilterFunc,
	)

	registrationController := registration.NewAddonRegistrationController(
		addonClient,
		clusterInformers.Cluster().V1().ManagedClusters(),
		addonInformers.Addon().V1alpha1().ManagedClusterAddOns(),
		a.addonAgents,
		mcaFilterFunc,
	)

	// This controller is used during migrating addons to be managed by addon-manager.
	// This should be removed when the migration is done.
	// The migration plan refer to https://github.com/open-cluster-management-io/ocm/issues/355.
	managementAddonController := cmamanagedby.NewCMAManagedByController(
		addonClient,
		addonInformers.Addon().V1alpha1().ClusterManagementAddOns(),
		a.addonAgents,
		utils.FilterByAddonName(a.addonAgents),
	)

	var addonConfigController, managementAddonConfigController factory.Controller
	if len(a.addonConfigs) != 0 {
		// ManagedClusterAddOn filter is intentionally disabled for the addon-config-controller.
		// This is because template-based addons require this controller to set the specHash in
		// managedclusteraddon.status.configReferences for addontemplates. Without this, all other
		// ManagedClusterAddOn controllers would wait indefinitely for the template configurations
		// to be applied.
		// Consider moving the logic of setting managedclusteraddon.status.configReferences
		// for addontemplates to the ocm addon-manager.
		addonConfigController = addonconfig.NewAddonConfigController(
			addonClient,
			addonInformers.Addon().V1alpha1().ManagedClusterAddOns(),
			addonInformers.Addon().V1alpha1().ClusterManagementAddOns(),
			dynamicInformers,
			a.addonConfigs,
			utils.FilterByAddonName(a.addonAgents),
		)
		managementAddonConfigController = cmaconfig.NewCMAConfigController(
			addonClient,
			addonInformers.Addon().V1alpha1().ClusterManagementAddOns(),
			dynamicInformers,
			a.addonConfigs,
			utils.FilterByAddonName(a.addonAgents),
		)
	}

	var csrApproveController factory.Controller
	var csrSignController factory.Controller
	// Spawn the following controllers only if v1 CSR api is supported in the
	// hub cluster. Under v1beta1 CSR api, all the CSR objects will be signed
	// by the kube-controller-manager so custom CSR controller should be
	// disabled to avoid conflict.
	if v1CSRSupported {
		csrApproveController = certificate.NewCSRApprovingController(
			kubeClient,
			clusterInformers.Cluster().V1().ManagedClusters(),
			kubeInformers.Certificates().V1().CertificateSigningRequests(),
			nil,
			addonInformers.Addon().V1alpha1().ManagedClusterAddOns(),
			a.addonAgents,
			mcaFilterFunc,
		)
		csrSignController = certificate.NewCSRSignController(
			kubeClient,
			clusterInformers.Cluster().V1().ManagedClusters(),
			kubeInformers.Certificates().V1().CertificateSigningRequests(),
			addonInformers.Addon().V1alpha1().ManagedClusterAddOns(),
			a.addonAgents,
			mcaFilterFunc,
		)
	} else if v1beta1Supported {
		csrApproveController = certificate.NewCSRApprovingController(
			kubeClient,
			clusterInformers.Cluster().V1().ManagedClusters(),
			nil,
			kubeInformers.Certificates().V1beta1().CertificateSigningRequests(),
			addonInformers.Addon().V1alpha1().ManagedClusterAddOns(),
			a.addonAgents,
			mcaFilterFunc,
		)
	}

	a.syncContexts = append(a.syncContexts,
		deployController.SyncContext(), registrationController.SyncContext())

	go deployController.Run(ctx, 1)
	go registrationController.Run(ctx, 1)
	go managementAddonController.Run(ctx, 1)

	if addonConfigController != nil {
		go addonConfigController.Run(ctx, 1)
	}
	if managementAddonConfigController != nil {
		go managementAddonConfigController.Run(ctx, 1)
	}
	if csrApproveController != nil {
		go csrApproveController.Run(ctx, 1)
	}
	if csrSignController != nil {
		go csrSignController.Run(ctx, 1)
	}
	return nil
}
