package clustermanagercontroller

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"

	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	apiregistrationclient "k8s.io/kube-aggregator/pkg/client/clientset_generated/clientset/typed/apiregistration/v1"

	"github.com/openshift/library-go/pkg/assets"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	operatorhelpers "github.com/openshift/library-go/pkg/operator/v1helpers"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	appsinformer "k8s.io/client-go/informers/apps/v1"
	corev1informers "k8s.io/client-go/informers/core/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"

	operatorv1client "open-cluster-management.io/api/client/operator/clientset/versioned/typed/operator/v1"
	operatorinformer "open-cluster-management.io/api/client/operator/informers/externalversions/operator/v1"
	operatorlister "open-cluster-management.io/api/client/operator/listers/operator/v1"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"
	"open-cluster-management.io/registration-operator/manifests"
	"open-cluster-management.io/registration-operator/pkg/helpers"
)

var (
	crdNames = []string{
		"manifestworks.work.open-cluster-management.io",
		"managedclusters.cluster.open-cluster-management.io",
	}

	namespaceResource = "cluster-manager/cluster-manager-namespace.yaml"

	// crdResourceFiles should be deployed in the hub cluster
	hubCRDResourceFiles = []string{
		"cluster-manager/hub/0000_00_addon.open-cluster-management.io_clustermanagementaddons.crd.yaml",
		"cluster-manager/hub/0000_00_clusters.open-cluster-management.io_managedclusters.crd.yaml",
		"cluster-manager/hub/0000_00_clusters.open-cluster-management.io_managedclustersets.crd.yaml",
		"cluster-manager/hub/0000_00_work.open-cluster-management.io_manifestworks.crd.yaml",
		"cluster-manager/hub/0000_01_addon.open-cluster-management.io_managedclusteraddons.crd.yaml",
		"cluster-manager/hub/0000_01_clusters.open-cluster-management.io_managedclustersetbindings.crd.yaml",
		"cluster-manager/hub/0000_02_clusters.open-cluster-management.io_placements.crd.yaml",
		"cluster-manager/hub/0000_03_clusters.open-cluster-management.io_placementdecisions.crd.yaml",
		"cluster-manager/hub/0000_05_clusters.open-cluster-management.io_addonplacementscores.crd.yaml",
	}

	// The hubWebhookResourceFiles should be deployed in the hub cluster
	// The service should may point to a external url which represent the webhook-server's address.
	hubWebhookResourceFiles = []string{
		// registration-webhook
		"cluster-manager/hub/cluster-manager-registration-webhook-validatingconfiguration.yaml",
		"cluster-manager/hub/cluster-manager-registration-webhook-mutatingconfiguration.yaml",
		"cluster-manager/hub/cluster-manager-registration-webhook-clustersetbinding-validatingconfiguration.yaml",
		// work-webhook
		"cluster-manager/hub/cluster-manager-work-webhook-validatingconfiguration.yaml",
	}

	// The apiservice resources should be applied after CABundle created.
	// And also should be deployed in the hub cluster.
	hubApiserviceFiles = []string{
		"cluster-manager/hub/cluster-manager-work-webhook-apiservice.yaml",
		"cluster-manager/hub/cluster-manager-registration-webhook-apiservice.yaml",
	}

	// The hubHostedWebhookServiceFiles should only be deployed on the hub cluster when the deploy mode is hosted.
	hubDefaultWebhookServiceFiles = []string{
		"cluster-manager/hub/cluster-manager-registration-webhook-service.yaml",
		"cluster-manager/hub/cluster-manager-work-webhook-service.yaml",
	}
	hubHostedWebhookServiceFiles = []string{
		"cluster-manager/hub/cluster-manager-registration-webhook-service-hosted.yaml",
		"cluster-manager/hub/cluster-manager-work-webhook-service-hosted.yaml",
	}
	// hubHostedWebhookEndpointFiles only apply when the deploy mode is hosted and address is IPFormat.
	hubHostedWebhookEndpointRegistration = "cluster-manager/hub/cluster-manager-registration-webhook-endpoint-hosted.yaml"
	hubHostedWebhookEndpointWork         = "cluster-manager/hub/cluster-manager-work-webhook-endpoint-hosted.yaml"

	// The hubRbacResourceFiles should be deployed in the hub cluster.
	hubRbacResourceFiles = []string{
		// registration
		"cluster-manager/hub/cluster-manager-registration-clusterrole.yaml",
		"cluster-manager/hub/cluster-manager-registration-clusterrolebinding.yaml",
		"cluster-manager/hub/cluster-manager-registration-serviceaccount.yaml",
		// registration-webhook
		"cluster-manager/hub/cluster-manager-registration-webhook-clusterrole.yaml",
		"cluster-manager/hub/cluster-manager-registration-webhook-clusterrolebinding.yaml",
		"cluster-manager/hub/cluster-manager-registration-webhook-serviceaccount.yaml",
		// work-webhook
		"cluster-manager/hub/cluster-manager-work-webhook-clusterrole.yaml",
		"cluster-manager/hub/cluster-manager-work-webhook-clusterrolebinding.yaml",
		"cluster-manager/hub/cluster-manager-work-webhook-serviceaccount.yaml",
		// placement
		"cluster-manager/hub/cluster-manager-placement-clusterrole.yaml",
		"cluster-manager/hub/cluster-manager-placement-clusterrolebinding.yaml",
		"cluster-manager/hub/cluster-manager-placement-serviceaccount.yaml",
	}

	// All deployments should be deployed in the management cluster.
	deploymentFiles = []string{
		"cluster-manager/management/cluster-manager-registration-deployment.yaml",
		"cluster-manager/management/cluster-manager-registration-webhook-deployment.yaml",
		"cluster-manager/management/cluster-manager-work-webhook-deployment.yaml",
		"cluster-manager/management/cluster-manager-placement-deployment.yaml",
	}
)

