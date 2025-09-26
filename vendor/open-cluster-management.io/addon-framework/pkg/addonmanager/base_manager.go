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

// BaseAddonManagerImpl is the base implementation of BaseAddonManager
// that manages the addon agents and configs.
type BaseAddonManagerImpl struct {
	addonAgents  map[string]agent.AgentAddon
	addonConfigs map[schema.GroupVersionResource]bool
	config       *rest.Config
	syncContexts []factory.SyncContext
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
	dynamicInformers dynamicinformer.DynamicSharedInformerFactory) error {

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
	)

	registrationController := registration.NewAddonRegistrationController(
		addonClient,
		clusterInformers.Cluster().V1().ManagedClusters(),
		addonInformers.Addon().V1alpha1().ManagedClusterAddOns(),
		a.addonAgents,
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
		)
		csrSignController = certificate.NewCSRSignController(
			kubeClient,
			clusterInformers.Cluster().V1().ManagedClusters(),
			kubeInformers.Certificates().V1().CertificateSigningRequests(),
			addonInformers.Addon().V1alpha1().ManagedClusterAddOns(),
			a.addonAgents,
		)
	} else if v1beta1Supported {
		csrApproveController = certificate.NewCSRApprovingController(
			kubeClient,
			clusterInformers.Cluster().V1().ManagedClusters(),
			nil,
			kubeInformers.Certificates().V1beta1().CertificateSigningRequests(),
			addonInformers.Addon().V1alpha1().ManagedClusterAddOns(),
			a.addonAgents,
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
