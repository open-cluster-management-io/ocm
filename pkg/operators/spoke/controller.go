package spoke

import (
	"context"
	"fmt"
	"path/filepath"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"

	"github.com/openshift/api"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"

	"github.com/openshift/library-go/pkg/assets"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	operatorhelpers "github.com/openshift/library-go/pkg/operator/v1helpers"

	nucleusv1client "github.com/open-cluster-management/api/client/nucleus/clientset/versioned/typed/nucleus/v1"
	nucleusinformer "github.com/open-cluster-management/api/client/nucleus/informers/externalversions/nucleus/v1"
	nucleuslister "github.com/open-cluster-management/api/client/nucleus/listers/nucleus/v1"
	nucleusapiv1 "github.com/open-cluster-management/api/nucleus/v1"
	"github.com/open-cluster-management/nucleus/pkg/helpers"
	"github.com/open-cluster-management/nucleus/pkg/operators/spoke/bindata"
)

const (
	nucleusSpokeFinalizer        = "nucleus.open-cluster-management.io/spoke-core-cleanup"
	bootstrapHubKubeConfigSecret = "bootstrap-hub-kubeconfig"
	hubKubeConfigSecret          = "hub-kubeconfig"
	nucluesSpokeCoreNamespace    = "open-cluster-management"
	spokeCoreApplied             = "Applied"
)

var (
	genericScheme = runtime.NewScheme()
	genericCodecs = serializer.NewCodecFactory(genericScheme)
	genericCodec  = genericCodecs.UniversalDeserializer()
)

type nucleusSpokeController struct {
	nucleusClient          nucleusv1client.SpokeCoreInterface
	nucleusLister          nucleuslister.SpokeCoreLister
	kubeClient             kubernetes.Interface
	registrationGeneration int64
	workGeneration         int64
}

func init() {
	utilruntime.Must(api.InstallKube(genericScheme))
}

// NewNucleusSpokeController construct nucleus spoke controller
func NewNucleusSpokeController(
	kubeClient kubernetes.Interface,
	nucleusClient nucleusv1client.SpokeCoreInterface,
	nucleusInformer nucleusinformer.SpokeCoreInformer,
	recorder events.Recorder) factory.Controller {
	controller := &nucleusSpokeController{
		kubeClient:    kubeClient,
		nucleusClient: nucleusClient,
		nucleusLister: nucleusInformer.Lister(),
	}

	return factory.New().WithSync(controller.sync).
		WithInformersQueueKeyFunc(func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			return accessor.GetName()
		}, nucleusInformer.Informer()).
		ToController("NucleusSpokeController", recorder)
}

// spokeConfig is used to render the template of hub manifests
type spokeConfig struct {
	SpokeCoreName             string
	SpokeCoreNamespace        string
	RegistrationImage         string
	WorkImage                 string
	ClusterName               string
	ExternalServerURL         string
	HubKubeConfigSecret       string
	BootStrapKubeConfigSecret string
}

