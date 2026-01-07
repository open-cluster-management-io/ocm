package clustermanagercontroller

import (
	"context"
	"encoding/base64"
	errorhelpers "errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/openshift/library-go/pkg/assets"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	corev1 "k8s.io/api/core/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	appsinformer "k8s.io/client-go/informers/apps/v1"
	corev1informers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	migrationclient "sigs.k8s.io/kube-storage-version-migrator/pkg/clients/clientset/typed/migration/v1alpha1"

	operatorv1client "open-cluster-management.io/api/client/operator/clientset/versioned/typed/operator/v1"
	operatorinformer "open-cluster-management.io/api/client/operator/informers/externalversions/operator/v1"
	operatorlister "open-cluster-management.io/api/client/operator/listers/operator/v1"
	ocmfeature "open-cluster-management.io/api/feature"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/events"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	"open-cluster-management.io/ocm/manifests"
	commonhelper "open-cluster-management.io/ocm/pkg/common/helpers"
	"open-cluster-management.io/ocm/pkg/common/queue"
	"open-cluster-management.io/ocm/pkg/operator/helpers"
)

const (
	clusterManagerFinalizer = "operator.open-cluster-management.io/cluster-manager-cleanup"

	defaultWebhookPort       = int32(9443)
	clusterManagerReSyncTime = 5 * time.Second
)

type clusterManagerController struct {
	patcher              patcher.Patcher[*operatorapiv1.ClusterManager, operatorapiv1.ClusterManagerSpec, operatorapiv1.ClusterManagerStatus]
	clusterManagerLister operatorlister.ClusterManagerLister
	operatorKubeClient   kubernetes.Interface
	operatorKubeconfig   *rest.Config
	configMapLister      corev1listers.ConfigMapLister
	cache                resourceapply.ResourceCache
	// For testcases which don't need these functions, we could set fake funcs
	ensureSAKubeconfigs func(ctx context.Context, clusterManagerName, clusterManagerNamespace string,
		hubConfig *rest.Config, hubClient, managementClient kubernetes.Interface, recorder events.Recorder,
		mwctrEnabled, addonManagerEnabled, grpcAuthEnabled bool) error
	generateHubClusterClients func(hubConfig *rest.Config) (kubernetes.Interface, apiextensionsclient.Interface,
		migrationclient.StorageVersionMigrationsGetter, error)
	skipRemoveCRDs                bool
	controlPlaneNodeLabelSelector string
	deploymentReplicas            int32
	operatorNamespace             string
	enableSyncLabels              bool
}

type clusterManagerReconcile interface {
	reconcile(ctx context.Context, cm *operatorapiv1.ClusterManager, config manifests.HubConfig) (*operatorapiv1.ClusterManager, reconcileState, error)
	clean(ctx context.Context, cm *operatorapiv1.ClusterManager, config manifests.HubConfig) (*operatorapiv1.ClusterManager, reconcileState, error)
}

type reconcileState int64

const (
	reconcileStop reconcileState = iota
	reconcileContinue
)