const (
	clusterManagerFinalizer = "operator.open-cluster-management.io/cluster-manager-cleanup"
	clusterManagerApplied   = "Applied"
	caBundleConfigmap       = "ca-bundle-configmap"

	hubRegistrationFeatureGatesValid = "ValidRegistrationFeatureGates"
)

type clusterManagerController struct {
	clusterManagerClient operatorv1client.ClusterManagerInterface
	clusterManagerLister operatorlister.ClusterManagerLister
	operatorKubeClient   kubernetes.Interface
	operatorKubeconfig   *rest.Config
	configMapLister      corev1listers.ConfigMapLister
	recorder             events.Recorder
	cache                resourceapply.ResourceCache
	// For testcases which don't need these functions, we could set fake funcs
	generateHubClusterClients func(hubConfig *rest.Config) (kubernetes.Interface, apiextensionsclient.Interface, apiregistrationclient.APIServicesGetter, error)
	ensureSAKubeconfigs       func(ctx context.Context, clusterManagerName, clusterManagerNamespace string, hubConfig *rest.Config, hubClient, managementClient kubernetes.Interface, recorder events.Recorder) error
}

// NewClusterManagerController construct cluster manager hub controller
func NewClusterManagerController(
	operatorKubeClient kubernetes.Interface,
	operatorKubeconfig *rest.Config,
	clusterManagerClient operatorv1client.ClusterManagerInterface,
	clusterManagerInformer operatorinformer.ClusterManagerInformer,
	deploymentInformer appsinformer.DeploymentInformer,
	configMapInformer corev1informers.ConfigMapInformer,
	recorder events.Recorder,
) factory.Controller {
	controller := &clusterManagerController{
		operatorKubeClient:        operatorKubeClient,
		operatorKubeconfig:        operatorKubeconfig,
		clusterManagerClient:      clusterManagerClient,
		clusterManagerLister:      clusterManagerInformer.Lister(),
		configMapLister:           configMapInformer.Lister(),
		recorder:                  recorder,
		generateHubClusterClients: generateHubClients,
		ensureSAKubeconfigs:       ensureSAKubeconfigs,
		cache:                     resourceapply.NewResourceCache(),
	}

	return factory.New().WithSync(controller.sync).
		ResyncEvery(3*time.Minute).
		WithInformersQueueKeyFunc(helpers.ClusterManagerDeploymentQueueKeyFunc(controller.clusterManagerLister), deploymentInformer.Informer()).
		WithFilteredEventsInformersQueueKeyFunc(
			helpers.ClusterManagerConfigmapQueueKeyFunc(controller.clusterManagerLister),
			func(obj interface{}) bool {
				accessor, _ := meta.Accessor(obj)
				if name := accessor.GetName(); name != caBundleConfigmap {
					return false
				}
				return true
			},
			configMapInformer.Informer()).
		WithInformersQueueKeyFunc(func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			return accessor.GetName()
		}, clusterManagerInformer.Informer()).
		ToController("ClusterManagerController", recorder)
}

