package klusterletcontroller

import (
	"context"
	"crypto/sha256"
	"fmt"
	"reflect"
	"strings"

	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/version"
	"k8s.io/client-go/dynamic"
	appsinformer "k8s.io/client-go/informers/apps/v1"
	coreinformer "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	operatorhelpers "github.com/openshift/library-go/pkg/operator/v1helpers"

	operatorv1client "open-cluster-management.io/api/client/operator/clientset/versioned/typed/operator/v1"
	operatorinformer "open-cluster-management.io/api/client/operator/informers/externalversions/operator/v1"
	operatorlister "open-cluster-management.io/api/client/operator/listers/operator/v1"
	workv1client "open-cluster-management.io/api/client/work/clientset/versioned/typed/work/v1"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"
	"open-cluster-management.io/registration-operator/pkg/helpers"
)

type klusterletCleanupController struct {
	klusterletClient          operatorv1client.KlusterletInterface
	klusterletLister          operatorlister.KlusterletLister
	kubeClient                kubernetes.Interface
	apiExtensionClient        apiextensionsclient.Interface
	dynamicClient             dynamic.Interface
	appliedManifestWorkClient workv1client.AppliedManifestWorkInterface
	kubeVersion               *version.Version
	operatorNamespace         string

	// buildManagedClusterClientsHostedMode build clients for the managed cluster in hosted mode,
	// this can be overridden for testing
	buildManagedClusterClientsHostedMode func(
		ctx context.Context,
		kubeClient kubernetes.Interface,
		namespace, secret string) (*managedClusterClients, error)
}

// NewKlusterletCleanupController construct klusterlet cleanup controller
func NewKlusterletCleanupController(
	kubeClient kubernetes.Interface,
	apiExtensionClient apiextensionsclient.Interface,
	dynamicClient dynamic.Interface,
	klusterletClient operatorv1client.KlusterletInterface,
	klusterletInformer operatorinformer.KlusterletInformer,
	secretInformer coreinformer.SecretInformer,
	deploymentInformer appsinformer.DeploymentInformer,
	appliedManifestWorkClient workv1client.AppliedManifestWorkInterface,
	kubeVersion *version.Version,
	operatorNamespace string,
	recorder events.Recorder) factory.Controller {
	controller := &klusterletCleanupController{
		kubeClient:                           kubeClient,
		apiExtensionClient:                   apiExtensionClient,
		dynamicClient:                        dynamicClient,
		klusterletClient:                     klusterletClient,
		klusterletLister:                     klusterletInformer.Lister(),
		appliedManifestWorkClient:            appliedManifestWorkClient,
		kubeVersion:                          kubeVersion,
		operatorNamespace:                    operatorNamespace,
		buildManagedClusterClientsHostedMode: buildManagedClusterClientsFromSecret,
	}

	return factory.New().WithSync(controller.sync).
		WithInformersQueueKeyFunc(helpers.KlusterletSecretQueueKeyFunc(controller.klusterletLister), secretInformer.Informer()).
		WithInformersQueueKeyFunc(helpers.KlusterletDeploymentQueueKeyFunc(controller.klusterletLister), deploymentInformer.Informer()).
		WithInformersQueueKeyFunc(func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			return accessor.GetName()
		}, klusterletInformer.Informer()).
		ToController("KlusterletCleanupController", recorder)
}

