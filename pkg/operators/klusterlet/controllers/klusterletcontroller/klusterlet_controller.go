package klusterletcontroller

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/version"
	"k8s.io/client-go/dynamic"
	appsinformer "k8s.io/client-go/informers/apps/v1"
	coreinformer "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	coreclientv1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	"github.com/openshift/library-go/pkg/assets"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	operatorhelpers "github.com/openshift/library-go/pkg/operator/v1helpers"

	operatorv1client "open-cluster-management.io/api/client/operator/clientset/versioned/typed/operator/v1"
	operatorinformer "open-cluster-management.io/api/client/operator/informers/externalversions/operator/v1"
	operatorlister "open-cluster-management.io/api/client/operator/listers/operator/v1"
	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	workv1client "open-cluster-management.io/api/client/work/clientset/versioned/typed/work/v1"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"
	"open-cluster-management.io/registration-operator/manifests"
	"open-cluster-management.io/registration-operator/pkg/helpers"
)

const (

	// klusterletHostedFinalizer is used to clean up resources on the managed/hosted cluster in Hosted mode
	klusterletHostedFinalizer    = "operator.open-cluster-management.io/klusterlet-hosted-cleanup"
	klusterletFinalizer          = "operator.open-cluster-management.io/klusterlet-cleanup"
	imagePullSecret              = "open-cluster-management-image-pull-credentials"
	klusterletApplied            = "Applied"
	klusterletReadyToApply       = "ReadyToApply"
	hubConnectionDegraded        = "HubConnectionDegraded"
	hubKubeConfigSecretMissing   = "HubKubeConfigSecretMissing"
	appliedManifestWorkFinalizer = "cluster.open-cluster-management.io/applied-manifest-work-cleanup"

	spokeRegistrationFeatureGatesInvalid = "InvalidRegistrationFeatureGates"
	spokeRegistrationFeatureGatesValid   = "ValidRegistrationFeatureGates"
)

var (
	crdV1StaticFiles = []string{
		"klusterlet/managed/0000_01_work.open-cluster-management.io_appliedmanifestworks.crd.yaml",
		"klusterlet/managed/0000_02_clusters.open-cluster-management.io_clusterclaims.crd.yaml",
	}

	crdV1beta1StaticFiles = []string{
		"klusterlet/managed/0001_01_work.open-cluster-management.io_appliedmanifestworks.crd.yaml",
		"klusterlet/managed/0001_02_clusters.open-cluster-management.io_clusterclaims.crd.yaml",
	}

	managedStaticResourceFiles = []string{
		"klusterlet/managed/klusterlet-registration-serviceaccount.yaml",
		"klusterlet/managed/klusterlet-registration-clusterrole.yaml",
		"klusterlet/managed/klusterlet-registration-clusterrole-addon-management.yaml",
		"klusterlet/managed/klusterlet-registration-clusterrolebinding.yaml",
		"klusterlet/managed/klusterlet-registration-clusterrolebinding-addon-management.yaml",
		"klusterlet/managed/klusterlet-work-serviceaccount.yaml",
		"klusterlet/managed/klusterlet-work-clusterrole.yaml",
		"klusterlet/managed/klusterlet-work-clusterrole-execution.yaml",
		"klusterlet/managed/klusterlet-work-clusterrolebinding.yaml",
		"klusterlet/managed/klusterlet-work-clusterrolebinding-execution.yaml",
		"klusterlet/managed/klusterlet-work-clusterrolebinding-execution-admin.yaml",
	}

	managementStaticResourceFiles = []string{
		"klusterlet/management/klusterlet-role-extension-apiserver.yaml",
		"klusterlet/management/klusterlet-registration-serviceaccount.yaml",
		"klusterlet/management/klusterlet-registration-role.yaml",
		"klusterlet/management/klusterlet-registration-rolebinding.yaml",
		"klusterlet/management/klusterlet-registration-rolebinding-extension-apiserver.yaml",
		"klusterlet/management/klusterlet-registration-clusterrole-addon-management.yaml",
		"klusterlet/management/klusterlet-registration-clusterrolebinding-addon-management.yaml",
		"klusterlet/management/klusterlet-work-serviceaccount.yaml",
		"klusterlet/management/klusterlet-work-role.yaml",
		"klusterlet/management/klusterlet-work-rolebinding.yaml",
		"klusterlet/management/klusterlet-work-rolebinding-extension-apiserver.yaml",
	}

	kube111StaticResourceFiles = []string{
		"klusterletkube111/klusterlet-registration-operator-clusterrolebinding.yaml",
		"klusterletkube111/klusterlet-work-clusterrolebinding.yaml",
	}
)