func (n *clusterManagerController) sync(ctx context.Context, controllerContext factory.SyncContext) error {
	clusterManagerName := controllerContext.QueueKey()
	klog.V(4).Infof("Reconciling ClusterManager %q", clusterManagerName)

	clusterManager, err := n.clusterManagerLister.Get(clusterManagerName)
	if errors.IsNotFound(err) {
		// ClusterManager not found, could have been deleted, do nothing.
		return nil
	}
	if err != nil {
		return err
	}
	clusterManager = clusterManager.DeepCopy()
	clusterManagerMode := clusterManager.Spec.DeployOption.Mode
	clusterManagerNamespace := helpers.ClusterManagerNamespace(clusterManagerName, clusterManagerMode)

	// This config is used to render template of manifests.
	config := manifests.HubConfig{
		ClusterManagerName:      clusterManager.Name,
		ClusterManagerNamespace: clusterManagerNamespace,
		RegistrationImage:       clusterManager.Spec.RegistrationImagePullSpec,
		WorkImage:               clusterManager.Spec.WorkImagePullSpec,
		PlacementImage:          clusterManager.Spec.PlacementImagePullSpec,
		Replica:                 helpers.DetermineReplica(ctx, n.operatorKubeClient, clusterManagerMode, nil),
		HostedMode:              clusterManager.Spec.DeployOption.Mode == operatorapiv1.InstallModeHosted,
	}

	conditions := &clusterManager.Status.Conditions

	// If there are some invalid feature gates of registration, will output condition `InvalidRegistrationFeatureGates` in ClusterManager.
	if clusterManager.Spec.RegistrationConfiguration != nil && len(clusterManager.Spec.RegistrationConfiguration.FeatureGates) > 0 {
		featureGateArgs, invalidFeatureGates := helpers.FeatureGatesArgs(
			clusterManager.Spec.RegistrationConfiguration.FeatureGates, helpers.ComponentHubKey)
		if len(invalidFeatureGates) == 0 {
			config.RegistrationFeatureGates = featureGateArgs
			meta.SetStatusCondition(conditions, metav1.Condition{
				Type:    hubRegistrationFeatureGatesValid,
				Status:  metav1.ConditionTrue,
				Reason:  "FeatureGatesAllValid",
				Message: "Registration feature gates of cluster manager are all valid",
			})
		} else {
			invalidFGErr := fmt.Errorf("there are some invalid feature gates of registration: %v ", invalidFeatureGates)
			_, _, updateError := helpers.UpdateClusterManagerStatus(ctx, n.clusterManagerClient, clusterManagerName, helpers.UpdateClusterManagerConditionFn(metav1.Condition{
				Type: hubRegistrationFeatureGatesValid, Status: metav1.ConditionFalse, Reason: "InvalidFeatureGatesExisting",
				Message: invalidFGErr.Error(),
			}))
			return updateError
		}
	}

	// If we are deploying in the hosted mode, it requires us to create webhook in a different way with the default mode.
	// In the hosted mode, the webhook servers is running in the management cluster but the users are accessing the hub cluster.
	// So we need to add configuration to make the apiserver of the hub cluster could access the webhook servers on the management cluster.
	if clusterManager.Spec.DeployOption.Hosted != nil {
		config.RegistrationWebhook = convertWebhookConfiguration(clusterManager.Spec.DeployOption.Hosted.RegistrationWebhookConfiguration)
		config.WorkWebhook = convertWebhookConfiguration(clusterManager.Spec.DeployOption.Hosted.WorkWebhookConfiguration)
	}

	// Update finalizer at first
	if clusterManager.DeletionTimestamp.IsZero() {
		hasFinalizer := false
		for i := range clusterManager.Finalizers {
			if clusterManager.Finalizers[i] == clusterManagerFinalizer {
				hasFinalizer = true
				break
			}
		}
		if !hasFinalizer {
			clusterManager.Finalizers = append(clusterManager.Finalizers, clusterManagerFinalizer)
			_, err := n.clusterManagerClient.Update(ctx, clusterManager, metav1.UpdateOptions{})
			return err
		}
	}

	// Get clients of the hub cluster and the management cluster
	hubKubeConfig, err := helpers.GetHubKubeconfig(ctx, n.operatorKubeconfig, n.operatorKubeClient, clusterManagerName, clusterManagerMode)
	if err != nil {
		return err
	}
	hubClient, hubApiExtensionClient, hubApiRegistrationClient, err := n.generateHubClusterClients(hubKubeConfig)
	if err != nil {
		return err
	}
	managementClient := n.operatorKubeClient // We assume that operator is always running on the management cluster.

	// If the ClusterManager is deleting, we remove its related resources on hub
	if !clusterManager.DeletionTimestamp.IsZero() {
		if err := cleanUpHub(ctx, controllerContext, clusterManagerMode, hubClient, hubApiExtensionClient, hubApiRegistrationClient, config); err != nil {
			return err
		}
		if err := cleanUpManagement(ctx, controllerContext, managementClient, config); err != nil {
			return err
		}
		return removeClusterManagerFinalizer(ctx, n.clusterManagerClient, clusterManager)
	}

	var relatedResources []operatorapiv1.RelatedResourceMeta

	// Apply resources on the hub cluster
	hubAppliedErrs, err := applyHubResources(
		ctx,
		clusterManagerNamespace,
		clusterManagerMode,
		config,
		&relatedResources,
		hubClient, hubApiExtensionClient, hubApiRegistrationClient,
		n.configMapLister, n.recorder, n.cache)
	if err != nil {
		return err
	}

	// Apply resources on the management cluster
	currentGenerations, managementAppliedErrs, err := applyManagementResources(
		ctx,
		clusterManager,
		config,
		&relatedResources,
		hubClient, hubKubeConfig,
		managementClient, n.recorder, n.cache, n.ensureSAKubeconfigs,
	)
	if err != nil {
		return err
	}

	// Update status
	errs := append(hubAppliedErrs, managementAppliedErrs...)
	observedGeneration := clusterManager.Status.ObservedGeneration
	if len(errs) == 0 {
		meta.SetStatusCondition(conditions, metav1.Condition{
			Type:    clusterManagerApplied,
			Status:  metav1.ConditionTrue,
			Reason:  "ClusterManagerApplied",
			Message: "Components of cluster manager are applied",
		})
		observedGeneration = clusterManager.Generation
	} else {
		meta.SetStatusCondition(conditions, metav1.Condition{
			Type:    clusterManagerApplied,
			Status:  metav1.ConditionFalse,
			Reason:  "ClusterManagerApplyFailed",
			Message: "Components of cluster manager fail to be applied",
		})
	}

	_, _, updatedErr := helpers.UpdateClusterManagerStatus(
		ctx, n.clusterManagerClient, clusterManager.Name,
		helpers.UpdateClusterManagerConditionFn(*conditions...),
		helpers.UpdateClusterManagerGenerationsFn(currentGenerations...),
		helpers.UpdateClusterManagerRelatedResourcesFn(relatedResources...),
		func(oldStatus *operatorapiv1.ClusterManagerStatus) error {
			oldStatus.ObservedGeneration = observedGeneration
			return nil
		},
	)
	if updatedErr != nil {
		errs = append(errs, updatedErr)
	}

	return operatorhelpers.NewMultiLineAggregate(errs)
}

