package clustermanagercontroller

import (
	"context"
	"encoding/base64"
	errorhelpers "errors"
	"strings"
	"time"

	"github.com/openshift/library-go/pkg/assets"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
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
	recorder             events.Recorder
	cache                resourceapply.ResourceCache
	// For testcases which don't need these functions, we could set fake funcs
	ensureSAKubeconfigs func(ctx context.Context, clusterManagerName, clusterManagerNamespace string,
		hubConfig *rest.Config, hubClient, managementClient kubernetes.Interface, recorder events.Recorder,
		mwctrEnabled, addonManagerEnabled bool) error
	generateHubClusterClients func(hubConfig *rest.Config) (kubernetes.Interface, apiextensionsclient.Interface,
		migrationclient.StorageVersionMigrationsGetter, error)
	skipRemoveCRDs                bool
	controlPlaneNodeLabelSelector string
	deploymentReplicas            int32
	operatorNamespace             string
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
	recorder events.Recorder,
	skipRemoveCRDs bool,
	controlPlaneNodeLabelSelector string,
	deploymentReplicas int32,
	operatorNamespace string,
) factory.Controller {
	controller := &clusterManagerController{
		operatorKubeClient: operatorKubeClient,
		operatorKubeconfig: operatorKubeconfig,
		patcher: patcher.NewPatcher[
			*operatorapiv1.ClusterManager, operatorapiv1.ClusterManagerSpec, operatorapiv1.ClusterManagerStatus](
			clusterManagerClient),
		clusterManagerLister:          clusterManagerInformer.Lister(),
		configMapLister:               configMapInformer.Lister(),
		recorder:                      recorder,
		generateHubClusterClients:     generateHubClients,
		ensureSAKubeconfigs:           ensureSAKubeconfigs,
		cache:                         resourceapply.NewResourceCache(),
		skipRemoveCRDs:                skipRemoveCRDs,
		controlPlaneNodeLabelSelector: controlPlaneNodeLabelSelector,
		deploymentReplicas:            deploymentReplicas,
		operatorNamespace:             operatorNamespace,
	}

	return factory.New().WithSync(controller.sync).
		ResyncEvery(3*time.Minute).
		WithInformersQueueKeysFunc(helpers.ClusterManagerDeploymentQueueKeyFunc(controller.clusterManagerLister), deploymentInformer.Informer()).
		WithFilteredEventsInformersQueueKeysFunc(
			helpers.ClusterManagerQueueKeyFunc(controller.clusterManagerLister),
			queue.FilterByNames(helpers.CaBundleConfigmap),
			configMapInformer.Informer()).
		WithInformersQueueKeysFunc(queue.QueueKeyByMetaName, clusterManagerInformer.Informer()).
		ToController("ClusterManagerController", recorder)
}