type klusterletController struct {
	klusterletClient          operatorv1client.KlusterletInterface
	klusterletLister          operatorlister.KlusterletLister
	kubeClient                kubernetes.Interface
	apiExtensionClient        apiextensionsclient.Interface
	dynamicClient             dynamic.Interface
	appliedManifestWorkClient workv1client.AppliedManifestWorkInterface
	kubeVersion               *version.Version
	operatorNamespace         string
	skipHubSecretPlaceholder  bool
	cache                     resourceapply.ResourceCache

	// buildManagedClusterClientsHostedMode build clients for the managed cluster in hosted mode,
	// this can be overridden for testing
	buildManagedClusterClientsHostedMode func(
		ctx context.Context,
		kubeClient kubernetes.Interface,
		namespace, secret string) (*managedClusterClients, error)
}

// NewKlusterletController construct klusterlet controller
func NewKlusterletController(
	kubeClient kubernetes.Interface,
	apiExtensionClient apiextensionsclient.Interface,
	dynamicClient dynamic.Interface,
	klusterletClient operatorv1client.KlusterletInterface,
	klusterletInformer operatorinformer.KlusterletInformer,
	secretInformer coreinformer.SecretInformer,
	deploymentInformer appsinformer.DeploymentInformer,
	appliedManifestWorkClient workv1client.AppliedManifestWorkInterface,
	kubeVersion *version.Version,
	operatorNamespace string,
	recorder events.Recorder,
	skipHubSecretPlaceholder bool) factory.Controller {
	controller := &klusterletController{
		kubeClient:                           kubeClient,
		apiExtensionClient:                   apiExtensionClient,
		dynamicClient:                        dynamicClient,
		klusterletClient:                     klusterletClient,
		klusterletLister:                     klusterletInformer.Lister(),
		appliedManifestWorkClient:            appliedManifestWorkClient,
		kubeVersion:                          kubeVersion,
		operatorNamespace:                    operatorNamespace,
		buildManagedClusterClientsHostedMode: buildManagedClusterClientsFromSecret,
		skipHubSecretPlaceholder:             skipHubSecretPlaceholder,
		cache:                                resourceapply.NewResourceCache(),
	}

	return factory.New().WithSync(controller.sync).
		WithInformersQueueKeyFunc(helpers.KlusterletSecretQueueKeyFunc(controller.klusterletLister), secretInformer.Informer()).
		WithInformersQueueKeyFunc(helpers.KlusterletDeploymentQueueKeyFunc(controller.klusterletLister), deploymentInformer.Informer()).
		WithInformersQueueKeyFunc(func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			return accessor.GetName()
		}, klusterletInformer.Informer()).
		ToController("KlusterletController", recorder)
}

// klusterletConfig is used to render the template of hub manifests
type klusterletConfig struct {
	KlusterletName string
	// KlusterletNamespace is the namespace created on the managed cluster for each
	// klusterlet.
	// 1). In the Default mode, it refers to the same namespace as AgentNamespace;
	// 2). In the Hosted mode, the namespace still exists and contains some necessary
	//     resources for agents, like service accounts, roles and rolebindings.
	KlusterletNamespace string
	// AgentNamespace is the namespace to deploy the agents.
	// 1). In the Default mode, it is on the managed cluster and refers to the same
	//     namespace as KlusterletNamespace;
	// 2). In the Hosted mode, it is on the management cluster and has the same name as
	//     the klusterlet.
	AgentNamespace            string
	AgentID                   string
	RegistrationImage         string
	WorkImage                 string
	ClusterName               string
	ExternalServerURL         string
	HubKubeConfigSecret       string
	BootStrapKubeConfigSecret string
	OperatorNamespace         string
	Replica                   int32

	ExternalManagedKubeConfigSecret             string
	ExternalManagedKubeConfigRegistrationSecret string
	ExternalManagedKubeConfigWorkSecret         string
	InstallMode                                 operatorapiv1.InstallMode

	RegistrationFeatureGates []string

	HubApiServerHostAlias *operatorapiv1.HubApiServerHostAlias
}