// NewClusterManagerController construct cluster manager hub controller
func NewClusterManagerController(
	operatorKubeClient kubernetes.Interface,
	operatorKubeconfig *rest.Config,
	clusterManagerClient operatorv1client.ClusterManagerInterface,
	clusterManagerInformer operatorinformer.ClusterManagerInformer,
	deploymentInformer appsinformer.DeploymentInformer,
	configMapInformer corev1informers.ConfigMapInformer,
	skipRemoveCRDs bool,
	controlPlaneNodeLabelSelector string,
	deploymentReplicas int32,
	operatorNamespace string,
	enableSyncLabels bool,
) factory.Controller {
	controller := &clusterManagerController{
		operatorKubeClient: operatorKubeClient,
		operatorKubeconfig: operatorKubeconfig,
		patcher: patcher.NewPatcher[
			*operatorapiv1.ClusterManager, operatorapiv1.ClusterManagerSpec, operatorapiv1.ClusterManagerStatus](
			clusterManagerClient),
		clusterManagerLister:          clusterManagerInformer.Lister(),
		configMapLister:               configMapInformer.Lister(),
		generateHubClusterClients:     generateHubClients,
		ensureSAKubeconfigs:           ensureSAKubeconfigs,
		cache:                         resourceapply.NewResourceCache(),
		skipRemoveCRDs:                skipRemoveCRDs,
		controlPlaneNodeLabelSelector: controlPlaneNodeLabelSelector,
		deploymentReplicas:            deploymentReplicas,
		operatorNamespace:             operatorNamespace,
		enableSyncLabels:              enableSyncLabels,
	}

	return factory.New().WithSync(controller.sync).
		ResyncEvery(3*time.Minute).
		WithInformersQueueKeysFunc(helpers.ClusterManagerDeploymentQueueKeyFunc(controller.clusterManagerLister), deploymentInformer.Informer()).
		WithFilteredEventsInformersQueueKeysFunc(
			helpers.ClusterManagerQueueKeyFunc(controller.clusterManagerLister),
			queue.FilterByNames(helpers.CaBundleConfigmap),
			configMapInformer.Informer()).
		WithInformersQueueKeysFunc(queue.QueueKeyByMetaName, clusterManagerInformer.Informer()).
		ToController("ClusterManagerController")
}