func applyHubResources(
	ctx context.Context,
	clusterManagerNamespace string,
	clusterManagerMode operatorapiv1.InstallMode,
	manifestsConfig manifests.HubConfig, // used to render templates
	relatedResources *[]operatorapiv1.RelatedResourceMeta,
	// hub clients
	hubClient kubernetes.Interface, hubApiExtensionClient apiextensionsclient.Interface, hubApiRegistrationClient apiregistrationclient.APIServicesGetter,
	configMapLister corev1listers.ConfigMapLister,
	recorder events.Recorder,
	cache resourceapply.ResourceCache,
) (appliedErrs []error, err error) {
	// Apply hub cluster resources
	hubResources := getHubResources(clusterManagerMode, manifestsConfig.RegistrationWebhook.IsIPFormat, manifestsConfig.WorkWebhook.IsIPFormat)
	resourceResults := helpers.ApplyDirectly(
		ctx,
		hubClient,
		hubApiExtensionClient,
		hubApiRegistrationClient,
		nil,
		recorder,
		cache,
		func(name string) ([]byte, error) {
			template, err := manifests.ClusterManagerManifestFiles.ReadFile(name)
			if err != nil {
				return nil, err
			}
			objData := assets.MustCreateAssetFromTemplate(name, template, manifestsConfig).Data
			helpers.SetRelatedResourcesStatusesWithObj(relatedResources, objData)
			return objData, nil
		},
		hubResources...,
	)
	for _, result := range resourceResults {
		if result.Error != nil {
			appliedErrs = append(appliedErrs, fmt.Errorf("%q (%T): %v", result.File, result.Type, result.Error))
		}
	}

	// Try to load ca bundle from configmap
	// If the configmap is found, populate it into configmap.
	// If the configmap not found yet, skip this and apply other resources first.
	caBundle := "placeholder"
	configmap, err := configMapLister.ConfigMaps(clusterManagerNamespace).Get(caBundleConfigmap)
	switch {
	case errors.IsNotFound(err):
		// do nothing
	case err != nil:
		return appliedErrs, err
	default:
		if cb := configmap.Data["ca-bundle.crt"]; len(cb) > 0 {
			caBundle = cb
		}
	}
	encodedCaBundle := base64.StdEncoding.EncodeToString([]byte(caBundle))
	manifestsConfig.RegistrationAPIServiceCABundle = encodedCaBundle
	manifestsConfig.WorkAPIServiceCABundle = encodedCaBundle

	// Apply Apiservice files to hub cluster.
	// The reason why apply Apiservice after apply other staticfiles(including namespace) is because Apiservices requires the CABundleConfigmap.
	// And it will return an error(uncatchable with NotFound type) if the namespace is not created.
	apiserviceResults := helpers.ApplyDirectly(
		ctx,
		hubClient,
		hubApiExtensionClient,
		hubApiRegistrationClient,
		nil,
		recorder,
		cache,
		func(name string) ([]byte, error) {
			template, err := manifests.ClusterManagerManifestFiles.ReadFile(name)
			if err != nil {
				return nil, err
			}
			objData := assets.MustCreateAssetFromTemplate(name, template, manifestsConfig).Data
			helpers.SetRelatedResourcesStatusesWithObj(relatedResources, objData)
			return objData, nil
		},
		hubApiserviceFiles...,
	)
	for _, result := range apiserviceResults {
		if result.Error != nil {
			appliedErrs = append(appliedErrs, fmt.Errorf("%q (%T): %v", result.File, result.Type, result.Error))
		}
	}

	return appliedErrs, nil
}