// managedClusterClients holds variety of kube client for managed cluster
type managedClusterClients struct {
	kubeClient                kubernetes.Interface
	apiExtensionClient        apiextensionsclient.Interface
	appliedManifestWorkClient workv1client.AppliedManifestWorkInterface
	dynamicClient             dynamic.Interface
	// Only used for Hosted mode to generate managed cluster kubeconfig
	// with minimum permission for registration and work.
	kubeconfig *rest.Config
}

func (n *klusterletController) sync(ctx context.Context, controllerContext factory.SyncContext) error {
	klusterletName := controllerContext.QueueKey()
	klog.V(4).Infof("Reconciling Klusterlet %q", klusterletName)
	klusterlet, err := n.klusterletLister.Get(klusterletName)
	if errors.IsNotFound(err) {
		// Klusterlet not found, could have been deleted, do nothing.
		return nil
	}
	if err != nil {
		return err
	}
	klusterlet = klusterlet.DeepCopy()

	config := klusterletConfig{
		KlusterletName:            klusterlet.Name,
		KlusterletNamespace:       helpers.KlusterletNamespace(klusterlet),
		AgentNamespace:            helpers.AgentNamespace(klusterlet),
		AgentID:                   string(klusterlet.UID),
		RegistrationImage:         klusterlet.Spec.RegistrationImagePullSpec,
		WorkImage:                 klusterlet.Spec.WorkImagePullSpec,
		ClusterName:               klusterlet.Spec.ClusterName,
		BootStrapKubeConfigSecret: helpers.BootstrapHubKubeConfig,
		HubKubeConfigSecret:       helpers.HubKubeConfig,
		ExternalServerURL:         getServersFromKlusterlet(klusterlet),
		OperatorNamespace:         n.operatorNamespace,
		Replica:                   helpers.DetermineReplica(ctx, n.kubeClient, klusterlet.Spec.DeployOption.Mode, n.kubeVersion),

		ExternalManagedKubeConfigSecret:             helpers.ExternalManagedKubeConfig,
		ExternalManagedKubeConfigRegistrationSecret: helpers.ExternalManagedKubeConfigRegistration,
		ExternalManagedKubeConfigWorkSecret:         helpers.ExternalManagedKubeConfigWork,
		InstallMode:                                 klusterlet.Spec.DeployOption.Mode,
		HubApiServerHostAlias:                       klusterlet.Spec.HubApiServerHostAlias,
	}

	managedClusterClients := &managedClusterClients{
		kubeClient:                n.kubeClient,
		apiExtensionClient:        n.apiExtensionClient,
		dynamicClient:             n.dynamicClient,
		appliedManifestWorkClient: n.appliedManifestWorkClient,
	}

	// If there are some invalid feature gates of registration, will output condition `ValidRegistrationFeatureGates` False in Klusterlet.
	if klusterlet.Spec.RegistrationConfiguration != nil && len(klusterlet.Spec.RegistrationConfiguration.FeatureGates) > 0 {
		featureGateArgs, invalidFeatureGates := helpers.FeatureGatesArgs(
			klusterlet.Spec.RegistrationConfiguration.FeatureGates, helpers.ComponentSpokeRegistrationKey)
		if len(invalidFeatureGates) == 0 {
			config.RegistrationFeatureGates = featureGateArgs
			// TODO: after the 0.10.0 is released, change back to UpdateKlusterletConditionFn
			_, _, _ = helpers.UpdateKlusterletStatus(ctx, n.klusterletClient, klusterletName, helpers.ReplaceKlusterletConditionFn(
				spokeRegistrationFeatureGatesInvalid,
				metav1.Condition{
					Type: spokeRegistrationFeatureGatesValid, Status: metav1.ConditionTrue, Reason: "FeatureGatesAllValid",
					Message: "Registration feature gates of klusterlet are all valid",
				}))
		} else {
			invalidFGErr := fmt.Errorf("there are some invalid feature gates of registration: %v ", invalidFeatureGates)
			// TODO: after the 0.10.0 is released, change back to UpdateKlusterletConditionFn
			_, _, _ = helpers.UpdateKlusterletStatus(ctx, n.klusterletClient, klusterletName, helpers.ReplaceKlusterletConditionFn(
				spokeRegistrationFeatureGatesInvalid,
				metav1.Condition{
					Type: spokeRegistrationFeatureGatesValid, Status: metav1.ConditionFalse, Reason: "InvalidFeatureGatesExisting",
					Message: invalidFGErr.Error(),
				}))
			return invalidFGErr
		}
	}

	if config.InstallMode == operatorapiv1.InstallModeHosted {
		managedClusterClients, err = n.buildManagedClusterClientsHostedMode(ctx,
			n.kubeClient, config.AgentNamespace, config.ExternalManagedKubeConfigSecret)
		if err != nil {
			_, _, _ = helpers.UpdateKlusterletStatus(ctx, n.klusterletClient, klusterletName,
				helpers.UpdateKlusterletConditionFn(metav1.Condition{
					Type: klusterletReadyToApply, Status: metav1.ConditionFalse, Reason: "KlusterletPrepareFailed",
					Message: fmt.Sprintf("Failed to build managed cluster clients: %v", err),
				}))
			return err
		}

		_, updated, updateErr := helpers.UpdateKlusterletStatus(ctx, n.klusterletClient, klusterletName,
			helpers.UpdateKlusterletConditionFn(metav1.Condition{
				Type: klusterletReadyToApply, Status: metav1.ConditionTrue, Reason: "KlusterletPrepared",
				Message: "Klusterlet is ready to apply",
			}))
		if updated {
			return updateErr
		}
	}

	if !klusterlet.DeletionTimestamp.IsZero() {
		// The work of klusterlet cleanup will be handled by klusterlet cleanup controller
		return nil
	}

	// do nothing until finalizer is added.
	if !hasFinalizer(klusterlet, klusterletFinalizer) {
		return nil
	}

	if !readyToOperateManagedClusterResources(klusterlet, config.InstallMode) {
		// wait for the external managed kubeconfig to exist to apply resources on the manged cluster
		return nil
	}

	// Start deploy klusterlet components
	// Ensure the agent namespace
	err = n.ensureNamespace(ctx, n.kubeClient, klusterletName, config.AgentNamespace)
	if err != nil {
		return err
	}
	// Sync pull secret to the agent namespace
	err = n.syncPullSecret(ctx, n.kubeClient, n.kubeClient, klusterlet.Name, config.AgentNamespace, controllerContext.Recorder())
	if err != nil {
		return err
	}
	// For now, whether in Default or Hosted mode, the addons will be deployed on the managed cluster.
	// sync image pull secret from management cluster to managed cluster for addon namespace
	// TODO(zhujian7): In the future, we may consider deploy addons on the management cluster in Hosted mode.
	addonNamespace := fmt.Sprintf("%s-addon", config.KlusterletNamespace)
	// Ensure the addon namespace on the managed cluster
	err = n.ensureNamespace(ctx, managedClusterClients.kubeClient, klusterletName, addonNamespace)
	if err != nil {
		return err
	}
	// Sync pull secret to the klusterlet addon namespace
	// The reason we keep syncing secret instead of adding a label to trigger addonsecretcontroller to sync is:
	// addonsecretcontroller only watch namespaces in the same cluster klusterlet is running on.
	// And if addons are deployed in default mode on the managed cluster, but klusterlet is deployed in hosted on management cluster, then we still need to sync the secret here in klusterlet-controller using `managedClusterClients.kubeClient`.
	err = n.syncPullSecret(ctx, n.kubeClient, managedClusterClients.kubeClient, klusterlet.Name, addonNamespace, controllerContext.Recorder())
	if err != nil {
		return err
	}

	if config.InstallMode == operatorapiv1.InstallModeHosted {
		// In hosted mode, we should ensure the namespace on the managed cluster since
		// some resources(eg:service account) are still deployed on managed cluster.
		err := n.ensureNamespace(ctx, managedClusterClients.kubeClient, klusterletName, config.KlusterletNamespace)
		if err != nil {
			return err
		}
	}

	var relatedResources []operatorapiv1.RelatedResourceMeta
	// If kube version is less than 1.12, deploy static resource for kube 1.11 at first
	// TODO remove this when we do not support kube 1.11 any longer
	if cnt, err := n.kubeVersion.Compare("v1.12.0"); err == nil && cnt < 0 {
		statuses, err := n.applyStaticFiles(ctx, klusterletName, kube111StaticResourceFiles, &config, managedClusterClients.kubeClient,
			managedClusterClients.apiExtensionClient, managedClusterClients.dynamicClient, controllerContext.Recorder())
		if err != nil {
			return err
		}
		relatedResources = append(relatedResources, statuses...)
	}

	// Apply static files on managed cluster
	var appliedStaticFiles []string
	// CRD v1beta1 was deprecated from k8s 1.16.0 and will be removed in k8s 1.22
	if cnt, err := n.kubeVersion.Compare("v1.16.0"); err == nil && cnt < 0 {
		appliedStaticFiles = append(crdV1beta1StaticFiles, managedStaticResourceFiles...)
	} else {
		appliedStaticFiles = append(crdV1StaticFiles, managedStaticResourceFiles...)
	}
	statuses, err := n.applyStaticFiles(ctx, klusterletName, appliedStaticFiles, &config, managedClusterClients.kubeClient,
		managedClusterClients.apiExtensionClient, managedClusterClients.dynamicClient, controllerContext.Recorder())
	if err != nil {
		return err
	}
	relatedResources = append(relatedResources, statuses...)

	// Apply static files on management cluster
	// Apply role, rolebinding, service account for registration and work to the management cluster.
	statuses, err = n.applyStaticFiles(ctx, klusterletName, managementStaticResourceFiles, &config, n.kubeClient,
		n.apiExtensionClient, managedClusterClients.dynamicClient, controllerContext.Recorder())
	if err != nil {
		return err
	}
	relatedResources = append(relatedResources, statuses...)

	if config.InstallMode == operatorapiv1.InstallModeHosted {
		// Create managed config secret for registration and work.
		err = n.createManagedClusterKubeconfig(ctx, klusterletName, config.KlusterletNamespace, config.AgentNamespace, registrationServiceAccountName(klusterletName), config.ExternalManagedKubeConfigRegistrationSecret,
			managedClusterClients.kubeconfig, managedClusterClients.kubeClient, n.kubeClient.CoreV1(), controllerContext.Recorder())
		if err != nil {
			return err
		}
		err := n.createManagedClusterKubeconfig(ctx, klusterletName, config.KlusterletNamespace, config.AgentNamespace, workServiceAccountName(klusterletName), config.ExternalManagedKubeConfigWorkSecret,
			managedClusterClients.kubeconfig, managedClusterClients.kubeClient, n.kubeClient.CoreV1(), controllerContext.Recorder())
		if err != nil {
			return err
		}
	}
	// Deploy registration agent
	statuses, registrationGeneration, err := n.applyDeployment(ctx, klusterlet, &config, "klusterlet/management/klusterlet-registration-deployment.yaml", controllerContext.Recorder())
	if err != nil {
		return err
	}
	relatedResources = append(relatedResources, statuses...)

	// If cluster name is empty, read cluster name from hub config secret.
	// registration-agent generated the cluster name and set it into hub config secret.
	if config.ClusterName == "" {
		err = n.getClusterNameFromHubKubeConfigSecret(ctx, &config)
		if err != nil {
			return err
		}
	}
	// Deploy work agent.
	// * work agent is scaled to 0 only when degrade is true with the reason is HubKubeConfigSecretMissing.
	//   It is to ensure a fast startup of work agent when the klusterlet is bootstrapped at the first time.
	// * The work agent should not be scaled to 0 in degraded condition with other reasons,
	//   because we still need work agent running even though the hub kubconfig is missing some certain permission.
	//   It can ensure work agent to clean up the resources defined in manifestworks when cluster is detaching from the hub.
	workConfig := config
	hubConnectionDegradedCondition := meta.FindStatusCondition(klusterlet.Status.Conditions, hubConnectionDegraded)
	if hubConnectionDegradedCondition == nil {
		workConfig.Replica = 0
	} else if hubConnectionDegradedCondition.Status == metav1.ConditionTrue && strings.Contains(hubConnectionDegradedCondition.Reason, hubKubeConfigSecretMissing) {
		workConfig.Replica = 0
	}

	statuses, workGeneration, err := n.applyDeployment(ctx, klusterlet, &workConfig, "klusterlet/management/klusterlet-work-deployment.yaml", controllerContext.Recorder())
	if err != nil {
		return err
	}
	relatedResources = append(relatedResources, statuses...)

	// If we get here, we have successfully applied everything and should indicate that
	_, _, _ = helpers.UpdateKlusterletStatus(ctx, n.klusterletClient, klusterletName,
		helpers.UpdateKlusterletConditionFn(metav1.Condition{
			Type: klusterletApplied, Status: metav1.ConditionTrue, Reason: "KlusterletApplied",
			Message: "Klusterlet Component Applied"}),
		helpers.UpdateKlusterletGenerationsFn(registrationGeneration, workGeneration),
		helpers.UpdateKlusterletRelatedResourcesFn(relatedResources...),
		func(oldStatus *operatorapiv1.KlusterletStatus) error {
			oldStatus.ObservedGeneration = klusterlet.Generation
			return nil
		},
	)
	return nil
}

