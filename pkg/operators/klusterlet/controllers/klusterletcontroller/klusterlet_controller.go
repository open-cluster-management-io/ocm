package klusterletcontroller

import (
	"context"
	"crypto/sha256"
	"fmt"
	"reflect"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/version"
	appsinformer "k8s.io/client-go/informers/apps/v1"
	coreinformer "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/openshift/library-go/pkg/assets"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	operatorhelpers "github.com/openshift/library-go/pkg/operator/v1helpers"

	operatorv1client "open-cluster-management.io/api/client/operator/clientset/versioned/typed/operator/v1"
	operatorinformer "open-cluster-management.io/api/client/operator/informers/externalversions/operator/v1"
	operatorlister "open-cluster-management.io/api/client/operator/listers/operator/v1"
	workv1client "open-cluster-management.io/api/client/work/clientset/versioned/typed/work/v1"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"
	"open-cluster-management.io/registration-operator/manifests"
	"open-cluster-management.io/registration-operator/pkg/helpers"
)

const (
	klusterletFinalizer          = "operator.open-cluster-management.io/klusterlet-cleanup"
	imagePullSecret              = "open-cluster-management-image-pull-credentials"
	klusterletApplied            = "Applied"
	appliedManifestWorkFinalizer = "cluster.open-cluster-management.io/applied-manifest-work-cleanup"
	defaultReplica               = 3
	singleReplica                = 1
)

var (
	crdV1StaticFiles = []string{
		"klusterlet/0000_01_work.open-cluster-management.io_appliedmanifestworks.crd.yaml",
		"klusterlet/0000_02_clusters.open-cluster-management.io_clusterclaims.crd.yaml",
	}

	crdV1beta1StaticFiles = []string{
		"klusterlet/0001_01_work.open-cluster-management.io_appliedmanifestworks.crd.yaml",
		"klusterlet/0001_02_clusters.open-cluster-management.io_clusterclaims.crd.yaml",
	}

	staticResourceFiles = []string{
		"klusterlet/klusterlet-registration-serviceaccount.yaml",
		"klusterlet/klusterlet-registration-clusterrole.yaml",
		"klusterlet/klusterlet-registration-clusterrolebinding.yaml",
		"klusterlet/klusterlet-registration-role.yaml",
		"klusterlet/klusterlet-registration-rolebinding.yaml",
		"klusterlet/klusterlet-work-serviceaccount.yaml",
		"klusterlet/klusterlet-work-clusterrole.yaml",
		"klusterlet/klusterlet-work-clusterrolebinding.yaml",
		"klusterlet/klusterlet-work-clusterrolebinding-addition.yaml",
	}

	kube111StaticResourceFiles = []string{
		"klusterletkube111/klusterlet-registration-operator-clusterrolebinding.yaml",
		"klusterletkube111/klusterlet-work-clusterrolebinding.yaml",
	}
)

type klusterletController struct {
	klusterletClient          operatorv1client.KlusterletInterface
	klusterletLister          operatorlister.KlusterletLister
	kubeClient                kubernetes.Interface
	apiExtensionClient        apiextensionsclient.Interface
	appliedManifestWorkClient workv1client.AppliedManifestWorkInterface
	kubeVersion               *version.Version
	operatorNamespace         string
}