func applyManagementResources(
	ctx context.Context,
	clusterManager *operatorapiv1.ClusterManager,
	manifestsConfig manifests.HubConfig,
	relatedResources *[]operatorapiv1.RelatedResourceMeta,
	hubClient kubernetes.Interface, hubKubeConfig *rest.Config,
	managementKubeClient kubernetes.Interface,
	recorder events.Recorder,
	cache resourceapply.ResourceCache,
	ensureSAKubeconfigs func(ctx context.Context, clusterManagerName, clusterManagerNamespace string, hubConfig *rest.Config, hubClient, managementClient kubernetes.Interface, recorder events.Recorder) error,
) (currentGenerations []operatorapiv1.GenerationStatus, appliedErrs []error, err error) {
	// Apply management cluster resources(namespace and deployments).
	// Note: the certrotation-controller will create CABundle after the namespace applied.
	// And CABundle is used to render apiservice resources.
	managementResources := []string{namespaceResource}
	resourceResults := helpers.ApplyDirectly(
		ctx,
		managementKubeClient, nil, nil, nil,
		recorder,
		cache,
		func(name string) ([]byte, error) {
			template, err := manifests.ClusterManagerManifestFiles.ReadFile(name)
			if err != nil {
				return nil, err
			}
			objData := assets.MustCreateAssetFromTemplate(name, template, manifestsConfig).Data
			helpers.SetRelatedResourcesStatusesWithObj(relatedResources, objData)
			return objData, nil
		},
		managementResources...,
	)
	for _, result := range resourceResults {
		if result.Error != nil {
			appliedErrs = append(appliedErrs, fmt.Errorf("%q (%T): %v", result.File, result.Type, result.Error))
		}
	}

	clusterManagerName := clusterManager.Name
	clusterManagerMode := clusterManager.Spec.DeployOption.Mode
	clusterManagerNamespace := helpers.ClusterManagerNamespace(clusterManagerName, clusterManagerMode)

	// In the Hosted mode, ensure the rbac kubeconfig secrets is existed for deployments to mount.
	// In this step, we get serviceaccount token from the hub cluster to form a kubeconfig and set it as a secret on the management cluster.
	// Before this step, the serviceaccounts in the hub cluster and the namespace in the management cluster should be applied first.
	if clusterManagerMode == operatorapiv1.InstallModeHosted {
		err = ensureSAKubeconfigs(ctx, clusterManagerName, clusterManagerNamespace,
			hubKubeConfig, hubClient, managementKubeClient, recorder)
		if err != nil {
			return currentGenerations, appliedErrs, err
		}
	}

	for _, file := range deploymentFiles {
		currentGeneration, err := helpers.ApplyDeployment(
			ctx,
			managementKubeClient,
			clusterManager.Status.Generations,
			clusterManager.Spec.NodePlacement,
			func(name string) ([]byte, error) {
				template, err := manifests.ClusterManagerManifestFiles.ReadFile(name)
				if err != nil {
					return nil, err
				}
				objData := assets.MustCreateAssetFromTemplate(name, template, manifestsConfig).Data
				helpers.SetRelatedResourcesStatusesWithObj(relatedResources, objData)
				return objData, nil
			},
			recorder,
			file)
		if err != nil {
			appliedErrs = append(appliedErrs, err)
		}
		currentGenerations = append(currentGenerations, currentGeneration)
	}

	return currentGenerations, appliedErrs, nil
}