func readyToOperateManagedClusterResources(klusterlet *operatorapiv1.Klusterlet, mode operatorapiv1.InstallMode) bool {
	if mode != operatorapiv1.InstallModeHosted {
		return true
	}

	return meta.IsStatusConditionTrue(klusterlet.Status.Conditions, klusterletReadyToApply) && hasFinalizer(klusterlet, klusterletHostedFinalizer)
}

// getClusterNameFromHubKubeConfigSecret gets cluster name from hub kubeConfig secret
func (n *klusterletController) getClusterNameFromHubKubeConfigSecret(ctx context.Context, config *klusterletConfig) error {
	hubSecret, err := n.kubeClient.CoreV1().Secrets(config.AgentNamespace).Get(ctx, helpers.HubKubeConfig, metav1.GetOptions{})
	if err != nil {
		_, _, _ = helpers.UpdateKlusterletStatus(ctx, n.klusterletClient, config.KlusterletName, helpers.UpdateKlusterletConditionFn(metav1.Condition{
			Type: klusterletApplied, Status: metav1.ConditionFalse, Reason: "KlusterletApplyFailed",
			Message: fmt.Sprintf("Failed to get cluster name from hub kubeconfig secret with error %v", err),
		}))
		return err
	}

	clusterName := hubSecret.Data["cluster-name"]
	if len(clusterName) == 0 {
		err = fmt.Errorf("the cluster name in the secret is empty")
		_, _, _ = helpers.UpdateKlusterletStatus(ctx, n.klusterletClient, config.KlusterletName, helpers.UpdateKlusterletConditionFn(metav1.Condition{
			Type: klusterletApplied, Status: metav1.ConditionFalse, Reason: "KlusterletApplyFailed",
			Message: fmt.Sprintf("Failed to get cluster name from hub kubeconfig secret with error %v", err),
		}))
		return err
	}
	config.ClusterName = string(clusterName)
	return nil
}