func (n *clusterManagerController) sync(ctx context.Context, controllerContext factory.SyncContext, clusterManagerName string) error {
	logger := klog.FromContext(ctx).WithValues("clusterManager", clusterManagerName)
	logger.V(4).Info("Reconciling ClusterManager")

	originalClusterManager, err := n.clusterManagerLister.Get(clusterManagerName)
	if errors.IsNotFound(err) {
		// ClusterManager not found, could have been deleted, do nothing.
		return nil
	}
	if err != nil {
		return err
	}
	clusterManager := originalClusterManager.DeepCopy()
	clusterManagerMode := clusterManager.Spec.DeployOption.Mode
	clusterManagerNamespace := helpers.ClusterManagerNamespace(clusterManagerName, clusterManagerMode)

	resourceRequirements, err := helpers.ResourceRequirements(ctx, clusterManager)
	if err != nil {
		logger.Error(err, "failed to parse resource requirements for cluster manager")
		return err
	}

	// default driver is kube
	workDriver := operatorapiv1.WorkDriverTypeKube
	if clusterManager.Spec.WorkConfiguration != nil && clusterManager.Spec.WorkConfiguration.WorkDriver != "" {
		workDriver = clusterManager.Spec.WorkConfiguration.WorkDriver
	}

	replica := n.deploymentReplicas
	if replica <= 0 {
		replica = helpers.DetermineReplica(ctx, n.operatorKubeClient, clusterManager.Spec.DeployOption.Mode, n.controlPlaneNodeLabelSelector)
	}

	// This config is used to render template of manifests.
	config := manifests.HubConfig{
		ClusterManagerName:      clusterManager.Name,
		ClusterManagerNamespace: clusterManagerNamespace,
		OperatorNamespace:       n.operatorNamespace,
		RegistrationImage:       clusterManager.Spec.RegistrationImagePullSpec,
		WorkImage:               clusterManager.Spec.WorkImagePullSpec,
		PlacementImage:          clusterManager.Spec.PlacementImagePullSpec,
		AddOnManagerImage:       clusterManager.Spec.AddOnManagerImagePullSpec,
		Replica:                 replica,
		HostedMode:              clusterManager.Spec.DeployOption.Mode == operatorapiv1.InstallModeHosted,
		RegistrationWebhook: manifests.Webhook{
			Port: defaultWebhookPort,
		},
		WorkWebhook: manifests.Webhook{
			Port: defaultWebhookPort,
		},
		AddonWebhook: manifests.Webhook{
			Port: defaultWebhookPort,
		},
		ResourceRequirementResourceType: helpers.ResourceType(clusterManager),
		ResourceRequirements:            resourceRequirements,
		WorkDriver:                      string(workDriver),
	}

	var registrationFeatureMsgs, workFeatureMsgs, addonFeatureMsgs string
	// If there are some invalid feature gates of registration or work, will output
	// condition `ValidFeatureGates` False in ClusterManager.
	var registrationFeatureGates []operatorapiv1.FeatureGate
	if clusterManager.Spec.RegistrationConfiguration != nil {
		registrationFeatureGates = clusterManager.Spec.RegistrationConfiguration.FeatureGates
		config.AutoApproveUsers = strings.Join(clusterManager.Spec.RegistrationConfiguration.AutoApproveUsers, ",")
	}
	config.RegistrationFeatureGates, registrationFeatureMsgs = helpers.ConvertToFeatureGateFlags("Registration",
		registrationFeatureGates, ocmfeature.DefaultHubRegistrationFeatureGates)
	config.ClusterProfileEnabled = helpers.FeatureGateEnabled(registrationFeatureGates, ocmfeature.DefaultHubRegistrationFeatureGates, ocmfeature.ClusterProfile)
	// setting for cluster importer.
	// TODO(qiujian16) since this is disabled by feature gate, the image is obtained from cluster manager's env var. Need a more elegant approach.
	config.ClusterImporterEnabled = helpers.FeatureGateEnabled(registrationFeatureGates, ocmfeature.DefaultHubRegistrationFeatureGates, ocmfeature.ClusterImporter)
	if config.ClusterImporterEnabled {
		config.AgentImage = os.Getenv("AGENT_IMAGE")
	}

	var workFeatureGates []operatorapiv1.FeatureGate
	if clusterManager.Spec.WorkConfiguration != nil {
		workFeatureGates = clusterManager.Spec.WorkConfiguration.FeatureGates
	}
	config.WorkFeatureGates, workFeatureMsgs = helpers.ConvertToFeatureGateFlags("Work", workFeatureGates, ocmfeature.DefaultHubWorkFeatureGates)
	// start work controller if ManifestWorkReplicaSet or CleanUpCompletedManifestWork is enabled
	config.WorkControllerEnabled = helpers.FeatureGateEnabled(workFeatureGates, ocmfeature.DefaultHubWorkFeatureGates, ocmfeature.ManifestWorkReplicaSet) ||
		helpers.FeatureGateEnabled(workFeatureGates, ocmfeature.DefaultHubWorkFeatureGates, ocmfeature.CleanUpCompletedManifestWork)
	config.CloudEventsDriverEnabled = helpers.FeatureGateEnabled(workFeatureGates, ocmfeature.DefaultHubWorkFeatureGates, ocmfeature.CloudEventsDrivers)

	var addonFeatureGates []operatorapiv1.FeatureGate
	if clusterManager.Spec.AddOnManagerConfiguration != nil {
		addonFeatureGates = clusterManager.Spec.AddOnManagerConfiguration.FeatureGates
	}
	_, addonFeatureMsgs = helpers.ConvertToFeatureGateFlags("Addon", addonFeatureGates, ocmfeature.DefaultHubAddonManagerFeatureGates)
	featureGateCondition := helpers.BuildFeatureCondition(registrationFeatureMsgs, workFeatureMsgs, addonFeatureMsgs)

	// Check if addon management is enabled by the feature gate
	config.AddOnManagerEnabled = helpers.FeatureGateEnabled(addonFeatureGates, ocmfeature.DefaultHubAddonManagerFeatureGates, ocmfeature.AddonManagement)

	// Compute and populate the value of managed cluster identity creator role to be used in cluster manager registration service account
	config.ManagedClusterIdentityCreatorRole = getIdentityCreatorRoleAndTags(*clusterManager)

	// If we are deploying in the hosted mode, it requires us to create webhook in a different way with the default mode.
	// In the hosted mode, the webhook servers is running in the management cluster but the users are accessing the hub cluster.
	// So we need to add configuration to make the apiserver of the hub cluster could access the webhook servers on the management cluster.
	if clusterManager.Spec.DeployOption.Hosted != nil {
		config.RegistrationWebhook = convertWebhookConfiguration(clusterManager.Spec.DeployOption.Hosted.RegistrationWebhookConfiguration)
		config.WorkWebhook = convertWebhookConfiguration(clusterManager.Spec.DeployOption.Hosted.WorkWebhookConfiguration)
	}

	config.Labels = helpers.GetClusterManagerHubLabels(clusterManager, n.enableSyncLabels)
	config.LabelsString = helpers.GetRegistrationLabelString(config.Labels)

	// Determine if the gRPC auth is enabled
	config.GRPCAuthEnabled = helpers.GRPCAuthEnabled(clusterManager)

	// Get gRPC endpoint type
	if config.GRPCAuthEnabled {
		config.GRPCEndpointType = helpers.GRPCServerEndpointType(clusterManager)
	}

	// Update finalizer at first
	if clusterManager.DeletionTimestamp.IsZero() {
		updated, err := n.patcher.AddFinalizer(ctx, clusterManager, clusterManagerFinalizer)
		if updated {
			return err
		}
	}

	// Get clients of the hub cluster and the management cluster
	hubKubeConfig, err := helpers.GetHubKubeconfig(ctx, n.operatorKubeconfig, n.operatorKubeClient, clusterManagerName, clusterManagerMode)
	if err != nil {
		return err
	}
	hubClient, hubApiExtensionClient, hubMigrationClient, err := n.generateHubClusterClients(hubKubeConfig)
	if err != nil {
		return err
	}
	managementClient := n.operatorKubeClient // We assume that operator is always running on the management cluster.

	var errs []error
	reconcilers := []clusterManagerReconcile{
		&crdReconcile{cache: n.cache, recorder: controllerContext.Recorder(), hubAPIExtensionClient: hubApiExtensionClient,
			hubMigrationClient: hubMigrationClient, skipRemoveCRDs: n.skipRemoveCRDs},
		&secretReconcile{cache: n.cache, recorder: controllerContext.Recorder(), operatorKubeClient: n.operatorKubeClient,
			hubKubeClient: hubClient, operatorNamespace: n.operatorNamespace, enableSyncLabels: n.enableSyncLabels},
		&hubReconcile{cache: n.cache, recorder: controllerContext.Recorder(), hubKubeClient: hubClient},
		&runtimeReconcile{cache: n.cache, recorder: controllerContext.Recorder(), hubKubeConfig: hubKubeConfig, hubKubeClient: hubClient,
			kubeClient: managementClient, ensureSAKubeconfigs: n.ensureSAKubeconfigs},
		&webhookReconcile{cache: n.cache, recorder: controllerContext.Recorder(), hubKubeClient: hubClient, kubeClient: managementClient},
	}

	// If the ClusterManager is deleting, we remove its related resources on hub
	if !clusterManager.DeletionTimestamp.IsZero() {
		for _, reconciler := range reconcilers {
			// TODO should handle requeu error differently
			clusterManager, _, err = reconciler.clean(ctx, clusterManager, config)
			if err != nil {
				return err
			}
		}
		return n.patcher.RemoveFinalizer(ctx, clusterManager, clusterManagerFinalizer)
	}

	// Create the hub namespace FIRST, before waiting for the CA bundle.
	// This is required because the cert rotation controller needs the namespace to exist
	// before it can create the ca-bundle-configmap. If we wait for the CA bundle before
	// creating the namespace, we would have a deadlock.
	if err := ensureNamespace(ctx, hubClient, clusterManagerNamespace); err != nil {
		return err
	}

	// get caBundle from the ConfigMap created by the cert rotation controller.
	// The cert rotation controller must create this ConfigMap before we can proceed,
	// otherwise the CRDs will be created with an invalid "placeholder" CA bundle,
	// causing webhook conversion to fail and the CRDs to not become Established.
	configmap, err := n.configMapLister.ConfigMaps(clusterManagerNamespace).Get(helpers.CaBundleConfigmap)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.V(4).Info("CA bundle configmap not yet available, will retry",
				"namespace", clusterManagerNamespace, "configmap", helpers.CaBundleConfigmap)
			controllerContext.Queue().AddAfter(clusterManagerName, 3*time.Second)
			return nil
		}
		return err
	}
	caBundle := configmap.Data["ca-bundle.crt"]
	if len(caBundle) == 0 {
		return fmt.Errorf("CA bundle configmap %s/%s exists but ca-bundle.crt is empty",
			clusterManagerNamespace, helpers.CaBundleConfigmap)
	}
	encodedCaBundle := base64.StdEncoding.EncodeToString([]byte(caBundle))
	config.RegistrationAPIServiceCABundle = encodedCaBundle
	config.WorkAPIServiceCABundle = encodedCaBundle
	config.AddonAPIServiceCABundle = encodedCaBundle

	// check imagePulSecret here because there will be a warning event FailedToRetrieveImagePullSecret
	// if imagePullSecret does not exist.
	if config.ImagePullSecret, err = n.getImagePullSecret(ctx); err != nil {
		// may meet permission error in the upgrade case when the new rbac is not upgraded, so ignore err in this release.
		// TODO: need return err if fail to get secret in the next release.
		klog.Warningf("failed to get image pull secret: %v", err)
	}

	for _, reconciler := range reconcilers {
		var state reconcileState
		var rqe commonhelper.RequeueError
		clusterManager, state, err = reconciler.reconcile(ctx, clusterManager, config)
		if errorhelpers.As(err, &rqe) {
			controllerContext.Queue().AddAfter(clusterManagerName, rqe.RequeueTime)
		} else if err != nil {
			errs = append(errs, err)
		}
		if state == reconcileStop {
			break
		}
	}

	// Update status
	meta.SetStatusCondition(&clusterManager.Status.Conditions, featureGateCondition)
	clusterManager.Status.ObservedGeneration = clusterManager.Generation
	if len(errs) == 0 {
		meta.SetStatusCondition(&clusterManager.Status.Conditions, metav1.Condition{
			Type:    operatorapiv1.ConditionClusterManagerApplied,
			Status:  metav1.ConditionTrue,
			Reason:  operatorapiv1.ReasonClusterManagerApplied,
			Message: "Components of cluster manager are applied",
		})
	} else {
		// When appliedCondition is false, we should not update related resources and resource generations
		clusterManager.Status.RelatedResources = originalClusterManager.Status.RelatedResources
		clusterManager.Status.Generations = originalClusterManager.Status.Generations
	}

	_, updatedErr := n.patcher.PatchStatus(ctx, clusterManager, clusterManager.Status, originalClusterManager.Status)
	if updatedErr != nil {
		errs = append(errs, updatedErr)
	}

	return utilerrors.NewAggregate(errs)
}

