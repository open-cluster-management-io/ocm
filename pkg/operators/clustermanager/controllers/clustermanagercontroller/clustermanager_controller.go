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
	"k8s.io/klog/v2"
	apiregistrationclient "k8s.io/kube-aggregator/pkg/client/clientset_generated/clientset/typed/apiregistration/v1"

	"github.com/openshift/library-go/pkg/assets"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
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

	staticResourceFiles = []string{
		"cluster-manager/0000_00_addon.open-cluster-management.io_clustermanagementaddons.crd.yaml",
		"cluster-manager/0000_00_clusters.open-cluster-management.io_managedclusters.crd.yaml",
		"cluster-manager/0000_00_clusters.open-cluster-management.io_managedclustersets.crd.yaml",
		"cluster-manager/0000_00_work.open-cluster-management.io_manifestworks.crd.yaml",
		"cluster-manager/0000_01_addon.open-cluster-management.io_managedclusteraddons.crd.yaml",
		"cluster-manager/0000_01_clusters.open-cluster-management.io_managedclustersetbindings.crd.yaml",
		"cluster-manager/0000_02_clusters.open-cluster-management.io_placements.crd.yaml",
		"cluster-manager/0000_03_clusters.open-cluster-management.io_placementdecisions.crd.yaml",
		"cluster-manager/0000_05_clusters.open-cluster-management.io_addonplacementscores.crd.yaml",
		"cluster-manager/cluster-manager-registration-clusterrole.yaml",
		"cluster-manager/cluster-manager-registration-clusterrolebinding.yaml",
		"cluster-manager/cluster-manager-namespace.yaml",
		"cluster-manager/cluster-manager-registration-serviceaccount.yaml",
		"cluster-manager/cluster-manager-registration-webhook-clusterrole.yaml",
		"cluster-manager/cluster-manager-registration-webhook-clusterrolebinding.yaml",
		"cluster-manager/cluster-manager-registration-webhook-service.yaml",
		"cluster-manager/cluster-manager-registration-webhook-serviceaccount.yaml",
		"cluster-manager/cluster-manager-registration-webhook-clustersetbinding-validatingconfiguration.yaml",
		"cluster-manager/cluster-manager-registration-webhook-validatingconfiguration.yaml",
		"cluster-manager/cluster-manager-registration-webhook-mutatingconfiguration.yaml",
		"cluster-manager/cluster-manager-work-webhook-clusterrole.yaml",
		"cluster-manager/cluster-manager-work-webhook-clusterrolebinding.yaml",
		"cluster-manager/cluster-manager-work-webhook-service.yaml",
		"cluster-manager/cluster-manager-work-webhook-serviceaccount.yaml",
		"cluster-manager/cluster-manager-work-webhook-validatingconfiguration.yaml",
		"cluster-manager/cluster-manager-placement-clusterrole.yaml",
		"cluster-manager/cluster-manager-placement-clusterrolebinding.yaml",
		"cluster-manager/cluster-manager-placement-serviceaccount.yaml",
	}

	// apiserviceResoruceFiles requires CABundle in HubConfig
	apiserviceResoruceFiles = []string{
		"cluster-manager/cluster-manager-work-webhook-apiservice.yaml",
		"cluster-manager/cluster-manager-registration-webhook-apiservice.yaml",
	}

	deploymentFiles = []string{
		"cluster-manager/cluster-manager-registration-deployment.yaml",
		"cluster-manager/cluster-manager-registration-webhook-deployment.yaml",
		"cluster-manager/cluster-manager-work-webhook-deployment.yaml",
		"cluster-manager/cluster-manager-placement-deployment.yaml",
	}
)

const (
	clusterManagerFinalizer = "operator.open-cluster-management.io/cluster-manager-cleanup"
	clusterManagerApplied   = "Applied"
	clusterManagerAvailable = "Available"
	caBundleConfigmap       = "ca-bundle-configmap"
)

type clusterManagerController struct {
	clusterManagerClient  operatorv1client.ClusterManagerInterface
	clusterManagerLister  operatorlister.ClusterManagerLister
	kubeClient            kubernetes.Interface
	apiExtensionClient    apiextensionsclient.Interface
	apiRegistrationClient apiregistrationclient.APIServicesGetter
	configMapLister       corev1listers.ConfigMapLister
}