func (n *clusterManagerController) sync(ctx context.Context, controllerContext factory.SyncContext) error {
	clusterManagerName := controllerContext.QueueKey()
	klog.V(4).Infof("Reconciling ClusterManager %q", clusterManagerName)

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

	resourceRequirements, err := helpers.ResourceRequirements(clusterManager)
	if err != nil {
		klog.Errorf("failed to parse resource requirements for cluster manager %s: %v", clusterManager.Name, err)
		return err
	}

	// default driver is kube
	workDriver := operatorapiv1.WorkDriverTypeKube
	if clusterManager.Spec.WorkConfiguration != nil && clusterManager.Spec.WorkConfiguration.WorkDriver != "" {
		workDriver = clusterManager.Spec.WorkConfiguration.WorkDriver
	}

	replica := n.deploymentReplicas
	if replica <= 0 {
		replica = helpers.DetermineReplica(ctx, n.operatorKubeClient, clusterManager.Spec.DeployOption.Mode, nil, n.controlPlaneNodeLabelSelector)
	}

	// This config is used to render template of manifests.
	config := manifests.HubConfig{
		ClusterManagerName:      clusterManager.Name,
		ClusterManagerNamespace: clusterManagerNamespace,
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

	var workFeatureGates []operatorapiv1.FeatureGate
	if clusterManager.Spec.WorkConfiguration != nil {
		workFeatureGates = clusterManager.Spec.WorkConfiguration.FeatureGates
	}
	config.WorkFeatureGates, workFeatureMsgs = helpers.ConvertToFeatureGateFlags("Work", workFeatureGates, ocmfeature.DefaultHubWorkFeatureGates)
	config.MWReplicaSetEnabled = helpers.FeatureGateEnabled(workFeatureGates, ocmfeature.DefaultHubWorkFeatureGates, ocmfeature.ManifestWorkReplicaSet)
	config.CloudEventsDriverEnabled = helpers.FeatureGateEnabled(workFeatureGates, ocmfeature.DefaultHubWorkFeatureGates, ocmfeature.CloudEventsDrivers)

	var addonFeatureGates []operatorapiv1.FeatureGate
	if clusterManager.Spec.AddOnManagerConfiguration != nil {
		addonFeatureGates = clusterManager.Spec.AddOnManagerConfiguration.FeatureGates
	}
	_, addonFeatureMsgs = helpers.ConvertToFeatureGateFlags("Addon", addonFeatureGates, ocmfeature.DefaultHubAddonManagerFeatureGates)
	featureGateCondition := helpers.BuildFeatureCondition(registrationFeatureMsgs, workFeatureMsgs, addonFeatureMsgs)

	// Check if addon management is enabled by the feature gate
	config.AddOnManagerEnabled = helpers.FeatureGateEnabled(addonFeatureGates, ocmfeature.DefaultHubAddonManagerFeatureGates, ocmfeature.AddonManagement)

	// If we are deploying in the hosted mode, it requires us to create webhook in a different way with the default mode.
	// In the hosted mode, the webhook servers is running in the management cluster but the users are accessing the hub cluster.
	// So we need to add configuration to make the apiserver of the hub cluster could access the webhook servers on the management cluster.
	if clusterManager.Spec.DeployOption.Hosted != nil {
		config.RegistrationWebhook = convertWebhookConfiguration(clusterManager.Spec.DeployOption.Hosted.RegistrationWebhookConfiguration)
		config.WorkWebhook = convertWebhookConfiguration(clusterManager.Spec.DeployOption.Hosted.WorkWebhookConfiguration)
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
		&crdReconcile{cache: n.cache, recorder: n.recorder, hubAPIExtensionClient: hubApiExtensionClient,
			hubMigrationClient: hubMigrationClient, skipRemoveCRDs: n.skipRemoveCRDs},
		&secretReconcile{cache: n.cache, recorder: n.recorder, operatorKubeClient: n.operatorKubeClient,
			hubKubeClient: hubClient, operatorNamespace: n.operatorNamespace},
		&hubReconcile{cache: n.cache, recorder: n.recorder, hubKubeClient: hubClient},
		&runtimeReconcile{cache: n.cache, recorder: n.recorder, hubKubeConfig: hubKubeConfig, hubKubeClient: hubClient,
			kubeClient: managementClient, ensureSAKubeconfigs: n.ensureSAKubeconfigs},
		&webhookReconcile{cache: n.cache, recorder: n.recorder, hubKubeClient: hubClient, kubeClient: managementClient},
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

	// get caBundle
	caBundle := "placeholder"
	configmap, err := n.configMapLister.ConfigMaps(clusterManagerNamespace).Get(helpers.CaBundleConfigmap)
	switch {
	case errors.IsNotFound(err):
		// do nothing
	case err != nil:
		return err
	default:
		if cb := configmap.Data["ca-bundle.crt"]; len(cb) > 0 {
			caBundle = cb
		}
	}
	encodedCaBundle := base64.StdEncoding.EncodeToString([]byte(caBundle))
	config.RegistrationAPIServiceCABundle = encodedCaBundle
	config.WorkAPIServiceCABundle = encodedCaBundle

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
	mwctrEnabled, addonManagerEnabled bool) error {
	for _, sa := range getSAs(mwctrEnabled, addonManagerEnabled) {
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

func convertWebhookConfiguration(webhookConfiguration operatorapiv1.WebhookConfiguration) manifests.Webhook {
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
				helpers.RemoveRelatedResourcesStatusesWithObj(&cm.Status.RelatedResources, objData)
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