func generateHubClients(hubKubeConfig *rest.Config) (kubernetes.Interface, apiextensionsclient.Interface,
	migrationclient.StorageVersionMigrationsGetter, error) {
	hubClient, err := kubernetes.NewForConfig(hubKubeConfig)
	if err != nil {
		return nil, nil, nil, err
	}
	hubApiExtensionClient, err := apiextensionsclient.NewForConfig(hubKubeConfig)
	if err != nil {
		return nil, nil, nil, err
	}

	hubMigrationClient, err := migrationclient.NewForConfig(hubKubeConfig)
	if err != nil {
		return nil, nil, nil, err
	}
	return hubClient, hubApiExtensionClient, hubMigrationClient, nil
}

// ensureSAKubeconfigs is used to create a kubeconfig with a token from a ServiceAccount.
// We create a ServiceAccount with a rolebinding on the hub cluster, and then use the token of the ServiceAccount as the user of the kubeconfig.
// Finally, a deployment on the management cluster would use the kubeconfig to access resources on the hub cluster.
func ensureSAKubeconfigs(ctx context.Context, clusterManagerName, clusterManagerNamespace string,
	hubKubeConfig *rest.Config, hubClient, managementClient kubernetes.Interface, recorder events.Recorder,
	workControllerEnabled, addonManagerEnabled, grpcAuthEnabled bool) error {
	for _, sa := range getSAs(workControllerEnabled, addonManagerEnabled, grpcAuthEnabled) {
		tokenGetter := helpers.SATokenGetter(ctx, sa, clusterManagerNamespace, hubClient)
		err := helpers.SyncKubeConfigSecret(ctx, sa+"-kubeconfig", clusterManagerNamespace,
			"/var/run/secrets/hub/kubeconfig", &rest.Config{
				Host: hubKubeConfig.Host,
				TLSClientConfig: rest.TLSClientConfig{
					CAData: hubKubeConfig.CAData,
				},
			}, managementClient.CoreV1(), tokenGetter, recorder, nil)
		if err != nil {
			return err
		}
	}
	return nil
}