func removeClusterManagerFinalizer(ctx context.Context, clusterManagerClient operatorv1client.ClusterManagerInterface, deploy *operatorapiv1.ClusterManager) error {
	copiedFinalizers := []string{}
	for i := range deploy.Finalizers {
		if deploy.Finalizers[i] == clusterManagerFinalizer {
			continue
		}
		copiedFinalizers = append(copiedFinalizers, deploy.Finalizers[i])
	}

	if len(deploy.Finalizers) != len(copiedFinalizers) {
		deploy.Finalizers = copiedFinalizers
		_, err := clusterManagerClient.Update(ctx, deploy, metav1.UpdateOptions{})
		return err
	}

	return nil
}

// removeCRD removes crd, and check if crd resource is removed. Since the related cr is still being deleted,
// it will check the crd existence after deletion, and only return nil when crd is not found.
func removeCRD(ctx context.Context, apiExtensionClient apiextensionsclient.Interface, name string) error {
	err := apiExtensionClient.ApiextensionsV1().CustomResourceDefinitions().Delete(
		ctx, name, metav1.DeleteOptions{})
	switch {
	case errors.IsNotFound(err):
		return nil
	case err != nil:
		return err
	}

	_, err = apiExtensionClient.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, name, metav1.GetOptions{})
	switch {
	case errors.IsNotFound(err):
		return nil
	case err != nil:
		return err
	}

	return fmt.Errorf("CRD %s is still being deleted", name)
}

