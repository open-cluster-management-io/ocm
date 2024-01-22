package klusterletcontroller

import (
	"context"
	"fmt"
	"strings"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	corev1 "k8s.io/api/core/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/version"
	appsinformer "k8s.io/client-go/informers/apps/v1"
	coreinformer "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	operatorv1client "open-cluster-management.io/api/client/operator/clientset/versioned/typed/operator/v1"
	operatorinformer "open-cluster-management.io/api/client/operator/informers/externalversions/operator/v1"
	operatorlister "open-cluster-management.io/api/client/operator/listers/operator/v1"
	workv1client "open-cluster-management.io/api/client/work/clientset/versioned/typed/work/v1"
	ocmfeature "open-cluster-management.io/api/feature"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	commonhelpers "open-cluster-management.io/ocm/pkg/common/helpers"
	"open-cluster-management.io/ocm/pkg/common/queue"
	"open-cluster-management.io/ocm/pkg/operator/helpers"
)

const (

	// klusterletHostedFinalizer is used to clean up resources on the managed/hosted cluster in Hosted mode
	klusterletHostedFinalizer             = "operator.open-cluster-management.io/klusterlet-hosted-cleanup"
	klusterletFinalizer                   = "operator.open-cluster-management.io/klusterlet-cleanup"
	imagePullSecret                       = "open-cluster-management-image-pull-credentials"
	klusterletApplied                     = "Applied"
	klusterletReadyToApply                = "ReadyToApply"
	hubConnectionDegraded                 = "HubConnectionDegraded"
	hubKubeConfigSecretMissing            = "HubKubeConfigSecretMissing" // #nosec G101
	managedResourcesEvictionTimestampAnno = "operator.open-cluster-management.io/managed-resources-eviction-timestamp"
)

type klusterletController struct {
	patcher                      patcher.Patcher[*operatorapiv1.Klusterlet, operatorapiv1.KlusterletSpec, operatorapiv1.KlusterletStatus]
	klusterletLister             operatorlister.KlusterletLister
	kubeClient                   kubernetes.Interface
	kubeVersion                  *version.Version
	operatorNamespace            string
	skipHubSecretPlaceholder     bool
	cache                        resourceapply.ResourceCache
	managedClusterClientsBuilder managedClusterClientsBuilderInterface
}