// applyDeployment applies deployment on the management cluster
func (n *klusterletController) applyDeployment(ctx context.Context, klusterlet *operatorapiv1.Klusterlet, config *klusterletConfig, deploymentFile string, recorder events.Recorder) (
	[]operatorapiv1.RelatedResourceMeta, operatorapiv1.GenerationStatus, error) {
	var relatedResources []operatorapiv1.RelatedResourceMeta
	generationStatus, err := helpers.ApplyDeployment(
		ctx,
		n.kubeClient,
		klusterlet.Status.Generations,
		klusterlet.Spec.NodePlacement,
		func(name string) ([]byte, error) {
			template, err := manifests.KlusterletManifestFiles.ReadFile(name)
			if err != nil {
				return nil, err
			}
			objData := assets.MustCreateAssetFromTemplate(name, template, config).Data
			helpers.SetRelatedResourcesStatusesWithObj(&relatedResources, objData)
			return objData, nil
		},
		recorder,
		deploymentFile)
	if err != nil {
		_, _, _ = helpers.UpdateKlusterletStatus(ctx, n.klusterletClient, klusterlet.Name, helpers.UpdateKlusterletConditionFn(metav1.Condition{
			Type: klusterletApplied, Status: metav1.ConditionFalse, Reason: "KlusterletApplyFailed",
			Message: fmt.Sprintf("Failed to deploy %s deployment with error %v", deploymentFile, err),
		}))
		return relatedResources, generationStatus, err
	}

	return relatedResources, generationStatus, nil
}

