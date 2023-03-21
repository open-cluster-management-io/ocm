package klusterletcontroller

import (
	"context"
	"github.com/openshift/library-go/pkg/assets"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/version"
	appsinformer "k8s.io/client-go/informers/apps/v1"
	coreinformer "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"open-cluster-management.io/registration-operator/manifests"
	"reflect"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	operatorv1client "open-cluster-management.io/api/client/operator/clientset/versioned/typed/operator/v1"
	operatorinformer "open-cluster-management.io/api/client/operator/informers/externalversions/operator/v1"
	operatorlister "open-cluster-management.io/api/client/operator/listers/operator/v1"
	workv1client "open-cluster-management.io/api/client/work/clientset/versioned/typed/work/v1"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"
	"open-cluster-management.io/registration-operator/pkg/helpers"
)

type klusterletCleanupController struct {
	klusterletClient             operatorv1client.KlusterletInterface
	klusterletLister             operatorlister.KlusterletLister
	kubeClient                   kubernetes.Interface
	kubeVersion                  *version.Version
	operatorNamespace            string
	managedClusterClientsBuilder managedClusterClientsBuilderInterface
}

// NewKlusterletCleanupController construct klusterlet cleanup controller
func NewKlusterletCleanupController(
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
	controller := &klusterletCleanupController{
		kubeClient:                   kubeClient,
		klusterletClient:             klusterletClient,
		klusterletLister:             klusterletInformer.Lister(),
		kubeVersion:                  kubeVersion,
		operatorNamespace:            operatorNamespace,
		managedClusterClientsBuilder: newManagedClusterClientsBuilder(kubeClient, apiExtensionClient, appliedManifestWorkClient),
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

	if klusterlet.DeletionTimestamp.IsZero() {
		if !hasFinalizer(klusterlet, klusterletFinalizer) {
			return n.addFinalizer(ctx, klusterlet, klusterletFinalizer)
		}

		if !hasFinalizer(klusterlet, klusterletHostedFinalizer) && readyToAddHostedFinalizer(klusterlet, klusterlet.Spec.DeployOption.Mode) {
			// the external managed kubeconfig secret is ready, there will be some resources applied on the managed
			// cluster, add hosted finalizer here to indicate these resources should be cleaned up when deleting the
			// klusterlet
			return n.addFinalizer(ctx, klusterlet, klusterletHostedFinalizer)
		}

		return nil
	}

	// Klusterlet is deleting, we remove its related resources on managed and management cluster
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
		InstallMode:                                 klusterlet.Spec.DeployOption.Mode,
		HubApiServerHostAlias:                       klusterlet.Spec.HubApiServerHostAlias,
	}

	reconcilers := []klusterletReconcile{
		&runtimeReconcile{
			kubeClient: n.kubeClient,
			recorder:   controllerContext.Recorder(),
		},
	}

	// Add other reconcilers only when managed cluster is ready to manage.
	// we should clean managedcluster resource when
	// 1. install mode is not hosted
	// 2. install mode is hosted and some resources has been applied on managed cluster (if hosted finalizer exists)
	if config.InstallMode != operatorapiv1.InstallModeHosted || hasFinalizer(klusterlet, klusterletHostedFinalizer) {
		managedClusterClients, err := n.managedClusterClientsBuilder.
			withMode(config.InstallMode).
			withKubeConfigSecret(config.AgentNamespace, config.ExternalManagedKubeConfigSecret).
			build(ctx)

		// stop when hosted kubeconfig is not found. the klustelet controller will monitor the secret and retrigger
		// reconcilation of cleanup controller when secret is created again.
		if errors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}

		reconcilers = append(reconcilers,
			&crdReconcile{
				managedClusterClients: managedClusterClients,
				kubeVersion:           n.kubeVersion,
				recorder:              controllerContext.Recorder(),
			},
			&managedReconcile{
				managedClusterClients: managedClusterClients,
				kubeClient:            n.kubeClient,
				kubeVersion:           n.kubeVersion,
				opratorNamespace:      n.operatorNamespace,
				recorder:              controllerContext.Recorder(),
			},
		)
	}
	// managementReconcile should be added as the last one, since we finally need to remove agent namespace.
	reconcilers = append(reconcilers, &managementReconcile{
		kubeClient:        n.kubeClient,
		operatorNamespace: n.operatorNamespace,
		recorder:          controllerContext.Recorder(),
	})

	var errs []error
	for _, reconciler := range reconcilers {
		var state reconcileState
		klusterlet, state, err = reconciler.clean(ctx, klusterlet, config)
		if err != nil {
			errs = append(errs, err)
		}
		if state == reconcileStop {
			break
		}
	}

	if len(errs) > 0 {
		return utilerrors.NewAggregate(errs)
	}

	return n.removeKlusterletFinalizers(ctx, klusterlet)
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
	var copiedFinalizers []string
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

// readyToAddHostedFinalizer checkes whether the hosted finalizer should be added.
// It is only added when mode is hosted, and some resources have been applied to the managed cluster.
func readyToAddHostedFinalizer(klusterlet *operatorapiv1.Klusterlet, mode operatorapiv1.InstallMode) bool {
	if mode != operatorapiv1.InstallModeHosted {
		return false
	}

	return meta.IsStatusConditionTrue(klusterlet.Status.Conditions, klusterletReadyToApply)
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

	var newFinalizers []string
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

func removeStaticResources(ctx context.Context,
	kubeClient kubernetes.Interface,
	apiExtensionClient apiextensionsclient.Interface,
	resources []string,
	config klusterletConfig) error {
	for _, file := range resources {
		err := helpers.CleanUpStaticObject(
			ctx,
			kubeClient,
			apiExtensionClient,
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