// NewKlusterletController construct klusterlet controller
func NewKlusterletController(
	kubeClient kubernetes.Interface,
	apiExtensionClient apiextensionsclient.Interface,
	klusterletClient operatorv1client.KlusterletInterface,
	klusterletInformer operatorinformer.KlusterletInformer,
	secretInformer coreinformer.SecretInformer,
	deploymentInformer appsinformer.DeploymentInformer,
	appliedManifestWorkClient workv1client.AppliedManifestWorkInterface,
	kubeVersion *version.Version,
	operatorNamespace string,
	recorder events.Recorder) factory.Controller {
	controller := &klusterletController{
		kubeClient:                kubeClient,
		apiExtensionClient:        apiExtensionClient,
		klusterletClient:          klusterletClient,
		klusterletLister:          klusterletInformer.Lister(),
		appliedManifestWorkClient: appliedManifestWorkClient,
		kubeVersion:               kubeVersion,
		operatorNamespace:         operatorNamespace,
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
	KlusterletName            string
	KlusterletNamespace       string
	RegistrationImage         string
	WorkImage                 string
	ClusterName               string
	ExternalServerURL         string
	HubKubeConfigSecret       string
	BootStrapKubeConfigSecret string
	OperatorNamespace         string
	Replica                   int32
}

func (n *klusterletController) sync(ctx context.Context, controllerContext factory.SyncContext) error {
	klusterletName := controllerContext.QueueKey()
	klog.V(4).Infof("Reconciling Klusterlet %q", klusterletName)
	klusterlet, err := n.klusterletLister.Get(klusterletName)
	if errors.IsNotFound(err) {
		// AgentCore not found, could have been deleted, do nothing.
		return nil
	}
	if err != nil {
		return err
	}
	klusterlet = klusterlet.DeepCopy()
	config := klusterletConfig{
		KlusterletName:            klusterlet.Name,
		KlusterletNamespace:       klusterlet.Spec.Namespace,
		RegistrationImage:         klusterlet.Spec.RegistrationImagePullSpec,
		WorkImage:                 klusterlet.Spec.WorkImagePullSpec,
		ClusterName:               klusterlet.Spec.ClusterName,
		BootStrapKubeConfigSecret: helpers.BootstrapHubKubeConfig,
		HubKubeConfigSecret:       helpers.HubKubeConfig,
		ExternalServerURL:         getServersFromKlusterlet(klusterlet),
		OperatorNamespace:         n.operatorNamespace,
		Replica:                   helpers.DetermineReplicaByNodes(ctx, n.kubeClient),
	}
	// If namespace is not set, use the default namespace
	if config.KlusterletNamespace == "" {
		config.KlusterletNamespace = helpers.KlusterletDefaultNamespace
	}

	// Update finalizer at first
	if klusterlet.DeletionTimestamp.IsZero() {
		hasFinalizer := false
		for i := range klusterlet.Finalizers {
			if klusterlet.Finalizers[i] == klusterletFinalizer {
				hasFinalizer = true
				break
			}
		}
		if !hasFinalizer {
			klusterlet.Finalizers = append(klusterlet.Finalizers, klusterletFinalizer)
			_, err := n.klusterletClient.Update(ctx, klusterlet, metav1.UpdateOptions{})
			return err
		}
	}

	// Klusterlet is deleting, we remove its related resources on managed cluster
	if !klusterlet.DeletionTimestamp.IsZero() {
		if err := n.cleanUp(ctx, controllerContext, config); err != nil {
			return err
		}
		return n.removeKlusterletFinalizer(ctx, klusterlet)
	}

	// Start deploy klusterlet components
	// Ensure the existence namespaces for klusterlet and klusterlet addon
	// Sync pull secret to each namespace
	namespaces := []string{config.KlusterletNamespace, fmt.Sprintf("%s-addon", config.KlusterletNamespace)}
	for _, namespace := range namespaces {
		err := n.ensureNamespace(ctx, klusterlet.Name, namespace)
		if err != nil {
			return err
		}

		// Sync pull secret
		_, _, err = resourceapply.SyncSecret(
			n.kubeClient.CoreV1(),
			controllerContext.Recorder(),
			n.operatorNamespace,
			imagePullSecret,
			namespace,
			imagePullSecret,
			[]metav1.OwnerReference{},
		)

		if err != nil {
			return err
		}
	}

	if err != nil {
		_, _, _ = helpers.UpdateKlusterletStatus(ctx, n.klusterletClient, klusterletName, helpers.UpdateKlusterletConditionFn(metav1.Condition{
			Type: klusterletApplied, Status: metav1.ConditionFalse, Reason: "KlusterletApplyFailed",
			Message: fmt.Sprintf("Failed to sync image pull secret to namespace %q: %v", config.KlusterletNamespace, err),
		}))

		return err
	}

	var relatedResources []operatorapiv1.RelatedResourceMeta
	errs := []error{}
	// If kube version is less than 1.12, deploy static resource for kube 1.11 at first
	// TODO remove this when we do not support kube 1.11 any longer
	if cnt, err := n.kubeVersion.Compare("v1.12.0"); err == nil && cnt < 0 {
		resourceResult := resourceapply.ApplyDirectly(
			resourceapply.NewKubeClientHolder(n.kubeClient),
			controllerContext.Recorder(),
			func(name string) ([]byte, error) {
				template, err := manifests.Klusterlet111ManifestFiles.ReadFile(name)
				if err != nil {
					return nil, err
				}
				objData := assets.MustCreateAssetFromTemplate(name, template, config).Data
				helpers.SetRelatedResourcesStatusesWithObj(&relatedResources, objData)
				return objData, nil
			},
			kube111StaticResourceFiles...,
		)
		for _, result := range resourceResult {
			if result.Error != nil {
				errs = append(errs, fmt.Errorf("%q (%T): %v", result.File, result.Type, result.Error))
			}
		}
	}

	// Apply static files
	var appliedStaticFiles []string
	// CRD v1beta1 was deprecated from k8s 1.16.0 and will be removed in k8s 1.22
	if cnt, err := n.kubeVersion.Compare("v1.16.0"); err == nil && cnt < 0 {
		appliedStaticFiles = append(crdV1beta1StaticFiles, staticResourceFiles...)
	} else {
		appliedStaticFiles = append(crdV1StaticFiles, staticResourceFiles...)
	}

	resourceResults := resourceapply.ApplyDirectly(
		resourceapply.NewKubeClientHolder(n.kubeClient).WithAPIExtensionsClient(n.apiExtensionClient),
		controllerContext.Recorder(),
		func(name string) ([]byte, error) {
			template, err := manifests.KlusterletManifestFiles.ReadFile(name)
			if err != nil {
				return nil, err
			}
			objData := assets.MustCreateAssetFromTemplate(name, template, config).Data
			helpers.SetRelatedResourcesStatusesWithObj(&relatedResources, objData)
			return objData, nil
		},
		appliedStaticFiles...,
	)

	for _, result := range resourceResults {
		if result.Error != nil {
			errs = append(errs, fmt.Errorf("%q (%T): %v", result.File, result.Type, result.Error))
		}
	}

	if len(errs) > 0 {
		applyErrors := operatorhelpers.NewMultiLineAggregate(errs)
		_, _, _ = helpers.UpdateKlusterletStatus(ctx, n.klusterletClient, klusterletName, helpers.UpdateKlusterletConditionFn(metav1.Condition{
			Type: klusterletApplied, Status: metav1.ConditionFalse, Reason: "KlusterletApplyFailed",
			Message: applyErrors.Error(),
		}))
		return applyErrors
	}

	// Create hub config secret
	hubSecret, err := n.kubeClient.CoreV1().Secrets(config.KlusterletNamespace).Get(ctx, helpers.HubKubeConfig, metav1.GetOptions{})
	switch {
	case errors.IsNotFound(err):
		// Create an empty secret with placeholder
		hubSecret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      helpers.HubKubeConfig,
				Namespace: config.KlusterletNamespace,
			},
			Data: map[string][]byte{"placeholder": []byte("placeholder")},
		}
		hubSecret, err = n.kubeClient.CoreV1().Secrets(config.KlusterletNamespace).Create(ctx, hubSecret, metav1.CreateOptions{})
		if err != nil {
			_, _, _ = helpers.UpdateKlusterletStatus(ctx, n.klusterletClient, klusterletName, helpers.UpdateKlusterletConditionFn(metav1.Condition{
				Type: klusterletApplied, Status: metav1.ConditionFalse, Reason: "KlusterletApplyFailed",
				Message: fmt.Sprintf("Failed to create hub kubeconfig secret -n %q %q: %v", hubSecret.Namespace, hubSecret.Name, err),
			}))
			return err
		}
	case err != nil:
		_, _, _ = helpers.UpdateKlusterletStatus(ctx, n.klusterletClient, klusterletName, helpers.UpdateKlusterletConditionFn(metav1.Condition{
			Type: klusterletApplied, Status: metav1.ConditionFalse, Reason: "KlusterletApplyFailed",
			Message: fmt.Sprintf("Failed to get hub kubeconfig secret with error %v", err),
		}))
		return err
	}

	// Deploy registration agent
	registrationGeneration, err := helpers.ApplyDeployment(
		n.kubeClient,
		klusterlet.Status.Generations,
		klusterlet.Spec.NodePlacement,
		func(name string) ([]byte, error) {
			template, err := manifests.KlusterletManifestFiles.ReadFile(name)
			if err != nil {
				return nil, err
			}
			objData := assets.MustCreateAssetFromTemplate(name, template, config).Data
			helpers.SetRelatedResourcesStatusesWithObj(&relatedResources, objData)
			return objData, nil
		},
		controllerContext.Recorder(),
		"klusterlet/klusterlet-registration-deployment.yaml")
	if err != nil {
		_, _, _ = helpers.UpdateKlusterletStatus(ctx, n.klusterletClient, klusterletName, helpers.UpdateKlusterletConditionFn(metav1.Condition{
			Type: klusterletApplied, Status: metav1.ConditionFalse, Reason: "KlusterletApplyFailed",
			Message: fmt.Sprintf("Failed to deploy registration deployment with error %v", err),
		}))
		return err
	}

	// If cluster name is empty, read cluster name from hub config secret
	if config.ClusterName == "" {
		clusterName := hubSecret.Data["cluster-name"]
		if clusterName != nil {
			config.ClusterName = string(clusterName)
		}
	}

	// Deploy work agent
	workGeneration, err := helpers.ApplyDeployment(
		n.kubeClient,
		klusterlet.Status.Generations,
		klusterlet.Spec.NodePlacement,
		func(name string) ([]byte, error) {
			template, err := manifests.KlusterletManifestFiles.ReadFile(name)
			if err != nil {
				return nil, err
			}
			objData := assets.MustCreateAssetFromTemplate(name, template, config).Data
			helpers.SetRelatedResourcesStatusesWithObj(&relatedResources, objData)
			return objData, nil
		},
		controllerContext.Recorder(),
		"klusterlet/klusterlet-work-deployment.yaml")
	if err != nil {
		_, _, _ = helpers.UpdateKlusterletStatus(ctx, n.klusterletClient, klusterletName, helpers.UpdateKlusterletConditionFn(metav1.Condition{
			Type: klusterletApplied, Status: metav1.ConditionFalse, Reason: "KlusterletApplyFailed",
			Message: fmt.Sprintf("Failed to deploy work deployment with error %v", err),
		}))
		return err
	}
	observedKlusterletGeneration := klusterlet.Generation

	// if we get here, we have successfully applied everything and should indicate that
	_, _, _ = helpers.UpdateKlusterletStatus(ctx, n.klusterletClient, klusterletName,
		helpers.UpdateKlusterletConditionFn(metav1.Condition{
			Type: klusterletApplied, Status: metav1.ConditionTrue, Reason: "KlusterletApplied",
			Message: "Klusterlet Component Applied"}),
		helpers.UpdateKlusterletGenerationsFn(registrationGeneration, workGeneration),
		helpers.UpdateKlusterletRelatedResourcesFn(relatedResources...),
		func(oldStatus *operatorapiv1.KlusterletStatus) error {
			oldStatus.ObservedGeneration = observedKlusterletGeneration
			return nil
		},
	)
	return nil
}