// applyStaticFiles applies files using destKubeclient nad destApiExtensionClient.
// resource status will be saved in the relatedResourcesStatuses, and will save the
// result to the klusterlet status if there is any error.
func (n *klusterletController) applyStaticFiles(ctx context.Context, klusterletName string,
	staticFiles []string,
	config *klusterletConfig,
	destKubeclient kubernetes.Interface, destApiExtensionClient apiextensionsclient.Interface, destDynamicClient dynamic.Interface,
	recorder events.Recorder) ([]operatorapiv1.RelatedResourceMeta, error) {
	errs := []error{}
	var relatedResources []operatorapiv1.RelatedResourceMeta

	resourceResults := helpers.ApplyDirectly(
		ctx,
		destKubeclient,
		destApiExtensionClient,
		nil,
		destDynamicClient,
		recorder,
		n.cache,
		func(name string) ([]byte, error) {
			template, err := manifests.KlusterletManifestFiles.ReadFile(name)
			if err != nil {
				return nil, err
			}
			objData := assets.MustCreateAssetFromTemplate(name, template, config).Data
			helpers.SetRelatedResourcesStatusesWithObj(&relatedResources, objData)
			return objData, nil
		},
		staticFiles...,
	)

	for _, result := range resourceResults {
		if result.Error != nil {
			errs = append(errs, fmt.Errorf("%q (%T): %v", result.File, result.Type, result.Error))
		}
	}

	if len(errs) > 0 {
		applyErrors := operatorhelpers.NewMultiLineAggregate(errs)
		_, _, _ = helpers.UpdateKlusterletStatus(ctx, n.klusterletClient, klusterletName, helpers.UpdateKlusterletConditionFn(metav1.Condition{
			Type: klusterletApplied, Status: metav1.ConditionFalse, Reason: "KlusterletApplyFailed",
			Message: applyErrors.Error(),
		}))
		return relatedResources, applyErrors
	}

	return relatedResources, nil
}