func cleanUpHub(ctx context.Context, controllerContext factory.SyncContext,
	mode operatorapiv1.InstallMode,
	kubeClient kubernetes.Interface, apiExtensionClient apiextensionsclient.Interface, apiRegistrationClient apiregistrationclient.APIServicesGetter,
	config manifests.HubConfig) error {
	// Remove crd
	for _, name := range crdNames {
		err := removeCRD(ctx, apiExtensionClient, name)
		if err != nil {
			return err
		}
		controllerContext.Recorder().Eventf("CRDDeleted", "crd %s is deleted", name)
	}

	// Remove All Static files
	hubResources := append(getHubResources(mode, config.RegistrationWebhook.IsIPFormat, config.WorkWebhook.IsIPFormat), hubApiserviceFiles...)
	for _, file := range hubResources {
		err := helpers.CleanUpStaticObject(
			ctx,
			kubeClient,
			apiExtensionClient,
			apiRegistrationClient,
			func(name string) ([]byte, error) {
				template, err := manifests.ClusterManagerManifestFiles.ReadFile(name)
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

func cleanUpManagement(ctx context.Context, controllerContext factory.SyncContext,
	kubeClient kubernetes.Interface, config manifests.HubConfig) error {
	// Remove All Static files
	managementResources := []string{namespaceResource} // because namespace is removed, we don't need to remove deployments explicitly
	for _, file := range managementResources {
		err := helpers.CleanUpStaticObject(
			ctx,
			kubeClient, nil, nil,
			func(name string) ([]byte, error) {
				template, err := manifests.ClusterManagerManifestFiles.ReadFile(name)
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

func generateHubClients(hubKubeConfig *rest.Config) (kubernetes.Interface, apiextensionsclient.Interface, apiregistrationclient.APIServicesGetter, error) {
	hubClient, err := kubernetes.NewForConfig(hubKubeConfig)
	if err != nil {
		return nil, nil, nil, err
	}
	hubApiExtensionClient, err := apiextensionsclient.NewForConfig(hubKubeConfig)
	if err != nil {
		return nil, nil, nil, err
	}
	hubApiRegistrationClient, err := apiregistrationclient.NewForConfig(hubKubeConfig)
	if err != nil {
		return nil, nil, nil, err
	}
	return hubClient, hubApiExtensionClient, hubApiRegistrationClient, nil
}

// ensureSAKubeconfigs is used to create a kubeconfig with a token from a ServiceAccount.
// We create a ServiceAccount with a rolebinding on the hub cluster, and then use the token of the ServiceAccount as the user of the kubeconfig.
// Finally, a deployment on the management cluster would use the kubeconfig to access resources on the hub cluster.
func ensureSAKubeconfigs(ctx context.Context, clusterManagerName, clusterManagerNamespace string,
	hubKubeConfig *rest.Config, hubClient, managementClient kubernetes.Interface, recorder events.Recorder) error {
	sas := getSAs(clusterManagerName)
	for _, sa := range sas {
		tokenGetter := helpers.SATokenGetter(ctx, sa, clusterManagerNamespace, hubClient)
		err := helpers.SyncKubeConfigSecret(ctx, sa+"-kubeconfig", clusterManagerNamespace, &rest.Config{
			Host: hubKubeConfig.Host,
			TLSClientConfig: rest.TLSClientConfig{
				CAData: hubKubeConfig.CAData,
			},
		}, managementClient.CoreV1(), tokenGetter, recorder)
		if err != nil {
			return err
		}
	}
	return nil
}

// getSAs return serviceaccount names of all hub components
func getSAs(clusterManagerName string) []string {
	return []string{
		clusterManagerName + "-registration-controller-sa",
		clusterManagerName + "-registration-webhook-sa",
		clusterManagerName + "-work-webhook-sa",
		clusterManagerName + "-placement-controller-sa",
	}
}

func getHubResources(mode operatorapiv1.InstallMode, isRegistrationIPFormat, isWorkIPFormat bool) []string {
	hubResources := []string{namespaceResource}
	hubResources = append(hubResources, hubCRDResourceFiles...)
	hubResources = append(hubResources, hubWebhookResourceFiles...)
	hubResources = append(hubResources, hubRbacResourceFiles...)

	// the hubHostedWebhookServiceFiles are only used in hosted mode
	if mode == operatorapiv1.InstallModeHosted {
		hubResources = append(hubResources, hubHostedWebhookServiceFiles...)
		if isRegistrationIPFormat {
			hubResources = append(hubResources, hubHostedWebhookEndpointRegistration)
		}
		if isWorkIPFormat {
			hubResources = append(hubResources, hubHostedWebhookEndpointWork)
		}
	} else {
		hubResources = append(hubResources, hubDefaultWebhookServiceFiles...)
	}
	return hubResources
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