type klusterletReconcile interface {
	reconcile(ctx context.Context, cm *operatorapiv1.Klusterlet, config klusterletConfig) (*operatorapiv1.Klusterlet, reconcileState, error)
	clean(ctx context.Context, cm *operatorapiv1.Klusterlet, config klusterletConfig) (*operatorapiv1.Klusterlet, reconcileState, error)
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
	klusterletClient operatorv1client.KlusterletInterface,
	klusterletInformer operatorinformer.KlusterletInformer,
	secretInformers map[string]coreinformer.SecretInformer,
	deploymentInformer appsinformer.DeploymentInformer,
	appliedManifestWorkClient workv1client.AppliedManifestWorkInterface,
	kubeVersion *version.Version,
	operatorNamespace string,
	recorder events.Recorder,
	skipHubSecretPlaceholder bool) factory.Controller {
	controller := &klusterletController{
		kubeClient: kubeClient,
		patcher: patcher.NewPatcher[
			*operatorapiv1.Klusterlet, operatorapiv1.KlusterletSpec, operatorapiv1.KlusterletStatus](klusterletClient),
		klusterletLister:             klusterletInformer.Lister(),
		kubeVersion:                  kubeVersion,
		operatorNamespace:            operatorNamespace,
		skipHubSecretPlaceholder:     skipHubSecretPlaceholder,
		cache:                        resourceapply.NewResourceCache(),
		managedClusterClientsBuilder: newManagedClusterClientsBuilder(kubeClient, apiExtensionClient, appliedManifestWorkClient, recorder),
	}

	return factory.New().WithSync(controller.sync).
		WithInformersQueueKeysFunc(helpers.KlusterletSecretQueueKeyFunc(controller.klusterletLister),
			secretInformers[helpers.HubKubeConfig].Informer(),
			secretInformers[helpers.BootstrapHubKubeConfig].Informer(),
			secretInformers[helpers.ExternalManagedKubeConfig].Informer()).
		WithInformersQueueKeysFunc(helpers.KlusterletDeploymentQueueKeyFunc(
			controller.klusterletLister), deploymentInformer.Informer()).
		WithInformersQueueKeysFunc(queue.QueueKeyByMetaName, klusterletInformer.Informer()).
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
	AgentNamespace                              string
	AgentID                                     string
	RegistrationImage                           string
	WorkImage                                   string
	SingletonImage                              string
	RegistrationServiceAccount                  string
	WorkServiceAccount                          string
	ClusterName                                 string
	ExternalServerURL                           string
	HubKubeConfigSecret                         string
	BootStrapKubeConfigSecret                   string
	OperatorNamespace                           string
	Replica                                     int32
	ClientCertExpirationSeconds                 int32
	ClusterAnnotationsString                    string
	RegistrationKubeAPIQPS                      float32
	RegistrationKubeAPIBurst                    int32
	WorkKubeAPIQPS                              float32
	WorkKubeAPIBurst                            int32
	AgentKubeAPIQPS                             float32
	AgentKubeAPIBurst                           int32
	ExternalManagedKubeConfigSecret             string
	ExternalManagedKubeConfigRegistrationSecret string
	ExternalManagedKubeConfigWorkSecret         string
	ExternalManagedKubeConfigAgentSecret        string
	InstallMode                                 operatorapiv1.InstallMode

	RegistrationFeatureGates []string
	WorkFeatureGates         []string

	HubApiServerHostAlias *operatorapiv1.HubApiServerHostAlias

	//  is useful for the testing cluster with limited resources or enabled resource quota.
	ResourceRequirement operatorapiv1.ResourceQosClass
}