// ensureNamespace creates the namespace if it doesn't exist.
// This is used to create the hub namespace before waiting for the CA bundle,
// so that the cert rotation controller can create the ca-bundle-configmap.
func ensureNamespace(ctx context.Context, kubeClient kubernetes.Interface, namespace string) error {
	_, err := kubeClient.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err == nil {
		// Namespace already exists
		return nil
	}
	if !errors.IsNotFound(err) {
		return err
	}
	// Create the namespace
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}
	_, err = kubeClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil && !errors.IsAlreadyExists(err) {
		return err
	}
	logger := klog.FromContext(ctx)
	logger.V(4).Info("Created namespace for cert rotation controller", "namespace", namespace)
	return nil
}

// TODO: support IPV6 address
func isIPFormat(address string) bool {
	runes := []rune(address)
	for i := 0; i < len(runes); i++ {
		if (runes[i] < '0' || runes[i] > '9') && runes[i] != '.' {
			return false
		}
	}
	return true
}

func convertWebhookConfiguration(webhookConfiguration operatorapiv1.HostedWebhookConfiguration) manifests.Webhook {
	return manifests.Webhook{
		Address:    webhookConfiguration.Address,
		Port:       webhookConfiguration.Port,
		IsIPFormat: isIPFormat(webhookConfiguration.Address),
	}
}

