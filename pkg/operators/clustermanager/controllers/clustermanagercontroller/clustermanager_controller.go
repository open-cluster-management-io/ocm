package clustermanagercontroller

import (
	"context"
	"encoding/base64"

	utilerrors "k8s.io/apimachinery/pkg/util/errors"

	errorhelpers "errors"
	"time"

	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	"github.com/openshift/library-go/pkg/assets"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
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
	migrationclient "sigs.k8s.io/kube-storage-version-migrator/pkg/clients/clientset/typed/migration/v1alpha1"
)

const (
	clusterManagerFinalizer   = "operator.open-cluster-management.io/cluster-manager-cleanup"
	clusterManagerApplied     = "Applied"
	clusterManagerProgressing = "Progressing"

	caBundleConfigmap = "ca-bundle-configmap"

	defaultWebhookPort       = int32(9443)
	clusterManagerReSyncTime = 5 * time.Second
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
	ensureSAKubeconfigs       func(ctx context.Context, clusterManagerName, clusterManagerNamespace string, hubConfig *rest.Config, hubClient, managementClient kubernetes.Interface, recorder events.Recorder) error
	generateHubClusterClients func(hubConfig *rest.Config) (kubernetes.Interface, apiextensionsclient.Interface, migrationclient.StorageVersionMigrationsGetter, error)
	skipRemoveCRDs            bool
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
		skipRemoveCRDs:            skipRemoveCRDs,
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
		AddOnManagerImage:       clusterManager.Spec.AddOnManagerImagePullSpec,
		Replica:                 helpers.DetermineReplica(ctx, n.operatorKubeClient, clusterManager.Spec.DeployOption.Mode, nil),
		HostedMode:              clusterManager.Spec.DeployOption.Mode == operatorapiv1.InstallModeHosted,
		RegistrationWebhook: manifests.Webhook{
			Port: defaultWebhookPort,
		},
		WorkWebhook: manifests.Webhook{
			Port: defaultWebhookPort,
		},
	}

	if clusterManager.Spec.AddOnManagerConfiguration != nil {
		config.AddOnManagerComponentMode = string(clusterManager.Spec.AddOnManagerConfiguration.Mode)
	}

	var featureGateCondition metav1.Condition
	// If there are some invalid feature gates of registration or work, will output
	// condition `ValidFeatureGates` False in ClusterManager.
	config.RegistrationFeatureGates, config.WorkFeatureGates, featureGateCondition = helpers.CheckFeatureGates(
		helpers.OperatorTypeClusterManager,
		clusterManager.Spec.RegistrationConfiguration,
		clusterManager.Spec.WorkConfiguration)

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
	hubClient, hubApiExtensionClient, hubMigrationClient, err := n.generateHubClusterClients(hubKubeConfig)
	if err != nil {
		return err
	}
	managementClient := n.operatorKubeClient // We assume that operator is always running on the management cluster.

	var errs []error
	reconcilers := []clusterManagerReconcile{
		&crdReconcile{cache: n.cache, recorder: n.recorder, hubAPIExtensionClient: hubApiExtensionClient, hubMigrationClient: hubMigrationClient, skipRemoveCRDs: n.skipRemoveCRDs},
		&hubReoncile{cache: n.cache, recorder: n.recorder, hubKubeClient: hubClient},
		&runtimeReconcile{cache: n.cache, recorder: n.recorder, hubKubeConfig: hubKubeConfig, hubKubeClient: hubClient, kubeClient: managementClient, ensureSAKubeconfigs: n.ensureSAKubeconfigs},
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
		return removeClusterManagerFinalizer(ctx, n.clusterManagerClient, clusterManager)
	}

	//get caBundle
	caBundle := "placeholder"
	configmap, err := n.configMapLister.ConfigMaps(clusterManagerNamespace).Get(caBundleConfigmap)
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

	for _, reconciler := range reconcilers {
		var state reconcileState
		var rqe *helpers.RequeueError
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
	var conds []metav1.Condition = []metav1.Condition{featureGateCondition}

	if cond := meta.FindStatusCondition(clusterManager.Status.Conditions, clusterManagerProgressing); cond != nil {
		conds = append(conds, *cond)
	}

	if len(errs) == 0 {
		conds = append(conds, metav1.Condition{
			Type:    clusterManagerApplied,
			Status:  metav1.ConditionTrue,
			Reason:  "ClusterManagerApplied",
			Message: "Components of cluster manager are applied",
		})
	} else {
		if cond := meta.FindStatusCondition(clusterManager.Status.Conditions, clusterManagerApplied); cond != nil {
			conds = append(conds, *cond)
		}

		// When appliedCondition is false, we should not update related resources and resource generations
		_, updated, updatedErr := helpers.UpdateClusterManagerStatus(
			ctx, n.clusterManagerClient, clusterManager.Name,
			helpers.UpdateClusterManagerConditionFn(conds...),
			func(oldStatus *operatorapiv1.ClusterManagerStatus) error {
				oldStatus.ObservedGeneration = clusterManager.Generation
				return nil
			},
		)

		if updated {
			return updatedErr
		}

		return utilerrors.NewAggregate(errs)
	}

	_, _, updatedErr := helpers.UpdateClusterManagerStatus(
		ctx, n.clusterManagerClient, clusterManager.Name,
		helpers.UpdateClusterManagerConditionFn(conds...),
		helpers.UpdateClusterManagerGenerationsFn(clusterManager.Status.Generations...),
		helpers.UpdateClusterManagerRelatedResourcesFn(clusterManager.Status.RelatedResources...),
		func(oldStatus *operatorapiv1.ClusterManagerStatus) error {
			oldStatus.ObservedGeneration = clusterManager.Generation
			return nil
		},
	)
	if updatedErr != nil {
		errs = append(errs, updatedErr)
	}

	return utilerrors.NewAggregate(errs)
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

func generateHubClients(hubKubeConfig *rest.Config) (kubernetes.Interface, apiextensionsclient.Interface, migrationclient.StorageVersionMigrationsGetter, error) {
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
	hubKubeConfig *rest.Config, hubClient, managementClient kubernetes.Interface, recorder events.Recorder) error {
	sas := getSAs(clusterManagerName)
	for _, sa := range sas {
		tokenGetter := helpers.SATokenGetter(ctx, sa, clusterManagerNamespace, hubClient)
		err := helpers.SyncKubeConfigSecret(ctx, sa+"-kubeconfig", clusterManagerNamespace, "/var/run/secrets/hub/kubeconfig", &rest.Config{
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
func cleanResources(ctx context.Context, kubeClient kubernetes.Interface, cm *operatorapiv1.ClusterManager, config manifests.HubConfig, resources ...string) (*operatorapiv1.ClusterManager, reconcileState, error) {
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