func (n *nucleusSpokeController) sync(ctx context.Context, controllerContext factory.SyncContext) error {
	spokeCoreName := controllerContext.QueueKey()
	klog.V(4).Infof("Reconciling SpokeCore %q", spokeCoreName)
	spokeCore, err := n.nucleusLister.Get(spokeCoreName)
	if errors.IsNotFound(err) {
		// AgentCore not found, could have been deleted, do nothing.
		return nil
	}
	if err != nil {
		return err
	}
	spokeCore = spokeCore.DeepCopy()

	config := spokeConfig{
		SpokeCoreName:             spokeCore.Name,
		SpokeCoreNamespace:        spokeCore.Spec.Namespace,
		RegistrationImage:         spokeCore.Spec.RegistrationImagePullSpec,
		WorkImage:                 spokeCore.Spec.WorkImagePullSpec,
		ClusterName:               spokeCore.Spec.ClusterName,
		BootStrapKubeConfigSecret: bootstrapHubKubeConfigSecret,
		HubKubeConfigSecret:       hubKubeConfigSecret,
	}
	// If namespace is not set, use the default namespace
	if config.SpokeCoreNamespace == "" {
		config.SpokeCoreNamespace = nucluesSpokeCoreNamespace
	}

	// Update finalizer at first
	if spokeCore.DeletionTimestamp.IsZero() {
		hasFinalizer := false
		for i := range spokeCore.Finalizers {
			if spokeCore.Finalizers[i] == nucleusSpokeFinalizer {
				hasFinalizer = true
				break
			}
		}
		if !hasFinalizer {
			spokeCore.Finalizers = append(spokeCore.Finalizers, nucleusSpokeFinalizer)
			_, err := n.nucleusClient.Update(ctx, spokeCore, metav1.UpdateOptions{})
			return err
		}
	}

	// SpokeCore is deleting, we remove its related resources on spoke
	if !spokeCore.DeletionTimestamp.IsZero() {
		if err := n.cleanUp(ctx, controllerContext, config); err != nil {
			return err
		}
		return n.removeWorkFinalizer(ctx, spokeCore)
	}

	// Start deploy spoke core components
	// Check if namespace exists
	_, err = n.kubeClient.CoreV1().Namespaces().Get(ctx, config.SpokeCoreNamespace, metav1.GetOptions{})
	if err != nil {
		helpers.SetNucleusCondition(&spokeCore.Status.Conditions, nucleusapiv1.StatusCondition{
			Type:    spokeCoreApplied,
			Status:  metav1.ConditionFalse,
			Reason:  "SpokeCoreApplyFailed",
			Message: fmt.Sprintf("Failed to get namespace %q", config.SpokeCoreNamespace),
		})
		helpers.UpdateNucleusSpokeStatus(
			ctx, n.nucleusClient, spokeCoreName, helpers.UpdateNucleusSpokeConditionFn(spokeCore.Status.Conditions...))
		return err
	}

	// Check if bootstrap secret exists
	_, err = n.kubeClient.CoreV1().Secrets(config.SpokeCoreNamespace).Get(
		ctx, config.BootStrapKubeConfigSecret, metav1.GetOptions{})
	if err != nil {
		helpers.SetNucleusCondition(&spokeCore.Status.Conditions, nucleusapiv1.StatusCondition{
			Type:    spokeCoreApplied,
			Status:  metav1.ConditionFalse,
			Reason:  "SpokeCoreApplyFailed",
			Message: fmt.Sprintf("Failed to get bootstracp secret %s/%s", config.SpokeCoreNamespace, config.BootStrapKubeConfigSecret),
		})
		helpers.UpdateNucleusSpokeStatus(
			ctx, n.nucleusClient, spokeCoreName, helpers.UpdateNucleusSpokeConditionFn(spokeCore.Status.Conditions...))
		return err
	}

	// Deploy the static resources
	// Apply static files
	resourceResults := resourceapply.ApplyDirectly(
		resourceapply.NewKubeClientHolder(n.kubeClient),
		controllerContext.Recorder(),
		func(name string) ([]byte, error) {
			return assets.MustCreateAssetFromTemplate(name, bindata.MustAsset(filepath.Join("", name)), config).Data, nil
		},
		"manifests/spoke/spoke-clusterrolebinding.yaml",
		"manifests/spoke/spoke-serviceaccount.yaml",
	)
	errs := []error{}
	for _, result := range resourceResults {
		if result.Error != nil {
			errs = append(errs, fmt.Errorf("%q (%T): %v", result.File, result.Type, result.Error))
		}
	}

	if len(errs) > 0 {
		appleErros := operatorhelpers.NewMultiLineAggregate(errs)
		helpers.SetNucleusCondition(&spokeCore.Status.Conditions, nucleusapiv1.StatusCondition{
			Type:    spokeCoreApplied,
			Status:  metav1.ConditionFalse,
			Reason:  "SpokeCoreApplyFailed",
			Message: appleErros.Error(),
		})
		helpers.UpdateNucleusSpokeStatus(
			ctx, n.nucleusClient, spokeCoreName, helpers.UpdateNucleusSpokeConditionFn(spokeCore.Status.Conditions...))
		return appleErros
	}

	// Create hub config secret
	hubSecret, err := n.kubeClient.CoreV1().Secrets(config.SpokeCoreNamespace).Get(
		ctx, hubKubeConfigSecret, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		// Craete an empty secret with placeholder
		hubSecret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      hubKubeConfigSecret,
				Namespace: config.SpokeCoreNamespace,
			},
			Data: map[string][]byte{"placeholder": []byte("placeholder")},
		}
		hubSecret, err = n.kubeClient.CoreV1().Secrets(config.SpokeCoreNamespace).Create(ctx, hubSecret, metav1.CreateOptions{})
		if err != nil {
			return err
		}
	}

	if err != nil {
		helpers.SetNucleusCondition(&spokeCore.Status.Conditions, nucleusapiv1.StatusCondition{
			Type:    spokeCoreApplied,
			Status:  metav1.ConditionFalse,
			Reason:  "SpokeCoreApplyFailed",
			Message: fmt.Sprintf("Failed to get hub kubeconfig secret with error %v", err),
		})
		helpers.UpdateNucleusSpokeStatus(
			ctx, n.nucleusClient, spokeCoreName, helpers.UpdateNucleusSpokeConditionFn(spokeCore.Status.Conditions...))
		return err
	}

	// Deploy registration agent
	generation, err := n.applyDeployment(
		config, "manifests/spoke/spoke-registration-deployment.yaml", n.registrationGeneration, controllerContext)
	if err != nil {
		helpers.SetNucleusCondition(&spokeCore.Status.Conditions, nucleusapiv1.StatusCondition{
			Type:    spokeCoreApplied,
			Status:  metav1.ConditionFalse,
			Reason:  "SpokeCoreApplyFailed",
			Message: fmt.Sprintf("Failed to deploy registration deployment with error %v", err),
		})
		helpers.UpdateNucleusSpokeStatus(
			ctx, n.nucleusClient, spokeCoreName, helpers.UpdateNucleusSpokeConditionFn(spokeCore.Status.Conditions...))
		return err
	}
	n.registrationGeneration = generation

	// If cluster name is empty, read cluster name from hub config secret
	if config.ClusterName == "" {
		clusterName := hubSecret.Data["cluster-name"]
		if clusterName == nil {
			helpers.SetNucleusCondition(&spokeCore.Status.Conditions, nucleusapiv1.StatusCondition{
				Type:    spokeCoreApplied,
				Status:  metav1.ConditionFalse,
				Reason:  "SpokeCoreApplyFailed",
				Message: fmt.Sprintf("Failed to get cluster name"),
			})
			helpers.UpdateNucleusSpokeStatus(
				ctx, n.nucleusClient, spokeCoreName, helpers.UpdateNucleusSpokeConditionFn(spokeCore.Status.Conditions...))
			return fmt.Errorf("Failed to get cluster name")
		}
		config.ClusterName = string(clusterName)
	}

	// If hub kubeconfig does not exist, return err.
	if hubSecret.Data["kubeconfig"] == nil {
		klog.Infof("data is %#v", hubSecret.Data)
		return fmt.Errorf("Failed to get kubeconfig from hub kubeconfig secret")
	}

	// Deploy work agent
	generation, err = n.applyDeployment(
		config, "manifests/spoke/spoke-work-deployment.yaml", n.workGeneration, controllerContext)
	if err != nil {
		helpers.SetNucleusCondition(&spokeCore.Status.Conditions, nucleusapiv1.StatusCondition{
			Type:    spokeCoreApplied,
			Status:  metav1.ConditionFalse,
			Reason:  "SpokeCoreApplyFailed",
			Message: fmt.Sprintf("Failed to deploy work deployment with error %v", err),
		})
		helpers.UpdateNucleusSpokeStatus(
			ctx, n.nucleusClient, spokeCoreName, helpers.UpdateNucleusSpokeConditionFn(spokeCore.Status.Conditions...))
		return err
	}
	n.workGeneration = generation

	// Update status
	helpers.SetNucleusCondition(&spokeCore.Status.Conditions, nucleusapiv1.StatusCondition{
		Type:    spokeCoreApplied,
		Status:  metav1.ConditionTrue,
		Reason:  "SpokeCoreApplied",
		Message: "Spoke Core Component Applied",
	})
	helpers.UpdateNucleusSpokeStatus(
		ctx, n.nucleusClient, spokeCoreName, helpers.UpdateNucleusSpokeConditionFn(spokeCore.Status.Conditions...))
	return err
}