// registrationServiceAccountName splices the name of registration service account
func registrationServiceAccountName(klusterletName string) string {
	return fmt.Sprintf("%s-registration-sa", klusterletName)
}

// workServiceAccountName splices the name of work service account
func workServiceAccountName(klusterletName string) string {
	return fmt.Sprintf("%s-work-sa", klusterletName)
}

// createManagedClusterKubeconfig creates managed cluster kubeconfig on the management cluster
// by fetching token from the managed cluster service account.
func (n *klusterletController) createManagedClusterKubeconfig(
	ctx context.Context,
	klusterletName, klusterletNamespace, agentNamespace string,
	saName, secretName string,
	kubeconfigTemplate *rest.Config,
	saClient kubernetes.Interface, secretClient coreclientv1.SecretsGetter,
	recorder events.Recorder) error {
	tokenGetter := helpers.SATokenGetter(ctx, saName, klusterletNamespace, saClient)
	err := helpers.SyncKubeConfigSecret(ctx, secretName, agentNamespace, "/spoke/config/kubeconfig", kubeconfigTemplate, n.kubeClient.CoreV1(), tokenGetter, recorder)
	if err != nil {
		_, _, _ = helpers.UpdateKlusterletStatus(ctx, n.klusterletClient, klusterletName, helpers.UpdateKlusterletConditionFn(metav1.Condition{
			Type: klusterletApplied, Status: metav1.ConditionFalse, Reason: "KlusterletApplyFailed",
			Message: fmt.Sprintf("Failed to create managed kubeconfig secret %s with error %v", secretName, err),
		}))
	}
	return err
}

// syncPullSecret will sync pull secret from the sourceClient cluster to the targetClient cluster in desired namespace.
func (n *klusterletController) syncPullSecret(ctx context.Context, sourceClient, targetClient kubernetes.Interface, klusterletName, namespace string, recorder events.Recorder) error {
	_, _, err := helpers.SyncSecret(
		ctx,
		sourceClient.CoreV1(),
		targetClient.CoreV1(),
		recorder,
		n.operatorNamespace,
		imagePullSecret,
		namespace,
		imagePullSecret,
		[]metav1.OwnerReference{},
	)

	if err != nil {
		_, _, _ = helpers.UpdateKlusterletStatus(ctx, n.klusterletClient, klusterletName, helpers.UpdateKlusterletConditionFn(metav1.Condition{
			Type: klusterletApplied, Status: metav1.ConditionFalse, Reason: "KlusterletApplyFailed",
			Message: fmt.Sprintf("Failed to sync image pull secret to namespace %q: %v", namespace, err),
		}))
		return err
	}
	return nil
}