func (n *klusterletCleanupController) sync(ctx context.Context, controllerContext factory.SyncContext) error {
	klusterletName := controllerContext.QueueKey()
	klog.V(4).Infof("Reconciling Klusterlet %q", klusterletName)
	klusterlet, err := n.klusterletLister.Get(klusterletName)
	if errors.IsNotFound(err) {
		// Klusterlet not found, could have been deleted, do nothing.
		return nil
	}
	if err != nil {
		return err
	}
	klusterlet = klusterlet.DeepCopy()
	installMode := klusterlet.Spec.DeployOption.Mode

	if klusterlet.DeletionTimestamp.IsZero() {
		if !hasFinalizer(klusterlet, klusterletFinalizer) {
			return n.addFinalizer(ctx, klusterlet, klusterletFinalizer)
		}

		if !hasFinalizer(klusterlet, klusterletHostedFinalizer) && readyToAddHostedFinalizer(klusterlet, installMode) {
			// the external managed kubeconfig secret is ready, there will be some resources applied on the managed
			// cluster, add hosted finalizer here to indicate these resources should be cleaned up when deleting the
			// klusterlet
			return n.addFinalizer(ctx, klusterlet, klusterletHostedFinalizer)
		}

		return nil
	}

	// Klusterlet is deleting, we remove its related resources on managed and management cluster
	skip := skipCleanupManagedClusterResources(klusterlet, installMode)

	if !skip && !readyToOperateManagedClusterResources(klusterlet, installMode) {
		// wait for the external managed kubeconfig to exist to clean up resources on the managed cluster
		return nil
	}

	config := klusterletConfig{
		KlusterletName:            klusterlet.Name,
		KlusterletNamespace:       helpers.KlusterletNamespace(klusterlet),
		AgentNamespace:            helpers.AgentNamespace(klusterlet),
		RegistrationImage:         klusterlet.Spec.RegistrationImagePullSpec,
		WorkImage:                 klusterlet.Spec.WorkImagePullSpec,
		ClusterName:               klusterlet.Spec.ClusterName,
		BootStrapKubeConfigSecret: helpers.BootstrapHubKubeConfig,
		HubKubeConfigSecret:       helpers.HubKubeConfig,
		ExternalServerURL:         getServersFromKlusterlet(klusterlet),
		OperatorNamespace:         n.operatorNamespace,
		Replica:                   helpers.DetermineReplica(ctx, n.kubeClient, klusterlet.Spec.DeployOption.Mode, n.kubeVersion),

		ExternalManagedKubeConfigSecret:             helpers.ExternalManagedKubeConfig,
		ExternalManagedKubeConfigRegistrationSecret: helpers.ExternalManagedKubeConfigRegistration,
		ExternalManagedKubeConfigWorkSecret:         helpers.ExternalManagedKubeConfigWork,
		InstallMode:                                 installMode,
		HubApiServerHostAlias:                       klusterlet.Spec.HubApiServerHostAlias,
	}

	managedClusterClients := &managedClusterClients{
		kubeClient:                n.kubeClient,
		apiExtensionClient:        n.apiExtensionClient,
		dynamicClient:             n.dynamicClient,
		appliedManifestWorkClient: n.appliedManifestWorkClient,
	}
	if installMode == operatorapiv1.InstallModeHosted && !skip {
		managedClusterClients, err = n.buildManagedClusterClientsHostedMode(ctx,
			n.kubeClient, config.AgentNamespace, config.ExternalManagedKubeConfigSecret)
		if err != nil {
			return err
		}
	}

	if err := n.cleanUp(ctx, controllerContext, managedClusterClients, config, skip); err != nil {
		return err
	}

	return n.removeKlusterletFinalizers(ctx, klusterlet)
}

func (n *klusterletCleanupController) cleanUp(
	ctx context.Context,
	controllerContext factory.SyncContext,
	managedClients *managedClusterClients,
	config klusterletConfig,
	skipCleanupResourcesOnManagedCluster bool) error {
	// Remove deployment
	deployments := []string{
		fmt.Sprintf("%s-registration-agent", config.KlusterletName),
		fmt.Sprintf("%s-work-agent", config.KlusterletName),
	}
	for _, deployment := range deployments {
		err := n.kubeClient.AppsV1().Deployments(config.AgentNamespace).Delete(ctx, deployment, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			return err
		}
		controllerContext.Recorder().Eventf("DeploymentDeleted", "deployment %s is deleted", deployment)
	}

	if !skipCleanupResourcesOnManagedCluster {
		// get hub host from bootstrap kubeconfig
		var hubHost string
		bootstrapKubeConfigSecret, err := n.kubeClient.CoreV1().Secrets(config.AgentNamespace).Get(ctx, config.BootStrapKubeConfigSecret, metav1.GetOptions{})
		switch {
		case err == nil:
			restConfig, err := helpers.LoadClientConfigFromSecret(bootstrapKubeConfigSecret)
			if err != nil {
				return fmt.Errorf("unable to load kubeconfig from secret %q %q: %w", config.AgentNamespace, config.BootStrapKubeConfigSecret, err)
			}
			hubHost = restConfig.Host
		case !errors.IsNotFound(err):
			return err
		}

		err = n.cleanUpManagedClusterResources(ctx, managedClients, config, hubHost)
		if err != nil {
			return err
		}
	}

	// Remove secrets
	secrets := []string{config.HubKubeConfigSecret}
	if config.InstallMode == operatorapiv1.InstallModeHosted {
		// In Hosted mod, also need to remove the external-managed-kubeconfig-registration and external-managed-kubeconfig-work
		secrets = append(secrets, []string{config.ExternalManagedKubeConfigRegistrationSecret, config.ExternalManagedKubeConfigWorkSecret}...)
	}
	for _, secret := range secrets {
		err := n.kubeClient.CoreV1().Secrets(config.AgentNamespace).Delete(ctx, secret, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			return err
		}
		controllerContext.Recorder().Eventf("SecretDeleted", "secret %s is deleted", secret)
	}

	// remove static file on the management cluster
	err := removeStaticResources(ctx, n.kubeClient, n.apiExtensionClient, managementStaticResourceFiles, config)
	if err != nil {
		return err
	}

	// The agent namespace on the management cluster should be removed **at the end**. Otherwise if any failure occurred,
	// the managed-external-kubeconfig secret would be removed and the next reconcile will fail due to can not build the
	// managed cluster clients.
	if config.InstallMode == operatorapiv1.InstallModeHosted {
		// remove the agent namespace on the management cluster
		err = n.kubeClient.CoreV1().Namespaces().Delete(ctx, config.AgentNamespace, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			return err
		}
	}

	return nil
}

