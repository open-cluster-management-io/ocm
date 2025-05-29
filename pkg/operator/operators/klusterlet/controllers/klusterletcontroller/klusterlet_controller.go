package klusterletcontroller

import (
	"context"
	"fmt"
	"strings"
	"time"

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
	managedResourcesEvictionTimestampAnno = "operator.open-cluster-management.io/managed-resources-eviction-timestamp"
	klusterletNamespaceLabelKey           = "operator.open-cluster-management.io/klusterlet"
)

type klusterletController struct {
	patcher                       patcher.Patcher[*operatorapiv1.Klusterlet, operatorapiv1.KlusterletSpec, operatorapiv1.KlusterletStatus]
	klusterletLister              operatorlister.KlusterletLister
	kubeClient                    kubernetes.Interface
	kubeVersion                   *version.Version
	operatorNamespace             string
	cache                         resourceapply.ResourceCache
	managedClusterClientsBuilder  managedClusterClientsBuilderInterface
	controlPlaneNodeLabelSelector string
	deploymentReplicas            int32
	disableAddonNamespace         bool
	enableSyncLabels              bool
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
	controlPlaneNodeLabelSelector string,
	deploymentReplicas int32,
	disableAddonNamespace bool,
	enableSyncLabels bool,
	recorder events.Recorder) factory.Controller {
	controller := &klusterletController{
		kubeClient: kubeClient,
		patcher: patcher.NewPatcher[
			*operatorapiv1.Klusterlet, operatorapiv1.KlusterletSpec, operatorapiv1.KlusterletStatus](klusterletClient),
		klusterletLister:              klusterletInformer.Lister(),
		kubeVersion:                   kubeVersion,
		operatorNamespace:             operatorNamespace,
		cache:                         resourceapply.NewResourceCache(),
		managedClusterClientsBuilder:  newManagedClusterClientsBuilder(kubeClient, apiExtensionClient, appliedManifestWorkClient, recorder),
		controlPlaneNodeLabelSelector: controlPlaneNodeLabelSelector,
		deploymentReplicas:            deploymentReplicas,
		disableAddonNamespace:         disableAddonNamespace,
		enableSyncLabels:              enableSyncLabels,
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

type AwsIrsa struct {
	HubClusterArn     string
	ManagedClusterArn string
}

type RegistrationDriver struct {
	AuthType string
	AwsIrsa  *AwsIrsa
}

type ManagedClusterIamRole struct {
	AwsIrsa *AwsIrsa
}

func (managedClusterIamRole *ManagedClusterIamRole) arn() string {
	managedClusterAccountId, managedClusterName := commonhelpers.GetAwsAccountIdAndClusterName(managedClusterIamRole.AwsIrsa.ManagedClusterArn)
	hubClusterAccountId, hubClusterName := commonhelpers.GetAwsAccountIdAndClusterName(managedClusterIamRole.AwsIrsa.HubClusterArn)
	md5HashUniqueIdentifier := commonhelpers.Md5HashSuffix(hubClusterAccountId, hubClusterName, managedClusterAccountId, managedClusterName)

	//arn:aws:iam::<managed-cluster-account-id>:role/ocm-managed-cluster-<md5-hash-unique-identifier>
	return "arn:aws:iam::" + managedClusterAccountId + ":role/ocm-managed-cluster-" + md5HashUniqueIdentifier
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
	MultipleHubs                                bool
	BootStrapKubeConfigSecret                   string
	BootStrapKubeConfigSecrets                  []string
	HubConnectionTimeoutSeconds                 int32
	OperatorNamespace                           string
	Replica                                     int32
	ClientCertExpirationSeconds                 int32
	ClusterAnnotationsString                    string
	RegistrationKubeAPIQPS                      float32
	RegistrationKubeAPIBurst                    int32
	WorkKubeAPIQPS                              float32
	WorkKubeAPIBurst                            int32
	WorkHubKubeAPIQPS                           float32
	WorkHubKubeAPIBurst                         int32
	AppliedManifestWorkEvictionGracePeriod      string
	WorkStatusSyncInterval                      string
	AgentKubeAPIQPS                             float32
	AgentKubeAPIBurst                           int32
	ExternalManagedKubeConfigSecret             string
	ExternalManagedKubeConfigRegistrationSecret string
	ExternalManagedKubeConfigWorkSecret         string
	ExternalManagedKubeConfigAgentSecret        string
	InstallMode                                 operatorapiv1.InstallMode

	MaxCustomClusterClaims       int
	ReservedClusterClaimSuffixes string
	// PriorityClassName is the name of the PriorityClass used by the deployed agents
	PriorityClassName string

	RegistrationFeatureGates []string
	WorkFeatureGates         []string

	HubApiServerHostAlias *operatorapiv1.HubApiServerHostAlias

	//  is useful for the testing cluster with limited resources or enabled resource quota.
	ResourceRequirementResourceType operatorapiv1.ResourceQosClass
	// ResourceRequirements is the resource requirements for the klusterlet managed containers.
	// The type has to be []byte to use "indent" template function.
	ResourceRequirements []byte

	// DisableAddonNamespace is the flag to disable the creationg of default addon namespace.
	DisableAddonNamespace bool

	// Labels of the agents are synced from klusterlet CR.
	Labels             map[string]string
	RegistrationDriver RegistrationDriver

	ManagedClusterArn        string
	ManagedClusterRoleArn    string
	ManagedClusterRoleSuffix string
}

// If multiplehubs feature gate is enabled, using the bootstrapkubeconfigs from klusterlet CR.
// Otherwise, using the default bootstrapkubeconfig.
func (config *klusterletConfig) populateBootstrap(klusterlet *operatorapiv1.Klusterlet) {
	config.MultipleHubs = klusterlet.Spec.RegistrationConfiguration != nil &&
		helpers.FeatureGateEnabled(klusterlet.Spec.RegistrationConfiguration.FeatureGates, ocmfeature.DefaultSpokeRegistrationFeatureGates, ocmfeature.MultipleHubs)

	if config.MultipleHubs {
		var bootstapKubeconfigSecrets []string
		if klusterlet.Spec.RegistrationConfiguration.BootstrapKubeConfigs.Type == operatorapiv1.LocalSecrets &&
			klusterlet.Spec.RegistrationConfiguration.BootstrapKubeConfigs.LocalSecrets != nil {
			for _, secret := range klusterlet.Spec.RegistrationConfiguration.BootstrapKubeConfigs.LocalSecrets.KubeConfigSecrets {
				bootstapKubeconfigSecrets = append(bootstapKubeconfigSecrets, secret.Name)
			}
		}
		config.BootStrapKubeConfigSecrets = bootstapKubeconfigSecrets
		config.HubConnectionTimeoutSeconds = klusterlet.Spec.RegistrationConfiguration.BootstrapKubeConfigs.LocalSecrets.HubConnectionTimeoutSeconds
	} else {
		config.BootStrapKubeConfigSecret = helpers.BootstrapHubKubeConfig
	}
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

	resourceRequirements, err := helpers.ResourceRequirements(klusterlet)
	if err != nil {
		klog.Errorf("Failed to parse resource requirements for klusterlet %s: %v", klusterlet.Name, err)
		return err
	}

	replica := n.deploymentReplicas
	if replica <= 0 {
		replica = helpers.DetermineReplica(ctx, n.kubeClient, klusterlet.Spec.DeployOption.Mode, n.kubeVersion, n.controlPlaneNodeLabelSelector)
	}

	config := klusterletConfig{
		KlusterletName:      klusterlet.Name,
		KlusterletNamespace: helpers.KlusterletNamespace(klusterlet),
		AgentNamespace:      helpers.AgentNamespace(klusterlet),
		AgentID:             string(klusterlet.UID),
		RegistrationImage:   klusterlet.Spec.RegistrationImagePullSpec,
		WorkImage:           klusterlet.Spec.WorkImagePullSpec,
		ClusterName:         klusterlet.Spec.ClusterName,
		SingletonImage:      klusterlet.Spec.ImagePullSpec,
		HubKubeConfigSecret: helpers.HubKubeConfig,
		ExternalServerURL:   getServersFromKlusterlet(klusterlet),
		OperatorNamespace:   n.operatorNamespace,
		Replica:             replica,
		PriorityClassName:   helpers.AgentPriorityClassName(klusterlet, n.kubeVersion),

		ExternalManagedKubeConfigSecret:             helpers.ExternalManagedKubeConfig,
		ExternalManagedKubeConfigRegistrationSecret: helpers.ExternalManagedKubeConfigRegistration,
		ExternalManagedKubeConfigWorkSecret:         helpers.ExternalManagedKubeConfigWork,
		ExternalManagedKubeConfigAgentSecret:        helpers.ExternalManagedKubeConfigAgent,
		InstallMode:                                 klusterlet.Spec.DeployOption.Mode,
		HubApiServerHostAlias:                       klusterlet.Spec.HubApiServerHostAlias,

		RegistrationServiceAccount:      serviceAccountName("registration-sa", klusterlet),
		WorkServiceAccount:              serviceAccountName("work-sa", klusterlet),
		ResourceRequirementResourceType: helpers.ResourceType(klusterlet),
		ResourceRequirements:            resourceRequirements,
		DisableAddonNamespace:           n.disableAddonNamespace,
	}

	config.populateBootstrap(klusterlet)

	if n.enableSyncLabels {
		config.Labels = helpers.GetKlusterletAgentLabels(klusterlet)
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
				Type: operatorapiv1.ConditionReadyToApply, Status: metav1.ConditionFalse, Reason: operatorapiv1.ReasonKlusterletPrepareFailed,
				Message: fmt.Sprintf("Failed to build managed cluster clients: %v", err),
			})
		} else {
			message := "Klusterlet is ready to apply"
			if !managedClusterClients.kubeconfigSecretCreationTime.IsZero() {
				message = "Klusterlet is ready to apply, the external managed kubeconfig secret was created at: " +
					managedClusterClients.kubeconfigSecretCreationTime.Format(time.RFC3339)
			}
			meta.SetStatusCondition(&klusterlet.Status.Conditions, metav1.Condition{
				Type: operatorapiv1.ConditionReadyToApply, Status: metav1.ConditionTrue, Reason: operatorapiv1.ReasonKlusterletPrepared,
				Message: message,
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
	if !commonhelpers.HasFinalizer(klusterlet.Finalizers, klusterletFinalizer) {
		return nil
	}

	// If there are some invalid feature gates of registration or work, will output condition `ValidFeatureGates`
	// False in Klusterlet.
	// TODO: For the work feature gates, when splitting permissions in the future, if the ExecutorValidatingCaches
	//       function is enabled, additional permissions for get, list, and watch RBAC resources required by this
	//       function need to be applied
	var registrationFeatureMsgs, workFeatureMsgs string
	var registrationFeatureGates []operatorapiv1.FeatureGate
	if klusterlet.Spec.RegistrationConfiguration != nil {
		registrationFeatureGates = klusterlet.Spec.RegistrationConfiguration.FeatureGates
		config.ClientCertExpirationSeconds = klusterlet.Spec.RegistrationConfiguration.ClientCertExpirationSeconds
		config.RegistrationKubeAPIQPS = float32(klusterlet.Spec.RegistrationConfiguration.KubeAPIQPS)
		config.RegistrationKubeAPIBurst = klusterlet.Spec.RegistrationConfiguration.KubeAPIBurst
		// Configuring Registration driver depending on registration auth
		if &klusterlet.Spec.RegistrationConfiguration.RegistrationDriver != nil &&
			klusterlet.Spec.RegistrationConfiguration.RegistrationDriver.AuthType == commonhelpers.AwsIrsaAuthType {

			hubClusterArn := klusterlet.Spec.RegistrationConfiguration.RegistrationDriver.AwsIrsa.HubClusterArn
			managedClusterArn := klusterlet.Spec.RegistrationConfiguration.RegistrationDriver.AwsIrsa.ManagedClusterArn

			config.RegistrationDriver = RegistrationDriver{
				AuthType: klusterlet.Spec.RegistrationConfiguration.RegistrationDriver.AuthType,
				AwsIrsa: &AwsIrsa{
					HubClusterArn:     hubClusterArn,
					ManagedClusterArn: managedClusterArn,
				},
			}
			managedClusterIamRole := ManagedClusterIamRole{
				AwsIrsa: config.RegistrationDriver.AwsIrsa,
			}
			config.ManagedClusterRoleArn = managedClusterIamRole.arn()
			managedClusterAccountId, managedClusterName := commonhelpers.GetAwsAccountIdAndClusterName(managedClusterIamRole.AwsIrsa.ManagedClusterArn)
			hubClusterAccountId, hubClusterName := commonhelpers.GetAwsAccountIdAndClusterName(managedClusterIamRole.AwsIrsa.HubClusterArn)
			config.ManagedClusterRoleSuffix = commonhelpers.Md5HashSuffix(hubClusterAccountId, hubClusterName, managedClusterAccountId, managedClusterName)
		} else {
			config.RegistrationDriver = RegistrationDriver{
				AuthType: klusterlet.Spec.RegistrationConfiguration.RegistrationDriver.AuthType,
			}
		}

		// include clusterClaimConfig info if it exists
		if klusterlet.Spec.RegistrationConfiguration.ClusterClaimConfiguration != nil {
			config.MaxCustomClusterClaims = int(klusterlet.Spec.RegistrationConfiguration.ClusterClaimConfiguration.MaxCustomClusterClaims)
			config.ReservedClusterClaimSuffixes = strings.Join(
				klusterlet.Spec.RegistrationConfiguration.ClusterClaimConfiguration.ReservedClusterClaimSuffixes, ",")
		}

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
		config.WorkHubKubeAPIQPS = float32(klusterlet.Spec.WorkConfiguration.HubKubeAPIQPS)
		config.WorkHubKubeAPIBurst = klusterlet.Spec.WorkConfiguration.HubKubeAPIBurst
		if klusterlet.Spec.WorkConfiguration.AppliedManifestWorkEvictionGracePeriod != nil {
			config.AppliedManifestWorkEvictionGracePeriod = klusterlet.Spec.WorkConfiguration.AppliedManifestWorkEvictionGracePeriod.Duration.String()
		}
		if klusterlet.Spec.WorkConfiguration.StatusSyncInterval != nil {
			config.WorkStatusSyncInterval = klusterlet.Spec.WorkConfiguration.StatusSyncInterval.Duration.String()
		}
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
			recorder:              controllerContext.Recorder(),
			cache:                 n.cache},
		&managedReconcile{
			managedClusterClients: managedClusterClients,
			kubeClient:            n.kubeClient,
			kubeVersion:           n.kubeVersion,
			operatorNamespace:     n.operatorNamespace,
			recorder:              controllerContext.Recorder(),
			cache:                 n.cache,
			enableSyncLabels:      n.enableSyncLabels},
		&managementReconcile{
			kubeClient:        n.kubeClient,
			operatorNamespace: n.operatorNamespace,
			recorder:          controllerContext.Recorder(),
			cache:             n.cache,
			enableSyncLabels:  n.enableSyncLabels},
		&runtimeReconcile{
			managedClusterClients: managedClusterClients,
			kubeClient:            n.kubeClient,
			recorder:              controllerContext.Recorder(),
			cache:                 n.cache,
			enableSyncLabels:      n.enableSyncLabels},
		&namespaceReconcile{
			managedClusterClients: managedClusterClients,
		},
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
			Type: operatorapiv1.ConditionKlusterletApplied, Status: metav1.ConditionTrue, Reason: operatorapiv1.ReasonKlusterletApplied,
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
func getManagedKubeConfig(ctx context.Context, kubeClient kubernetes.Interface,
	namespace, secretName string) (metav1.Time, *rest.Config, error) {
	managedKubeconfigSecret, err := kubeClient.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		return metav1.Time{}, nil, err
	}

	config, err := helpers.LoadClientConfigFromSecret(managedKubeconfigSecret)
	return managedKubeconfigSecret.CreationTimestamp, config, err
}

// syncPullSecret will sync pull secret from the sourceClient cluster to the targetClient cluster in desired namespace.
func syncPullSecret(ctx context.Context, sourceClient, targetClient kubernetes.Interface,
	klusterlet *operatorapiv1.Klusterlet, operatorNamespace, namespace string, labels map[string]string, recorder events.Recorder) error {
	_, _, err := helpers.SyncSecret(
		ctx,
		sourceClient.CoreV1(),
		targetClient.CoreV1(),
		recorder,
		operatorNamespace,
		helpers.ImagePullSecret,
		namespace,
		helpers.ImagePullSecret,
		[]metav1.OwnerReference{},
		labels,
	)

	if err != nil {
		meta.SetStatusCondition(&klusterlet.Status.Conditions, metav1.Condition{
			Type: operatorapiv1.ConditionKlusterletApplied, Status: metav1.ConditionFalse, Reason: operatorapiv1.ReasonKlusterletApplyFailed,
			Message: fmt.Sprintf("Failed to sync image pull secret to namespace %q: %v", namespace, err)})
		return err
	}
	return nil
}

// ensureNamespace is to apply the namespace defined in klusterlet spec to the managed cluster. The namespace
// will have a klusterlet label.
func ensureNamespace(
	ctx context.Context,
	kubeClient kubernetes.Interface,
	klusterlet *operatorapiv1.Klusterlet,
	namespace string, labels map[string]string, recorder events.Recorder) error {
	_, _, err := resourceapply.ApplyNamespace(ctx, kubeClient.CoreV1(), recorder, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
			Annotations: map[string]string{
				"workload.openshift.io/allowed": "management",
			},
			Labels: labels,
		},
	})
	if err != nil {
		meta.SetStatusCondition(&klusterlet.Status.Conditions, metav1.Condition{
			Type:    operatorapiv1.ConditionKlusterletApplied,
			Status:  metav1.ConditionFalse,
			Reason:  operatorapiv1.ReasonKlusterletApplyFailed,
			Message: fmt.Sprintf("Failed to ensure namespace of klusterlet %q: %v", namespace, err)})
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