func (n *klusterletController) ensureNamespace(ctx context.Context, kubeClient kubernetes.Interface, klusterletName, namespace string) error {
	_, err := kubeClient.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	switch {
	case errors.IsNotFound(err):
		_, createErr := kubeClient.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
				Annotations: map[string]string{
					"workload.openshift.io/allowed": "management",
				},
			},
		}, metav1.CreateOptions{})
		if createErr != nil {
			_, _, _ = helpers.UpdateKlusterletStatus(ctx, n.klusterletClient, klusterletName, helpers.UpdateKlusterletConditionFn(metav1.Condition{
				Type: klusterletApplied, Status: metav1.ConditionFalse, Reason: "KlusterletApplyFailed",
				Message: fmt.Sprintf("Failed to create namespace %q: %v", namespace, createErr),
			}))
			return createErr
		}
	case err != nil:
		_, _, _ = helpers.UpdateKlusterletStatus(ctx, n.klusterletClient, klusterletName, helpers.UpdateKlusterletConditionFn(metav1.Condition{
			Type: klusterletApplied, Status: metav1.ConditionFalse, Reason: "KlusterletApplyFailed",
			Message: fmt.Sprintf("Failed to get namespace %q: %v", namespace, err),
		}))
		return err
	}

	return nil
}

func removeStaticResources(ctx context.Context,
	kubeClient kubernetes.Interface,
	apiExtensionClient apiextensionsclient.Interface,
	resources []string,
	config klusterletConfig) error {
	for _, file := range resources {
		err := helpers.CleanUpStaticObject(
			ctx,
			kubeClient,
			apiExtensionClient,
			nil,
			func(name string) ([]byte, error) {
				template, err := manifests.KlusterletManifestFiles.ReadFile(name)
				if err != nil {
					return nil, err
				}
				return assets.MustCreateAssetFromTemplate(name, template, config).Data, nil
			},
			file,
		)
		if err != nil {
			return err
		}
	}

	return nil
}

// TODO also read CABundle from ExternalServerURLs and set into registration deployment
func getServersFromKlusterlet(klusterlet *operatorapiv1.Klusterlet) string {
	if klusterlet.Spec.ExternalServerURLs == nil {
		return ""
	}
	serverString := make([]string, 0, len(klusterlet.Spec.ExternalServerURLs))
	for _, server := range klusterlet.Spec.ExternalServerURLs {
		serverString = append(serverString, server.URL)
	}
	return strings.Join(serverString, ",")
}

// getManagedKubeConfig is a helper func for Hosted mode, it will retrive managed cluster
// kubeconfig from "external-managed-kubeconfig" secret.
func getManagedKubeConfig(ctx context.Context, kubeClient kubernetes.Interface, namespace, secretName string) (*rest.Config, error) {
	managedKubeconfigSecret, err := kubeClient.CoreV1().Secrets(namespace).Get(context.TODO(), secretName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return helpers.LoadClientConfigFromSecret(managedKubeconfigSecret)
}

// buildManagedClusterClientsFromSecret builds variety of clients for managed cluster from managed cluster kubeconfig secret.
func buildManagedClusterClientsFromSecret(ctx context.Context, client kubernetes.Interface, agentNamespace, secretName string) (
	*managedClusterClients, error) {
	// Ensure the agent namespace for users to create the external-managed-kubeconfig secret in this
	// namespace, so that in the next reconcile loop the controller can get the secret successfully after
	// the secret was created.
	err := ensureAgentNamespace(ctx, client, agentNamespace)
	if err != nil {
		return nil, err
	}

	managedKubeConfig, err := getManagedKubeConfig(ctx, client, agentNamespace, secretName)
	if err != nil {
		return nil, err
	}

	kubeClient, err := kubernetes.NewForConfig(managedKubeConfig)
	if err != nil {
		return nil, err
	}
	apiExtensionClient, err := apiextensionsclient.NewForConfig(managedKubeConfig)
	if err != nil {
		return nil, err
	}
	dynamicClient, err := dynamic.NewForConfig(managedKubeConfig)
	if err != nil {
		return nil, err
	}

	workClient, err := workclientset.NewForConfig(managedKubeConfig)
	if err != nil {
		return nil, err
	}

	return &managedClusterClients{
		kubeClient:                kubeClient,
		apiExtensionClient:        apiExtensionClient,
		appliedManifestWorkClient: workClient.WorkV1().AppliedManifestWorks(),
		dynamicClient:             dynamicClient,
		kubeconfig:                managedKubeConfig}, nil
}

// ensureAgentNamespace create agent namespace if it is not exist
func ensureAgentNamespace(ctx context.Context, kubeClient kubernetes.Interface, namespace string) error {
	_, err := kubeClient.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		_, createErr := kubeClient.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
				Annotations: map[string]string{
					"workload.openshift.io/allowed": "management",
				},
			},
		}, metav1.CreateOptions{})
		if createErr != nil {
			return createErr
		}
	}
	return err
}