func (n *klusterletController) sync(ctx context.Context, controllerContext factory.SyncContext) error {
	klusterletName := controllerContext.QueueKey()
	klog.V(4).Infof("Reconciling Klusterlet %q", klusterletName)
	originalKlusterlet, err := n.klusterletLister.Get(klusterletName)
	if errors.IsNotFound(err) {
		// Klusterlet not found, could have been deleted, do nothing.
		return nil
	}
	if err != nil {
		return err
	}
	klusterlet := originalKlusterlet.DeepCopy()

	config := klusterletConfig{
		KlusterletName:            klusterlet.Name,
		KlusterletNamespace:       helpers.KlusterletNamespace(klusterlet),
		AgentNamespace:            helpers.AgentNamespace(klusterlet),
		AgentID:                   string(klusterlet.UID),
		RegistrationImage:         klusterlet.Spec.RegistrationImagePullSpec,
		WorkImage:                 klusterlet.Spec.WorkImagePullSpec,
		ClusterName:               klusterlet.Spec.ClusterName,
		SingletonImage:            klusterlet.Spec.ImagePullSpec,
		BootStrapKubeConfigSecret: helpers.BootstrapHubKubeConfig,
		HubKubeConfigSecret:       helpers.HubKubeConfig,
		ExternalServerURL:         getServersFromKlusterlet(klusterlet),
		OperatorNamespace:         n.operatorNamespace,
		Replica:                   helpers.DetermineReplica(ctx, n.kubeClient, klusterlet.Spec.DeployOption.Mode, n.kubeVersion),

		ExternalManagedKubeConfigSecret:             helpers.ExternalManagedKubeConfig,
		ExternalManagedKubeConfigRegistrationSecret: helpers.ExternalManagedKubeConfigRegistration,
		ExternalManagedKubeConfigWorkSecret:         helpers.ExternalManagedKubeConfigWork,
		ExternalManagedKubeConfigAgentSecret:        helpers.ExternalManagedKubeConfigAgent,
		InstallMode:                                 klusterlet.Spec.DeployOption.Mode,
		HubApiServerHostAlias:                       klusterlet.Spec.HubApiServerHostAlias,

		RegistrationServiceAccount: serviceAccountName("registration-sa", klusterlet),
		WorkServiceAccount:         serviceAccountName("work-sa", klusterlet),
		ResourceRequirement:        helpers.ResourceType(klusterlet),
	}

	managedClusterClients, err := n.managedClusterClientsBuilder.
		withMode(config.InstallMode).
		withKubeConfigSecret(config.AgentNamespace, config.ExternalManagedKubeConfigSecret).
		build(ctx)

	// update klusterletReadyToApply condition at first in hosted mode
	// this conditions should be updated even when klusterlet is in deleting state.
	if helpers.IsHosted(config.InstallMode) {
		if err != nil {
			meta.SetStatusCondition(&klusterlet.Status.Conditions, metav1.Condition{
				Type: klusterletReadyToApply, Status: metav1.ConditionFalse, Reason: "KlusterletPrepareFailed",
				Message: fmt.Sprintf("Failed to build managed cluster clients: %v", err),
			})
		} else {
			meta.SetStatusCondition(&klusterlet.Status.Conditions, metav1.Condition{
				Type: klusterletReadyToApply, Status: metav1.ConditionTrue, Reason: "KlusterletPrepared",
				Message: "Klusterlet is ready to apply",
			})
		}

		updated, updateErr := n.patcher.PatchStatus(ctx, klusterlet, klusterlet.Status, originalKlusterlet.Status)
		if updated {
			return updateErr
		}
	}

	if err != nil {
		return err
	}

	if !klusterlet.DeletionTimestamp.IsZero() {
		// The work of klusterlet cleanup will be handled by klusterlet cleanup controller
		return nil
	}

	// do nothing until finalizer is added.
	if !hasFinalizer(klusterlet, klusterletFinalizer) {
		return nil
	}

	// If there are some invalid feature gates of registration or work, will output condition `ValidFeatureGates`
	// False in Klusterlet.
	// TODO: For the work feature gates, when splitting permissions in the future, if the ExecutorValidatingCaches
	//       function is enabled, additional permissions for get, list, and watch RBAC resources required by this
	//       function need to be applied
	var registrationFeatureMsgs, workFeatureMsgs string
	registrationFeatureGates := helpers.DefaultSpokeRegistrationFeatureGates
	if klusterlet.Spec.RegistrationConfiguration != nil {
		registrationFeatureGates = klusterlet.Spec.RegistrationConfiguration.FeatureGates
		config.ClientCertExpirationSeconds = klusterlet.Spec.RegistrationConfiguration.ClientCertExpirationSeconds
		config.RegistrationKubeAPIQPS = float32(klusterlet.Spec.RegistrationConfiguration.KubeAPIQPS)
		config.RegistrationKubeAPIBurst = klusterlet.Spec.RegistrationConfiguration.KubeAPIBurst

		// construct cluster annotations string, the final format is "key1=value1,key2=value2"
		var annotationsArray []string
		for k, v := range commonhelpers.FilterClusterAnnotations(klusterlet.Spec.RegistrationConfiguration.ClusterAnnotations) {
			annotationsArray = append(annotationsArray, fmt.Sprintf("%s=%s", k, v))
		}
		config.ClusterAnnotationsString = strings.Join(annotationsArray, ",")
	}
	config.RegistrationFeatureGates, registrationFeatureMsgs = helpers.ConvertToFeatureGateFlags("Registration",
		registrationFeatureGates, ocmfeature.DefaultSpokeRegistrationFeatureGates)

	var workFeatureGates []operatorapiv1.FeatureGate
	if klusterlet.Spec.WorkConfiguration != nil {
		workFeatureGates = klusterlet.Spec.WorkConfiguration.FeatureGates
		config.WorkKubeAPIQPS = float32(klusterlet.Spec.WorkConfiguration.KubeAPIQPS)
		config.WorkKubeAPIBurst = klusterlet.Spec.WorkConfiguration.KubeAPIBurst
	}

	config.WorkFeatureGates, workFeatureMsgs = helpers.ConvertToFeatureGateFlags("Work", workFeatureGates, ocmfeature.DefaultSpokeWorkFeatureGates)
	meta.SetStatusCondition(&klusterlet.Status.Conditions, helpers.BuildFeatureCondition(registrationFeatureMsgs, workFeatureMsgs))

	// for singleton agent, the QPS and Burst use the max one between the configurations of registration and work
	config.AgentKubeAPIQPS = config.RegistrationKubeAPIQPS
	if config.AgentKubeAPIQPS < config.WorkKubeAPIQPS {
		config.AgentKubeAPIQPS = config.WorkKubeAPIQPS
	}
	config.AgentKubeAPIBurst = config.RegistrationKubeAPIBurst
	if config.AgentKubeAPIBurst < config.WorkKubeAPIBurst {
		config.AgentKubeAPIBurst = config.WorkKubeAPIBurst
	}

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

	klusterlet.Status.ObservedGeneration = klusterlet.Generation

	if len(errs) == 0 {
		meta.SetStatusCondition(&klusterlet.Status.Conditions, metav1.Condition{
			Type: klusterletApplied, Status: metav1.ConditionTrue, Reason: "KlusterletApplied",
			Message: "Klusterlet Component Applied"})
	} else {
		// When appliedCondition is false, we should not update related resources and resource generations
		klusterlet.Status.RelatedResources = originalKlusterlet.Status.RelatedResources
		klusterlet.Status.Generations = originalKlusterlet.Status.Generations
	}

	// If we get here, we have successfully applied everything.
	_, updatedErr := n.patcher.PatchStatus(ctx, klusterlet, klusterlet.Status, originalKlusterlet.Status)
	if updatedErr != nil {
		errs = append(errs, updatedErr)
	}
	return utilerrors.NewAggregate(errs)
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

// getManagedKubeConfig is a helper func for Hosted mode, it will retrieve managed cluster
// kubeconfig from "external-managed-kubeconfig" secret.
func getManagedKubeConfig(ctx context.Context, kubeClient kubernetes.Interface, namespace, secretName string) (*rest.Config, error) {
	managedKubeconfigSecret, err := kubeClient.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return helpers.LoadClientConfigFromSecret(managedKubeconfigSecret)
}

// ensureAgentNamespace create agent namespace if it is not exist
func ensureAgentNamespace(ctx context.Context, kubeClient kubernetes.Interface, namespace string, recorder events.Recorder) error {
	_, _, err := resourceapply.ApplyNamespace(ctx, kubeClient.CoreV1(), recorder, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
			Annotations: map[string]string{
				"workload.openshift.io/allowed": "management",
			},
		},
	})
	return err
}