func (n *nucleusSpokeController) applyDeployment(
	config spokeConfig, file string, generation int64, controllerContext factory.SyncContext) (int64, error) {
	deploymentRaw := assets.MustCreateAssetFromTemplate(
		file,
		bindata.MustAsset(filepath.Join("", file)), config).Data
	deployment, _, err := genericCodec.Decode(deploymentRaw, nil, nil)
	if err != nil {
		return 0, fmt.Errorf("%q: %v", file, err)
	}
	updatedDeployment, updated, err := resourceapply.ApplyDeployment(
		n.kubeClient.AppsV1(),
		controllerContext.Recorder(),
		deployment.(*appsv1.Deployment), generation, false)
	if err != nil {
		klog.Errorf("Failed to apply hub deployment manifest: %v", err)
		return 0, fmt.Errorf("%q (%T): %v", file, deployment, err)
	}

	// Record the generation, so the deployment is only updated when generation is changed.
	if updated {
		generation = updatedDeployment.ObjectMeta.Generation
	}

	return generation, nil
}

func (n *nucleusSpokeController) cleanUp(ctx context.Context, controllerContext factory.SyncContext, config spokeConfig) error {
	// Remove deployment
	registrationDeployment := fmt.Sprintf("%s-registration-agent", config.SpokeCoreName)
	err := n.kubeClient.AppsV1().Deployments(config.SpokeCoreNamespace).Delete(ctx, registrationDeployment, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}
	controllerContext.Recorder().Eventf("DeploymentDeleted", "deployment %s is deleted", registrationDeployment)
	workDeployment := fmt.Sprintf("%s-work-agent", config.SpokeCoreName)
	err = n.kubeClient.AppsV1().Deployments(config.SpokeCoreNamespace).Delete(ctx, workDeployment, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}
	// Remove secret
	err = n.kubeClient.CoreV1().Secrets(config.SpokeCoreNamespace).Delete(ctx, config.HubKubeConfigSecret, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}
	controllerContext.Recorder().Eventf("SecretDeleted", "secret %s is deleted", config.HubKubeConfigSecret)
	// Remove service account
	serviceAccountName := fmt.Sprintf("%s-sa", config.SpokeCoreName)
	err = n.kubeClient.CoreV1().ServiceAccounts(config.SpokeCoreNamespace).Delete(ctx, serviceAccountName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}
	controllerContext.Recorder().Eventf("ServiceAccountDeleted", "serviceaccoount %s is deleted", serviceAccountName)
	// Remove clusterrolebinding
	clusterRoleBindingName := fmt.Sprintf("system:open-cluster-management:%s", config.SpokeCoreName)
	err = n.kubeClient.RbacV1().ClusterRoleBindings().Delete(ctx, clusterRoleBindingName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}
	controllerContext.Recorder().Eventf("ClusterRoleBindingDeleted", "clusterrole %s is deleted", clusterRoleBindingName)
	return nil
}

func (n *nucleusSpokeController) removeWorkFinalizer(ctx context.Context, deploy *nucleusapiv1.SpokeCore) error {
	copiedFinalizers := []string{}
	for i := range deploy.Finalizers {
		if deploy.Finalizers[i] == nucleusSpokeFinalizer {
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

func readClusterNameFromSecret(secret *corev1.Secret) (string, error) {
	if secret.Data["cluster-name"] == nil {
		return "", fmt.Errorf("Unable to find cluster name in secret")
	}

	return string(secret.Data["cluster-name"]), nil
}

func readKubuConfigFromSecret(secret *corev1.Secret, config spokeConfig) (string, error) {
	if secret.Data["cluster-name"] == nil {
		return "", fmt.Errorf("Unable to find cluster name in secret")
	}

	return string(secret.Data["cluster-name"]), nil
}