func (n *klusterletController) ensureNamespace(ctx context.Context, klusterletName, namespace string) error {
	_, err := n.kubeClient.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	switch {
	case errors.IsNotFound(err):
		_, createErr := n.kubeClient.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
				Annotations: map[string]string{
					"workload.openshift.io/allowed": "management",
				},
			},
		}, metav1.CreateOptions{})
		if createErr != nil {
			_, _, _ = helpers.UpdateKlusterletStatus(ctx, n.klusterletClient, klusterletName, helpers.UpdateKlusterletConditionFn(metav1.Condition{
				Type: klusterletApplied, Status: metav1.ConditionFalse, Reason: "KlusterletApplyFailed",
				Message: fmt.Sprintf("Failed to create namespace %q: %v", namespace, createErr),
			}))
			return createErr
		}
	case err != nil:
		_, _, _ = helpers.UpdateKlusterletStatus(ctx, n.klusterletClient, klusterletName, helpers.UpdateKlusterletConditionFn(metav1.Condition{
			Type: klusterletApplied, Status: metav1.ConditionFalse, Reason: "KlusterletApplyFailed",
			Message: fmt.Sprintf("Failed to get namespace %q: %v", namespace, err),
		}))
		return err
	}

	return nil
}

func (n *klusterletController) cleanUp(ctx context.Context, controllerContext factory.SyncContext, config klusterletConfig) error {
	// Remove deployment
	registrationDeployment := fmt.Sprintf("%s-registration-agent", config.KlusterletName)
	err := n.kubeClient.AppsV1().Deployments(config.KlusterletNamespace).Delete(ctx, registrationDeployment, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}
	controllerContext.Recorder().Eventf("DeploymentDeleted", "deployment %s is deleted", registrationDeployment)
	workDeployment := fmt.Sprintf("%s-work-agent", config.KlusterletName)
	err = n.kubeClient.AppsV1().Deployments(config.KlusterletNamespace).Delete(ctx, workDeployment, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	// get hub host from bootstrap kubeconfig
	var hubHost string
	bootstrapKubeConfigSecret, err := n.kubeClient.CoreV1().Secrets(config.KlusterletNamespace).Get(ctx, config.BootStrapKubeConfigSecret, metav1.GetOptions{})
	switch {
	case err == nil:
		restConfig, err := helpers.LoadClientConfigFromSecret(bootstrapKubeConfigSecret)
		if err != nil {
			return fmt.Errorf("unable to load kubeconfig from secret %q %q: %w", config.KlusterletNamespace, config.BootStrapKubeConfigSecret, err)
		}
		hubHost = restConfig.Host
	case !errors.IsNotFound(err):
		return err
	}

	// Remove secret
	err = n.kubeClient.CoreV1().Secrets(config.KlusterletNamespace).Delete(ctx, config.HubKubeConfigSecret, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}
	controllerContext.Recorder().Eventf("SecretDeleted", "secret %s is deleted", config.HubKubeConfigSecret)

	// Remove Static files
	for _, file := range staticResourceFiles {
		err := helpers.CleanUpStaticObject(
			ctx,
			n.kubeClient,
			n.apiExtensionClient,
			nil,
			func(name string) ([]byte, error) {
				template, err := manifests.KlusterletManifestFiles.ReadFile(name)
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

	// TODO remove this when we do not support kube 1.11 any longer
	cnt, err := n.kubeVersion.Compare("v1.12.0")
	klog.Errorf("comapare version %d, %v", cnt, err)
	if cnt, err := n.kubeVersion.Compare("v1.12.0"); err == nil && cnt < 0 {
		for _, file := range kube111StaticResourceFiles {
			err := helpers.CleanUpStaticObject(
				ctx,
				n.kubeClient,
				nil,
				nil,
				func(name string) ([]byte, error) {
					template, err := manifests.Klusterlet111ManifestFiles.ReadFile(name)
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
	}

	// remove the klusterlet namespace and klusterlet addon namespace
	namespaces := []string{config.KlusterletNamespace, fmt.Sprintf("%s-addon", config.KlusterletNamespace)}
	for _, namespace := range namespaces {
		err = n.kubeClient.CoreV1().Namespaces().Delete(ctx, namespace, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			return err
		}
	}

	// remove AppliedManifestWorks
	if len(hubHost) > 0 {
		if err := n.cleanUpAppliedManifestWorks(ctx, hubHost); err != nil {
			return err
		}
	}

	// remove the CRDs
	var crdStaticFiles []string
	// CRD v1beta1 was deprecated from k8s 1.16.0 and will be removed in k8s 1.22
	if cnt, err := n.kubeVersion.Compare("v1.16.0"); err == nil && cnt < 0 {
		crdStaticFiles = crdV1beta1StaticFiles
	} else {
		crdStaticFiles = crdV1StaticFiles
	}
	for _, file := range crdStaticFiles {
		err := helpers.CleanUpStaticObject(
			ctx,
			n.kubeClient,
			n.apiExtensionClient,
			nil,
			func(name string) ([]byte, error) {
				template, err := manifests.KlusterletManifestFiles.ReadFile(name)
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

func (n *klusterletController) removeKlusterletFinalizer(ctx context.Context, deploy *operatorapiv1.Klusterlet) error {
	// reload klusterlet
	deploy, err := n.klusterletClient.Get(ctx, deploy.Name, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	copiedFinalizers := []string{}
	for i := range deploy.Finalizers {
		if deploy.Finalizers[i] == klusterletFinalizer {
			continue
		}
		copiedFinalizers = append(copiedFinalizers, deploy.Finalizers[i])
	}
	if len(deploy.Finalizers) != len(copiedFinalizers) {
		deploy.Finalizers = copiedFinalizers
		_, err := n.klusterletClient.Update(ctx, deploy, metav1.UpdateOptions{})
		return err
	}

	return nil
}

// cleanUpAppliedManifestWorks removes finalizer from the AppliedManifestWorks whose name starts with
// the hash of the given hub host.
func (n *klusterletController) cleanUpAppliedManifestWorks(ctx context.Context, hubHost string) error {
	appliedManifestWorks, err := n.appliedManifestWorkClient.List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("unable to list AppliedManifestWorks: %w", err)
	}
	errs := []error{}
	prefix := fmt.Sprintf("%s-", fmt.Sprintf("%x", sha256.Sum256([]byte(hubHost))))
	for _, appliedManifestWork := range appliedManifestWorks.Items {
		// ignore AppliedManifestWork for other klusterlet
		if !strings.HasPrefix(appliedManifestWork.Name, prefix) {
			continue
		}

		// remove finalizer if exists
		if mutated := removeFinalizer(&appliedManifestWork, appliedManifestWorkFinalizer); !mutated {
			continue
		}

		_, err := n.appliedManifestWorkClient.Update(ctx, &appliedManifestWork, metav1.UpdateOptions{})
		if err != nil && !errors.IsNotFound(err) {
			errs = append(errs, fmt.Errorf("unable to remove finalizer from AppliedManifestWork %q: %w", appliedManifestWork.Name, err))
		}
	}
	return operatorhelpers.NewMultiLineAggregate(errs)
}

// removeFinalizer removes a finalizer from the list. It mutates its input.
func removeFinalizer(obj runtime.Object, finalizerName string) bool {
	if obj == nil || reflect.ValueOf(obj).IsNil() {
		return false
	}

	newFinalizers := []string{}
	accessor, _ := meta.Accessor(obj)
	found := false
	for _, finalizer := range accessor.GetFinalizers() {
		if finalizer == finalizerName {
			found = true
			continue
		}
		newFinalizers = append(newFinalizers, finalizer)
	}
	if found {
		accessor.SetFinalizers(newFinalizers)
	}
	return found
}

func readClusterNameFromSecret(secret *corev1.Secret) (string, error) {
	if secret.Data["cluster-name"] == nil {
		return "", fmt.Errorf("Unable to find cluster name in secret")
	}

	return string(secret.Data["cluster-name"]), nil
}

func readKubuConfigFromSecret(secret *corev1.Secret, config klusterletConfig) (string, error) {
	if secret.Data["kubeconfig"] == nil {
		return "", fmt.Errorf("Unable to find kubeconfig in secret")
	}

	return string(secret.Data["kubeconfig"]), nil
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
