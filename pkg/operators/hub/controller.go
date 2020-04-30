package hub

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"

	"github.com/openshift/api"
	"github.com/openshift/library-go/pkg/assets"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	operatorhelpers "github.com/openshift/library-go/pkg/operator/v1helpers"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	nucleusv1client "github.com/open-cluster-management/api/client/nucleus/clientset/versioned/typed/nucleus/v1"
	nucleusinformer "github.com/open-cluster-management/api/client/nucleus/informers/externalversions/nucleus/v1"
	nucleuslister "github.com/open-cluster-management/api/client/nucleus/listers/nucleus/v1"
	nucleusapiv1 "github.com/open-cluster-management/api/nucleus/v1"
	"github.com/open-cluster-management/nucleus/pkg/helpers"
	"github.com/open-cluster-management/nucleus/pkg/operators/hub/bindata"
)

var (
	genericScheme = runtime.NewScheme()
	genericCodecs = serializer.NewCodecFactory(genericScheme)
	genericCodec  = genericCodecs.UniversalDeserializer()
	crdNames      = []string{
		"manifestworks.work.open-cluster-management.io",
		"spokeclusters.cluster.open-cluster-management.io",
	}
)

const (
	nucleusHubFinalizer     = "nucleus.open-cluster-management.io/hub-core-cleanup"
	nucluesHubCoreNamespace = "open-cluster-management"
	hubCoreApplied          = "Applied"
	hubCoreAvailable        = "Available"
)

func init() {
	utilruntime.Must(api.InstallKube(genericScheme))
}

type nucleusHubController struct {
	nucleusClient                  nucleusv1client.HubCoreInterface
	nucleusLister                  nucleuslister.HubCoreLister
	kubeClient                     kubernetes.Interface
	apiExtensionClient             apiextensionsclient.Interface
	currentHubDeploymentGeneration int64
}

// NewNucleusHubController construct nucleus hub controller
func NewNucleusHubController(
	kubeClient kubernetes.Interface,
	apiExtensionClient apiextensionsclient.Interface,
	nucleusClient nucleusv1client.HubCoreInterface,
	nucleusInformer nucleusinformer.HubCoreInformer,
	recorder events.Recorder) factory.Controller {
	controller := &nucleusHubController{
		kubeClient:                     kubeClient,
		apiExtensionClient:             apiExtensionClient,
		nucleusClient:                  nucleusClient,
		nucleusLister:                  nucleusInformer.Lister(),
		currentHubDeploymentGeneration: 0,
	}

	return factory.New().WithSync(controller.sync).
		ResyncEvery(3*time.Minute).
		WithInformersQueueKeyFunc(func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			return accessor.GetName()
		}, nucleusInformer.Informer()).
		ToController("NucleusHubController", recorder)
}

// hubConfig is used to render the template of hub manifests
type hubConfig struct {
	HubCoreName       string
	HubCoreNamespace  string
	RegistrationImage string
}

func (n *nucleusHubController) sync(ctx context.Context, controllerContext factory.SyncContext) error {
	hubCoreName := controllerContext.QueueKey()
	klog.V(4).Infof("Reconciling HubCore %q", hubCoreName)

	hubCore, err := n.nucleusLister.Get(hubCoreName)
	if errors.IsNotFound(err) {
		// HubCore not found, could have been deleted, do nothing.
		return nil
	}
	if err != nil {
		return err
	}
	hubCore = hubCore.DeepCopy()

	// Update finalizer at first
	if hubCore.DeletionTimestamp.IsZero() {
		hasFinalizer := false
		for i := range hubCore.Finalizers {
			if hubCore.Finalizers[i] == nucleusHubFinalizer {
				hasFinalizer = true
				break
			}
		}
		if !hasFinalizer {
			hubCore.Finalizers = append(hubCore.Finalizers, nucleusHubFinalizer)
			_, err := n.nucleusClient.Update(ctx, hubCore, metav1.UpdateOptions{})
			return err
		}
	}

	// HubCore is deleting, we remove its related resources on hub
	if !hubCore.DeletionTimestamp.IsZero() {
		if err := n.cleanUp(ctx, controllerContext, hubCore.Name, nucluesHubCoreNamespace); err != nil {
			return err
		}
		return n.removeWorkFinalizer(ctx, hubCore)
	}

	config := hubConfig{
		HubCoreName:       hubCore.Name,
		HubCoreNamespace:  nucluesHubCoreNamespace,
		RegistrationImage: hubCore.Spec.RegistrationImagePullSpec,
	}

	clientHolder := resourceapply.NewKubeClientHolder(n.kubeClient).WithAPIExtensionsClient(n.apiExtensionClient)
	// Apply static files
	resourceResults := resourceapply.ApplyDirectly(
		clientHolder,
		controllerContext.Recorder(),
		func(name string) ([]byte, error) {
			return assets.MustCreateAssetFromTemplate(name, bindata.MustAsset(filepath.Join("", name)), config).Data, nil
		},
		"manifests/hub/0000_00_clusters.open-cluster-management.io_spokeclusters.crd.yaml",
		"manifests/hub/0000_00_work.open-cluster-management.io_manifestworks.crd.yaml",
		"manifests/hub/hub-clusterrole.yaml",
		"manifests/hub/hub-clusterrolebinding.yaml",
		"manifests/hub/hub-namespace.yaml",
		"manifests/hub/hub-serviceaccount.yaml",
	)
	errs := []error{}
	for _, result := range resourceResults {
		if result.Error != nil {
			errs = append(errs, fmt.Errorf("%q (%T): %v", result.File, result.Type, result.Error))
		}
	}

	// Render deployment manifest and apply
	err = n.applyDeployment(config, controllerContext)
	if err != nil {
		errs = append(errs, err)
	}

	conditions := &hubCore.Status.Conditions
	if len(errs) == 0 {
		helpers.SetNucleusCondition(conditions, nucleusapiv1.StatusCondition{
			Type:    hubCoreApplied,
			Status:  metav1.ConditionTrue,
			Reason:  "HubCoreApplied",
			Message: "Components of hub core is applied",
		})
	} else {
		helpers.SetNucleusCondition(conditions, nucleusapiv1.StatusCondition{
			Type:    hubCoreApplied,
			Status:  metav1.ConditionFalse,
			Reason:  "HubCoreApplyFailed",
			Message: "Components of hub core fail to be applied",
		})
	}

	//TODO Check if all the pods are running.
	// Update status
	_, _, updatedErr := helpers.UpdateNucleusHubStatus(
		ctx, n.nucleusClient, hubCore.Name, helpers.UpdateNucleusHubConditionFn(*conditions...))
	if updatedErr != nil {
		errs = append(errs, updatedErr)
	}

	return operatorhelpers.NewMultiLineAggregate(errs)
}