func (n *klusterletCleanupController) cleanUpManagedClusterResources(
	ctx context.Context,
	managedClients *managedClusterClients,
	config klusterletConfig,
	hubHost string) error {
	// remove finalizer from AppliedManifestWorks, should be executed **before** "remove hub kubeconfig secret".
	if len(hubHost) > 0 {
		if err := n.cleanUpAppliedManifestWorks(ctx, managedClients.appliedManifestWorkClient, hubHost); err != nil {
			return err
		}
	}

	// remove static file on the managed cluster
	err := removeStaticResources(ctx, managedClients.kubeClient, managedClients.apiExtensionClient,
		managedStaticResourceFiles, config)
	if err != nil {
		return err
	}

	// TODO remove this when we do not support kube 1.11 any longer
	cnt, err := n.kubeVersion.Compare("v1.12.0")
	klog.Infof("comapare version %d, %v", cnt, err)
	if cnt, err := n.kubeVersion.Compare("v1.12.0"); err == nil && cnt < 0 {
		err = removeStaticResources(ctx, managedClients.kubeClient, managedClients.apiExtensionClient,
			kube111StaticResourceFiles, config)
		if err != nil {
			return err
		}
	}

	// remove the klusterlet namespace and klusterlet addon namespace on the managed cluster
	// For now, whether in Default or Hosted mode, the addons could be deployed on the managed cluster.
	namespaces := []string{config.KlusterletNamespace, fmt.Sprintf("%s-addon", config.KlusterletNamespace)}
	for _, namespace := range namespaces {
		err = managedClients.kubeClient.CoreV1().Namespaces().Delete(ctx, namespace, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			return err
		}
	}

	// no longer remove the CRDs (AppliedManifestWork & ClusterClaim), because they might be shared
	// by multiple klusterlets. Consequently, the CRs of those CRDs will not be deleted as well when deleting a klusterlet.

	return nil
}

func (n *klusterletCleanupController) removeKlusterletFinalizers(ctx context.Context, deploy *operatorapiv1.Klusterlet) error {
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
		if deploy.Finalizers[i] == klusterletFinalizer || deploy.Finalizers[i] == klusterletHostedFinalizer {
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
func (n *klusterletCleanupController) cleanUpAppliedManifestWorks(ctx context.Context, appliedManifestWorkClient workv1client.AppliedManifestWorkInterface, hubHost string) error {
	appliedManifestWorks, err := appliedManifestWorkClient.List(ctx, metav1.ListOptions{})
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

		_, err := appliedManifestWorkClient.Update(ctx, &appliedManifestWork, metav1.UpdateOptions{})
		if err != nil && !errors.IsNotFound(err) {
			errs = append(errs, fmt.Errorf("unable to remove finalizer from AppliedManifestWork %q: %w", appliedManifestWork.Name, err))
		}
	}
	return operatorhelpers.NewMultiLineAggregate(errs)
}

func readyToAddHostedFinalizer(klusterlet *operatorapiv1.Klusterlet, mode operatorapiv1.InstallMode) bool {
	if mode != operatorapiv1.InstallModeHosted {
		return false
	}

	return meta.IsStatusConditionTrue(klusterlet.Status.Conditions, klusterletReadyToApply)
}

func skipCleanupManagedClusterResources(klusterlet *operatorapiv1.Klusterlet, mode operatorapiv1.InstallMode) bool {
	if mode != operatorapiv1.InstallModeHosted {
		return false
	}

	return !hasFinalizer(klusterlet, klusterletHostedFinalizer)
}

func (n *klusterletCleanupController) addFinalizer(ctx context.Context, k *operatorapiv1.Klusterlet, finalizer string) error {
	k.Finalizers = append(k.Finalizers, finalizer)
	_, err := n.klusterletClient.Update(ctx, k, metav1.UpdateOptions{})
	return err
}

func hasFinalizer(klusterlet *operatorapiv1.Klusterlet, finalizer string) bool {
	for _, f := range klusterlet.Finalizers {
		if f == finalizer {
			return true
		}
	}
	return false
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
