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
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	operatorv1client "open-cluster-management.io/api/client/operator/clientset/versioned/typed/operator/v1"
	operatorinformer "open-cluster-management.io/api/client/operator/informers/externalversions/operator/v1"
	operatorlister "open-cluster-management.io/api/client/operator/listers/operator/v1"
	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	workv1client "open-cluster-management.io/api/client/work/clientset/versioned/typed/work/v1"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"
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

type klusterletReconcile interface {
	reconcile(ctx context.Context, cm *operatorapiv1.Klusterlet, config klusterletConfig) (*operatorapiv1.Klusterlet, reconcileState, error)
}

type reconcileState int64

const (
	reconcileStop reconcileState = iota
	reconcileContinue
)

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
	WorkFeatureGates         []string

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

	var featureGateCondition metav1.Condition
	// If there are some invalid feature gates of registration or work, will output condition `ValidFeatureGates`
	// False in Klusterlet.
	// TODO: For the work feature gates, when splitting permissions in the future, if the ExecutorValidatingCaches
	//       function is enabled, additional permissions for get, list, and watch RBAC resources required by this
	//       function need to be applied
	config.RegistrationFeatureGates, config.WorkFeatureGates, featureGateCondition = helpers.CheckFeatureGates(
		helpers.OperatorTypeKlusterlet,
		klusterlet.Spec.RegistrationConfiguration,
		klusterlet.Spec.WorkConfiguration)

	reconcilers := []klusterletReconcile{
		&crdReconcile{
			managedClusterClients: managedClusterClients,
			kubeVersion:           n.kubeVersion,
			recorder:              controllerContext.Recorder(),
			cache:                 n.cache},
		&managedReconcile{
			managedClusterClients: managedClusterClients,
			kubeClient:            n.kubeClient,
			kubeVersion:           n.kubeVersion,
			opratorNamespace:      n.operatorNamespace,
			recorder:              controllerContext.Recorder(),
			cache:                 n.cache},
		&managementReconcile{
			kubeClient:        n.kubeClient,
			operatorNamespace: n.operatorNamespace,
			recorder:          controllerContext.Recorder(),
			cache:             n.cache},
		&runtimeReconcile{
			managedClusterClients: managedClusterClients,
			kubeClient:            n.kubeClient,
			recorder:              controllerContext.Recorder(),
			cache:                 n.cache},
	}

	var errs []error
	for _, reconciler := range reconcilers {
		var state reconcileState
		klusterlet, state, err = reconciler.reconcile(ctx, klusterlet, config)
		if err != nil {
			errs = append(errs, err)
		}
		if state == reconcileStop {
			break
		}
	}

	appliedCondition := meta.FindStatusCondition(klusterlet.Status.Conditions, klusterletApplied)
	if len(errs) == 0 {
		appliedCondition = &metav1.Condition{
			Type: klusterletApplied, Status: metav1.ConditionTrue, Reason: "KlusterletApplied",
			Message: "Klusterlet Component Applied"}
	} else if appliedCondition == nil {
		appliedCondition = &metav1.Condition{
			Type: klusterletApplied, Status: metav1.ConditionFalse, Reason: "KlusterletApplyFailed",
			Message: "Klusterlet Component Apply failed"}
	}

	// If we get here, we have successfully applied everything and should indicate that
	_, _, _ = helpers.UpdateKlusterletStatus(ctx, n.klusterletClient, klusterletName,
		helpers.UpdateKlusterletConditionFn(featureGateCondition, *appliedCondition),
		helpers.UpdateKlusterletGenerationsFn(klusterlet.Status.Generations...),
		helpers.UpdateKlusterletRelatedResourcesFn(klusterlet.Status.RelatedResources...),
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
	managedKubeconfigSecret, err := kubeClient.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
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

// syncPullSecret will sync pull secret from the sourceClient cluster to the targetClient cluster in desired namespace.
func syncPullSecret(ctx context.Context, sourceClient, targetClient kubernetes.Interface, klusterlet *operatorapiv1.Klusterlet, operatorNamespace, namespace string, recorder events.Recorder) error {
	_, _, err := helpers.SyncSecret(
		ctx,
		sourceClient.CoreV1(),
		targetClient.CoreV1(),
		recorder,
		operatorNamespace,
		imagePullSecret,
		namespace,
		imagePullSecret,
		[]metav1.OwnerReference{},
	)

	if err != nil {
		meta.SetStatusCondition(&klusterlet.Status.Conditions, metav1.Condition{
			Type: klusterletApplied, Status: metav1.ConditionFalse, Reason: "KlusterletApplyFailed",
			Message: fmt.Sprintf("Failed to sync image pull secret to namespace %q: %v", namespace, err)})
		return err
	}
	return nil
}

func ensureNamespace(ctx context.Context, kubeClient kubernetes.Interface, klusterlet *operatorapiv1.Klusterlet, namespace string) error {
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
			meta.SetStatusCondition(&klusterlet.Status.Conditions, metav1.Condition{
				Type: klusterletApplied, Status: metav1.ConditionFalse, Reason: "KlusterletApplyFailed",
				Message: fmt.Sprintf("Failed to create namespace %q: %v", namespace, createErr)})
			return createErr
		}
	case err != nil:
		meta.SetStatusCondition(&klusterlet.Status.Conditions, metav1.Condition{
			Type: klusterletApplied, Status: metav1.ConditionFalse, Reason: "KlusterletApplyFailed",
			Message: fmt.Sprintf("Failed to get namespace %q: %v", namespace, err)})
		return err
	}

	return nil
}