func (n *nucleusHubController) applyDeployment(config hubConfig, controllerContext factory.SyncContext) error {
	deploymentFile := "manifests/hub/hub-deployment.yaml"
	deploymentRaw := assets.MustCreateAssetFromTemplate(
		deploymentFile,
		bindata.MustAsset(filepath.Join("", deploymentFile)), config).Data
	deployment, _, err := genericCodec.Decode(deploymentRaw, nil, nil)
	if err != nil {
		return fmt.Errorf("%q: %v", deploymentFile, err)
	}
	updatedDeployment, updated, err := resourceapply.ApplyDeployment(
		n.kubeClient.AppsV1(),
		controllerContext.Recorder(),
		deployment.(*appsv1.Deployment), n.currentHubDeploymentGeneration, false)
	if err != nil {
		klog.Errorf("Failed to apply hub deployment manifest: %v", err)
		return fmt.Errorf("%q (%T): %v", deploymentFile, deployment, err)
	}

	// Record the generation, so the deployment is only updated when generation is changed.
	if updated {
		n.currentHubDeploymentGeneration = updatedDeployment.ObjectMeta.Generation
	}

	return nil
}

func (n *nucleusHubController) removeWorkFinalizer(ctx context.Context, deploy *nucleusapiv1.HubCore) error {
	copiedFinalizers := []string{}
	for i := range deploy.Finalizers {
		if deploy.Finalizers[i] == nucleusHubFinalizer {
			continue
		}
		copiedFinalizers = append(copiedFinalizers, deploy.Finalizers[i])
	}

	if len(deploy.Finalizers) != len(copiedFinalizers) {
		deploy.Finalizers = copiedFinalizers
		_, err := n.nucleusClient.Update(ctx, deploy, metav1.UpdateOptions{})
		return err
	}

	return nil
}

// removeCRD removes crd, and check if crd resource is removed. Since the related cr is still being deleted,
// it will check the crd existence after deletion, and only return nil when crd is not found.
func (n *nucleusHubController) removeCRD(ctx context.Context, name string) error {
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

func (n *nucleusHubController) cleanUp(
	ctx context.Context, controllerContext factory.SyncContext, name, namespace string) error {
	// Remove crd
	for _, name := range crdNames {
		err := n.removeCRD(ctx, name)
		if err != nil {
			return err
		}
		controllerContext.Recorder().Eventf("CRDDeleted", "crd %s is deleted", name)
	}

	// Remove deployment
	deploymentName := fmt.Sprintf("%s-controller", name)
	err := n.kubeClient.AppsV1().Deployments(namespace).Delete(ctx, deploymentName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}
	controllerContext.Recorder().Eventf("DeploymentDeleted", "deployment %s is deleted", deploymentName)

	// Remove serviceaccount
	serviceAccountName := fmt.Sprintf("%s-sa", name)
	err = n.kubeClient.CoreV1().ServiceAccounts(namespace).Delete(ctx, serviceAccountName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}
	controllerContext.Recorder().Eventf("ServiceAccountDeleted", "serviceaccoount %s is deleted", serviceAccountName)

	// Remove clusterrole
	clusterRoleName := fmt.Sprintf("system:open-cluster-management:%s", name)
	err = n.kubeClient.RbacV1().ClusterRoles().Delete(ctx, clusterRoleName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}
	controllerContext.Recorder().Eventf("ClusterRoleDeleted", "clusterrole %s is deleted", clusterRoleName)

	// Remove clusterrolebinding
	err = n.kubeClient.RbacV1().ClusterRoleBindings().Delete(ctx, clusterRoleName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}
	controllerContext.Recorder().Eventf("ClusterRoleDeleted", "clusterrole %s is deleted", clusterRoleName)
	return nil
}