// clean specified resources
func cleanResources(ctx context.Context, kubeClient kubernetes.Interface, cm *operatorapiv1.ClusterManager,
	config manifests.HubConfig, resources ...string) (*operatorapiv1.ClusterManager, reconcileState, error) {
	for _, file := range resources {
		err := helpers.CleanUpStaticObject(
			ctx,
			kubeClient, nil, nil,
			func(name string) ([]byte, error) {
				template, err := manifests.ClusterManagerManifestFiles.ReadFile(name)
				if err != nil {
					return nil, err
				}
				objData := assets.MustCreateAssetFromTemplate(name, template, config).Data
				helpers.RemoveRelatedResourcesStatusesWithObj(ctx, &cm.Status.RelatedResources, objData)
				return objData, nil
			},
			file,
		)
		if err != nil {
			return cm, reconcileContinue, err
		}
	}
	return cm, reconcileContinue, nil
}

func (n *clusterManagerController) getImagePullSecret(ctx context.Context) (string, error) {
	_, err := n.operatorKubeClient.CoreV1().Secrets(n.operatorNamespace).Get(ctx, helpers.ImagePullSecret, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}

	return helpers.ImagePullSecret, nil
}

func getIdentityCreatorRoleAndTags(cm operatorapiv1.ClusterManager) string {
	if cm.Spec.RegistrationConfiguration != nil {
		for _, registrationDriver := range cm.Spec.RegistrationConfiguration.RegistrationDrivers {
			if registrationDriver.AuthType == operatorapiv1.AwsIrsaAuthType && registrationDriver.AwsIrsa != nil {
				hubClusterArn := registrationDriver.AwsIrsa.HubClusterArn
				hubClusterAccountId, hubClusterName := commonhelper.GetAwsAccountIdAndClusterName(hubClusterArn)
				return "arn:aws:iam::" + hubClusterAccountId + ":role/" + hubClusterName + "_managed-cluster-identity-creator"
			}
		}
	}
	return ""
}