// syncPullSecret will sync pull secret from the sourceClient cluster to the targetClient cluster in desired namespace.
func syncPullSecret(ctx context.Context, sourceClient, targetClient kubernetes.Interface,
	klusterlet *operatorapiv1.Klusterlet, operatorNamespace, namespace string, recorder events.Recorder) error {
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

func ensureNamespace(ctx context.Context, kubeClient kubernetes.Interface, klusterlet *operatorapiv1.Klusterlet,
	namespace string, recorder events.Recorder) error {
	if err := ensureAgentNamespace(ctx, kubeClient, namespace, recorder); err != nil {
		meta.SetStatusCondition(&klusterlet.Status.Conditions, metav1.Condition{
			Type: klusterletApplied, Status: metav1.ConditionFalse, Reason: "KlusterletApplyFailed",
			Message: fmt.Sprintf("Failed to ensure namespace %q: %v", namespace, err)})
		return err
	}
	return nil
}

func serviceAccountName(suffix string, klusterlet *operatorapiv1.Klusterlet) string {
	// in singleton mode, we only need one sa, so the name of work and registration sa are
	// the same. We need to use the name of work sa for now, since the work sa permission can be
	// escalated by create manifestwork from other actors.
	// TODO(qiujian16) revisit to see if we can use inpersonate in work agent.
	if helpers.IsSingleton(klusterlet.Spec.DeployOption.Mode) {
		return fmt.Sprintf("%s-work-sa", klusterlet.Name)
	}
	return fmt.Sprintf("%s-%s", klusterlet.Name, suffix)
}