// NewClusterManagerController construct cluster manager hub controller
func NewClusterManagerController(
	kubeClient kubernetes.Interface,
	apiExtensionClient apiextensionsclient.Interface,
	apiRegistrationClient apiregistrationclient.APIServicesGetter,
	clusterManagerClient operatorv1client.ClusterManagerInterface,
	clusterManagerInformer operatorinformer.ClusterManagerInformer,
	deploymentInformer appsinformer.DeploymentInformer,
	configMapInformer corev1informers.ConfigMapInformer,
	recorder events.Recorder) factory.Controller {
	controller := &clusterManagerController{
		kubeClient:            kubeClient,
		apiExtensionClient:    apiExtensionClient,
		apiRegistrationClient: apiRegistrationClient,
		clusterManagerClient:  clusterManagerClient,
		clusterManagerLister:  clusterManagerInformer.Lister(),
		configMapLister:       configMapInformer.Lister(),
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
	clusterManagerNamespace := helpers.ClusterManagerNamespace(clusterManagerName, clusterManager.Spec.DeployOption.Mode)

	config := manifests.HubConfig{
		ClusterManagerName:      clusterManager.Name,
		ClusterManagerNamespace: clusterManagerNamespace,
		RegistrationImage:       clusterManager.Spec.RegistrationImagePullSpec,
		WorkImage:               clusterManager.Spec.WorkImagePullSpec,
		PlacementImage:          clusterManager.Spec.PlacementImagePullSpec,
		Replica:                 helpers.DetermineReplicaByNodes(ctx, n.kubeClient),
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

	// ClusterManager is deleting, we remove its related resources on hub
	if !clusterManager.DeletionTimestamp.IsZero() {
		if err := n.cleanUp(ctx, controllerContext, config); err != nil {
			return err
		}
		return n.removeClusterManagerFinalizer(ctx, clusterManager)
	}

	// Apply static files(Which don't require CABundle)
	var relatedResources []operatorapiv1.RelatedResourceMeta
	resourceResults := helpers.ApplyDirectly(
		n.kubeClient,
		n.apiExtensionClient,
		n.apiRegistrationClient,
		controllerContext.Recorder(),
		func(name string) ([]byte, error) {
			template, err := manifests.ClusterManagerManifestFiles.ReadFile(name)
			if err != nil {
				return nil, err
			}
			objData := assets.MustCreateAssetFromTemplate(name, template, config).Data
			helpers.SetRelatedResourcesStatusesWithObj(&relatedResources, objData)
			return objData, nil
		},
		staticResourceFiles...,
	)
	errs := []error{}
	for _, result := range resourceResults {
		if result.Error != nil {
			errs = append(errs, fmt.Errorf("%q (%T): %v", result.File, result.Type, result.Error))
		}
	}

	// try to load ca bundle from configmap
	// if the namespace not found yet, skip this and apply static resources first
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

	// Apply apiservice files
	apiserviceResults := helpers.ApplyDirectly(
		n.kubeClient,
		n.apiExtensionClient,
		n.apiRegistrationClient,
		controllerContext.Recorder(),
		func(name string) ([]byte, error) {
			template, err := manifests.ClusterManagerManifestFiles.ReadFile(name)
			if err != nil {
				return nil, err
			}
			objData := assets.MustCreateAssetFromTemplate(name, template, config).Data
			helpers.SetRelatedResourcesStatusesWithObj(&relatedResources, objData)
			return objData, nil
		},
		apiserviceResoruceFiles...,
	)
	for _, result := range apiserviceResults {
		if result.Error != nil {
			errs = append(errs, fmt.Errorf("%q (%T): %v", result.File, result.Type, result.Error))
		}
	}

	currentGenerations := []operatorapiv1.GenerationStatus{}
	// Render deployment manifest and apply
	for _, file := range deploymentFiles {
		currentGeneration, err := helpers.ApplyDeployment(
			n.kubeClient,
			clusterManager.Status.Generations,
			clusterManager.Spec.NodePlacement,
			func(name string) ([]byte, error) {
				template, err := manifests.ClusterManagerManifestFiles.ReadFile(name)
				if err != nil {
					return nil, err
				}
				objData := assets.MustCreateAssetFromTemplate(name, template, config).Data
				helpers.SetRelatedResourcesStatusesWithObj(&relatedResources, objData)
				return objData, nil
			},
			controllerContext.Recorder(),
			file)
		if err != nil {
			errs = append(errs, err)
		}
		currentGenerations = append(currentGenerations, currentGeneration)
	}

	conditions := &clusterManager.Status.Conditions
	observedKlusterletGeneration := clusterManager.Status.ObservedGeneration
	if len(errs) == 0 {
		meta.SetStatusCondition(conditions, metav1.Condition{
			Type:    clusterManagerApplied,
			Status:  metav1.ConditionTrue,
			Reason:  "ClusterManagerApplied",
			Message: "Components of cluster manager is applied",
		})
		observedKlusterletGeneration = clusterManager.Generation
	} else {
		meta.SetStatusCondition(conditions, metav1.Condition{
			Type:    clusterManagerApplied,
			Status:  metav1.ConditionFalse,
			Reason:  "ClusterManagerApplyFailed",
			Message: "Components of cluster manager fail to be applied",
		})
	}

	// Update status
	_, _, updatedErr := helpers.UpdateClusterManagerStatus(
		ctx, n.clusterManagerClient, clusterManager.Name,
		helpers.UpdateClusterManagerConditionFn(*conditions...),
		helpers.UpdateClusterManagerGenerationsFn(currentGenerations...),
		helpers.UpdateClusterManagerRelatedResourcesFn(relatedResources...),
		func(oldStatus *operatorapiv1.ClusterManagerStatus) error {
			oldStatus.ObservedGeneration = observedKlusterletGeneration
			return nil
		},
	)
	if updatedErr != nil {
		errs = append(errs, updatedErr)
	}

	return operatorhelpers.NewMultiLineAggregate(errs)
}

func (n *clusterManagerController) removeClusterManagerFinalizer(ctx context.Context, deploy *operatorapiv1.ClusterManager) error {
	copiedFinalizers := []string{}
	for i := range deploy.Finalizers {
		if deploy.Finalizers[i] == clusterManagerFinalizer {
			continue
		}
		copiedFinalizers = append(copiedFinalizers, deploy.Finalizers[i])
	}

	if len(deploy.Finalizers) != len(copiedFinalizers) {
		deploy.Finalizers = copiedFinalizers
		_, err := n.clusterManagerClient.Update(ctx, deploy, metav1.UpdateOptions{})
		return err
	}

	return nil
}

// removeCRD removes crd, and check if crd resource is removed. Since the related cr is still being deleted,
// it will check the crd existence after deletion, and only return nil when crd is not found.
func (n *clusterManagerController) removeCRD(ctx context.Context, name string) error {
	err := n.apiExtensionClient.ApiextensionsV1().CustomResourceDefinitions().Delete(
		ctx, name, metav1.DeleteOptions{})
	switch {
	case errors.IsNotFound(err):
		return nil
	case err != nil:
		return err
	}

	_, err = n.apiExtensionClient.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, name, metav1.GetOptions{})
	switch {
	case errors.IsNotFound(err):
		return nil
	case err != nil:
		return err
	}

	return fmt.Errorf("CRD %s is still being deleted", name)
}

func (n *clusterManagerController) cleanUp(
	ctx context.Context, controllerContext factory.SyncContext, config manifests.HubConfig) error {
	// Remove crd
	for _, name := range crdNames {
		err := n.removeCRD(ctx, name)
		if err != nil {
			return err
		}
		controllerContext.Recorder().Eventf("CRDDeleted", "crd %s is deleted", name)
	}

	// Remove All Static files
	allResourceFiles := append(staticResourceFiles, apiserviceResoruceFiles...)
	for _, file := range allResourceFiles {
		err := helpers.CleanUpStaticObject(
			ctx,
			n.kubeClient,
			n.apiExtensionClient,
			n.apiRegistrationClient,
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
