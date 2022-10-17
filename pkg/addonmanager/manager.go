package addonmanager

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/constants"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/controllers/addonconfig"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/controllers/addonhealthcheck"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/controllers/addoninstall"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/controllers/agentdeploy"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/controllers/certificate"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/controllers/clustermanagement"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/controllers/registration"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/basecontroller/factory"
	"open-cluster-management.io/addon-framework/pkg/utils"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	clusterv1client "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterv1informers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	workv1client "open-cluster-management.io/api/client/work/clientset/versioned"
	workv1informers "open-cluster-management.io/api/client/work/informers/externalversions"
)

// AddonManager is the interface to initialize a manager on hub to manage the addon
// agents on all managedcluster
type AddonManager interface {
	// AddAgent register an addon agent to the manager.
	AddAgent(addon agent.AgentAddon) error

	// Trigger triggers a reconcile loop in the manager. Currently it
	// only trigger the deploy controller.
	Trigger(clusterName, addonName string)

	// Start starts all registered addon agent.
	Start(ctx context.Context) error
}

type addonManager struct {
	addonAgents  map[string]agent.AgentAddon
	addonConfigs map[schema.GroupVersionResource]bool
	config       *rest.Config
	syncContexts []factory.SyncContext
}

func (a *addonManager) AddAgent(addon agent.AgentAddon) error {
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

func (a *addonManager) Trigger(clusterName, addonName string) {
	for _, syncContex := range a.syncContexts {
		syncContex.Queue().Add(fmt.Sprintf("%s/%s", clusterName, addonName))
	}
}

func (a *addonManager) Start(ctx context.Context) error {
	dynamicClient, err := dynamic.NewForConfig(a.config)
	if err != nil {
		return err
	}

	kubeClient, err := kubernetes.NewForConfig(a.config)
	if err != nil {
		return err
	}

	addonClient, err := addonv1alpha1client.NewForConfig(a.config)
	if err != nil {
		return err
	}

	clusterClient, err := clusterv1client.NewForConfig(a.config)
	if err != nil {
		return err
	}

	workClient, err := workv1client.NewForConfig(a.config)
	if err != nil {
		return err
	}

	v1CSRSupported, v1beta1Supported, err := utils.IsCSRSupported(kubeClient)
	if err != nil {
		return err
	}

	addonNames := []string{}
	for key, agent := range a.addonAgents {
		addonNames = append(addonNames, key)
		for _, configGVR := range agent.GetAgentAddonOptions().SupportedConfigGVRs {
			a.addonConfigs[configGVR] = true
		}
	}
	addonInformers := addoninformers.NewSharedInformerFactory(addonClient, 10*time.Minute)
	workInformers := workv1informers.NewSharedInformerFactoryWithOptions(workClient, 10*time.Minute,
		workv1informers.WithTweakListOptions(func(listOptions *metav1.ListOptions) {
			selector := &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      constants.AddonLabel,
						Operator: metav1.LabelSelectorOpIn,
						Values:   addonNames,
					},
				},
			}
			listOptions.LabelSelector = metav1.FormatLabelSelector(selector)
		}),
	)
	clusterInformers := clusterv1informers.NewSharedInformerFactory(clusterClient, 10*time.Minute)
	kubeInfomers := kubeinformers.NewSharedInformerFactoryWithOptions(kubeClient, 10*time.Minute,
		kubeinformers.WithTweakListOptions(func(listOptions *metav1.ListOptions) {
			selector := &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      constants.AddonLabel,
						Operator: metav1.LabelSelectorOpIn,
						Values:   addonNames,
					},
				},
			}
			listOptions.LabelSelector = metav1.FormatLabelSelector(selector)
		}),
	)
	dynamicInformers := dynamicinformer.NewDynamicSharedInformerFactory(dynamicClient, 10*time.Minute)

	deployController := agentdeploy.NewAddonDeployController(
		workClient,
		addonClient,
		clusterInformers.Cluster().V1().ManagedClusters(),
		addonInformers.Addon().V1alpha1().ManagedClusterAddOns(),
		workInformers.Work().V1().ManifestWorks(),
		a.addonAgents,
	)

	registrationController := registration.NewAddonConfigurationController(
		addonClient,
		clusterInformers.Cluster().V1().ManagedClusters(),
		addonInformers.Addon().V1alpha1().ManagedClusterAddOns(),
		a.addonAgents,
	)

	clusterManagementController := clustermanagement.NewClusterManagementController(
		addonClient,
		clusterInformers.Cluster().V1().ManagedClusters(),
		addonInformers.Addon().V1alpha1().ManagedClusterAddOns(),
		addonInformers.Addon().V1alpha1().ClusterManagementAddOns(),
		a.addonAgents,
	)

	addonInstallController := addoninstall.NewAddonInstallController(
		addonClient,
		clusterInformers.Cluster().V1().ManagedClusters(),
		addonInformers.Addon().V1alpha1().ManagedClusterAddOns(),
		a.addonAgents,
	)

	addonHealthCheckController := addonhealthcheck.NewAddonHealthCheckController(
		addonClient,
		addonInformers.Addon().V1alpha1().ManagedClusterAddOns(),
		workInformers.Work().V1().ManifestWorks(),
		a.addonAgents,
	)

	var addonConfigController factory.Controller
	if len(a.addonConfigs) != 0 {
		addonConfigController = addonconfig.NewAddonConfigController(
			addonClient,
			addonInformers.Addon().V1alpha1().ManagedClusterAddOns(),
			dynamicInformers,
			a.addonConfigs,
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
			kubeInfomers.Certificates().V1().CertificateSigningRequests(),
			nil,
			addonInformers.Addon().V1alpha1().ManagedClusterAddOns(),
			a.addonAgents,
		)
		csrSignController = certificate.NewCSRSignController(
			kubeClient,
			clusterInformers.Cluster().V1().ManagedClusters(),
			kubeInfomers.Certificates().V1().CertificateSigningRequests(),
			addonInformers.Addon().V1alpha1().ManagedClusterAddOns(),
			a.addonAgents,
		)
	} else if v1beta1Supported {
		csrApproveController = certificate.NewCSRApprovingController(
			kubeClient,
			clusterInformers.Cluster().V1().ManagedClusters(),
			nil,
			kubeInfomers.Certificates().V1beta1().CertificateSigningRequests(),
			addonInformers.Addon().V1alpha1().ManagedClusterAddOns(),
			a.addonAgents,
		)
	}

	a.syncContexts = append(a.syncContexts, deployController.SyncContext())

	go addonInformers.Start(ctx.Done())
	go workInformers.Start(ctx.Done())
	go clusterInformers.Start(ctx.Done())
	go kubeInfomers.Start(ctx.Done())
	go dynamicInformers.Start(ctx.Done())

	go deployController.Run(ctx, 1)
	go registrationController.Run(ctx, 1)
	go clusterManagementController.Run(ctx, 1)
	go addonInstallController.Run(ctx, 1)
	go addonHealthCheckController.Run(ctx, 1)
	if addonConfigController != nil {
		go addonConfigController.Run(ctx, 1)
	}
	if csrApproveController != nil {
		go csrApproveController.Run(ctx, 1)
	}
	if csrSignController != nil {
		go csrSignController.Run(ctx, 1)
	}
	return nil
}

// New returns a new Manager for creating addon agents.
func New(config *rest.Config) (AddonManager, error) {
	return &addonManager{
		config:       config,
		syncContexts: []factory.SyncContext{},
		addonConfigs: map[schema.GroupVersionResource]bool{},
		addonAgents:  map[string]agent.AgentAddon{},
	}, nil
}
