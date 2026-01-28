package addon

import (
	"context"
	"fmt"
	"time"

	operatorhelpers "github.com/openshift/library-go/pkg/operator/v1helpers"
	certificatesv1 "k8s.io/api/certificates/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonclient "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformerv1alpha1 "open-cluster-management.io/api/client/addon/informers/externalversions/addon/v1alpha1"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	"open-cluster-management.io/ocm/pkg/common/queue"
	"open-cluster-management.io/ocm/pkg/registration/register"
)

// addOnRegistrationController monitors ManagedClusterAddOns on hub and starts addOn registration
// according to the registrationConfigs read from annotations of ManagedClusterAddOns. Echo addOn
// may have multiple registrationConfigs. A clientcert.NewClientCertificateController will be started
// for each of them.
type addOnRegistrationController struct {
	clusterName          string
	agentName            string
	kubeconfigFile       string
	managementKubeClient kubernetes.Interface // in-cluster local management kubeClient
	spokeKubeClient      kubernetes.Interface
	hubAddOnLister       addonlisterv1alpha1.ManagedClusterAddOnLister
	patcher              patcher.Patcher[
		*addonv1alpha1.ManagedClusterAddOn, addonv1alpha1.ManagedClusterAddOnSpec, addonv1alpha1.ManagedClusterAddOnStatus]
	addonDriverFactory register.AddonDriverFactory
	// addonAuthConfig holds the cluster-wide addon registration configuration
	// that provides authentication method and access to driver options (CSR, Token)
	addonAuthConfig register.AddonAuthConfig

	startRegistrationFunc func(ctx context.Context, config registrationConfig) (context.CancelFunc, error)

	// registrationConfigs maps the addon name to a map of registrationConfigs whose key is the hash of
	// the registrationConfig
	addOnRegistrationConfigs map[string]map[string]registrationConfig
}

// NewAddOnRegistrationController returns an instance of addOnRegistrationController
func NewAddOnRegistrationController(
	clusterName string,
	agentName string,
	kubeconfigFile string,
	addOnClient addonclient.Interface,
	managementKubeClient kubernetes.Interface,
	managedKubeClient kubernetes.Interface,
	addonDriverFactory register.AddonDriverFactory,
	addonAuthConfig register.AddonAuthConfig,
	hubAddOnInformers addoninformerv1alpha1.ManagedClusterAddOnInformer,
) factory.Controller {
	c := &addOnRegistrationController{
		clusterName:          clusterName,
		agentName:            agentName,
		kubeconfigFile:       kubeconfigFile,
		managementKubeClient: managementKubeClient,
		spokeKubeClient:      managedKubeClient,
		hubAddOnLister:       hubAddOnInformers.Lister(),
		addonDriverFactory:   addonDriverFactory,
		addonAuthConfig:      addonAuthConfig,
		patcher: patcher.NewPatcher[
			*addonv1alpha1.ManagedClusterAddOn, addonv1alpha1.ManagedClusterAddOnSpec, addonv1alpha1.ManagedClusterAddOnStatus](
			addOnClient.AddonV1alpha1().ManagedClusterAddOns(clusterName)),
		addOnRegistrationConfigs: map[string]map[string]registrationConfig{},
	}

	c.startRegistrationFunc = c.startRegistration

	return factory.New().
		WithInformersQueueKeysFunc(
			queue.QueueKeyByMetaName,
			hubAddOnInformers.Informer()).
		WithSync(c.sync).
		ResyncEvery(10 * time.Minute).
		ToController("AddOnRegistrationController")
}

func (c *addOnRegistrationController) sync(ctx context.Context, syncCtx factory.SyncContext, queueKey string) error {
	if queueKey != factory.DefaultQueueKey {
		// sync a particular addOn
		return c.syncAddOn(ctx, syncCtx, queueKey)
	}

	// handle resync
	var errs []error
	for addOnName := range c.addOnRegistrationConfigs {
		_, err := c.hubAddOnLister.ManagedClusterAddOns(c.clusterName).Get(addOnName)
		if err == nil {
			syncCtx.Queue().Add(addOnName)
			continue
		}
		if errors.IsNotFound(err) {
			// clean up if the addOn no longer exists
			err = c.cleanup(ctx, addOnName)
		}
		if err != nil {
			errs = append(errs, err)
		}
	}

	return operatorhelpers.NewMultiLineAggregate(errs)
}

func (c *addOnRegistrationController) syncAddOn(ctx context.Context, syncCtx factory.SyncContext, addOnName string) error {
	logger := klog.FromContext(ctx).WithValues("addOnName", addOnName)
	logger.V(4).Info("Reconciling addOn")
	addOn, err := c.hubAddOnLister.ManagedClusterAddOns(c.clusterName).Get(addOnName)
	if errors.IsNotFound(err) {
		// addon is deleted
		return c.cleanup(ctx, addOnName)
	}
	if err != nil {
		return err
	}

	// do not clean up during addon is deleting.
	// in some case the addon agent may need hub-kubeconfig to do cleanup during deleting.
	if !addOn.DeletionTimestamp.IsZero() {
		return nil
	}

	// Ensure driver field is set for kubeClient type registrations
	// If updated, return early to allow re-sync with the updated state
	if updated, err := c.ensureDriver(ctx, addOn); err != nil || updated {
		return err
	}

	installOption := addonInstallOption{
		AgentRunningOutsideManagedCluster: isAddonRunningOutsideManagedCluster(addOn),
		InstallationNamespace:             getAddOnInstallationNamespace(addOn),
	}
	configs, err := getRegistrationConfigs(addOnName, installOption, addOn.Status.Registrations, addOn.Status.KubeClientDriver)
	if err != nil {
		return err
	}

	// stop registration for the stale registration configs
	var errs []error
	cachedConfigs := c.addOnRegistrationConfigs[addOnName]
	for hash, cachedConfig := range cachedConfigs {
		if _, ok := configs[hash]; ok {
			continue
		}

		if err := c.stopRegistration(ctx, cachedConfig); err != nil {
			errs = append(errs, err)
		}
	}
	if err := operatorhelpers.NewMultiLineAggregate(errs); err != nil {
		return err
	}

	syncedConfigs := map[string]registrationConfig{}
	for hash, config := range configs {
		// keep the unchanged configs
		if cachedConfig, ok := cachedConfigs[hash]; ok {
			// Hash matches - keep the existing controller running
			// Note: Driver is always excluded from hash, Subject is conditionally included based on driver type
			syncedConfigs[hash] = cachedConfig
			continue
		}

		// start registration for the new added configs
		stopFunc, err := c.startRegistrationFunc(ctx, config)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		config.stopFunc = stopFunc
		syncedConfigs[hash] = config
	}

	if len(syncedConfigs) == 0 {
		delete(c.addOnRegistrationConfigs, addOnName)
		return operatorhelpers.NewMultiLineAggregate(errs)
	}
	c.addOnRegistrationConfigs[addOnName] = syncedConfigs
	return operatorhelpers.NewMultiLineAggregate(errs)
}

// startRegistration starts a client certificate controller with the given config
func (c *addOnRegistrationController) startRegistration(ctx context.Context, config registrationConfig) (context.CancelFunc, error) {
	// the kubeClient here will be used to generate the hub kubeconfig secret for addon agents, it generates the secret
	// on the managed cluster by default, but if the addon agent is not running on the managed cluster(in Hosted mode
	// the addon agent runs outside the managed cluster, for more details see the hosted mode design docs for addon:
	// https://github.com/open-cluster-management-io/enhancements/pull/65), it generate the secret on the
	// management(hosting) cluster
	kubeClient := c.spokeKubeClient
	if config.AgentRunningOutsideManagedCluster {
		kubeClient = c.managementKubeClient
	}

	kubeInformerFactory := informers.NewSharedInformerFactoryWithOptions(
		kubeClient, 10*time.Minute, informers.WithNamespace(config.InstallationNamespace))

	secretOption := register.SecretOption{
		SecretNamespace: config.InstallationNamespace,
		SecretName:      config.secretName,
		Subject:         config.x509Subject(c.clusterName, c.agentName),
		Signer:          config.registration.SignerName,
		ClusterName:     c.clusterName,
	}

	if config.registration.SignerName == certificatesv1.KubeAPIServerClientSignerName {
		secretOption.BootStrapKubeConfigFile = c.kubeconfigFile
	}

	// Pass AddonAuthConfig directly to Fork
	// It provides authentication method and access to driver options (CSR, Token)
	// Registration type (KubeClient vs CustomSigner) is determined by Fork implementation
	// based on secretOption.Signer
	driver, err := c.addonDriverFactory.Fork(config.addOnName, c.addonAuthConfig, secretOption)
	if err != nil {
		return nil, fmt.Errorf("failed to create driver for addon %s: %w", config.addOnName, err)
	}

	controllerName := fmt.Sprintf("ClientCertController@addon:%s:signer:%s", config.addOnName, config.registration.SignerName)
	statusUpdater := c.generateStatusUpdate(c.clusterName, config.addOnName)
	secretController := register.NewSecretController(
		secretOption, driver, statusUpdater,
		kubeClient.CoreV1(),
		kubeInformerFactory.Core().V1().Secrets().Informer(),
		controllerName)

	ctx, stopFunc := context.WithCancel(ctx)
	go kubeInformerFactory.Start(ctx.Done())
	go secretController.Run(ctx, 1)

	return stopFunc, nil
}

func (c *addOnRegistrationController) generateStatusUpdate(clusterName, addonName string) register.StatusUpdateFunc {
	return func(ctx context.Context, cond metav1.Condition) error {
		addon, err := c.hubAddOnLister.ManagedClusterAddOns(clusterName).Get(addonName)
		if errors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}

		newAddon := addon.DeepCopy()
		meta.SetStatusCondition(&newAddon.Status.Conditions, cond)
		_, err = c.patcher.PatchStatus(ctx, newAddon, newAddon.Status, addon.Status)
		return err
	}
}

// stopRegistration stops the client certificate controller for the given config
func (c *addOnRegistrationController) stopRegistration(ctx context.Context, config registrationConfig) error {
	if config.stopFunc != nil {
		config.stopFunc()
	}

	kubeClient := c.spokeKubeClient
	if config.AgentRunningOutsideManagedCluster {
		// delete the secret generated on the management cluster
		kubeClient = c.managementKubeClient
	}

	err := kubeClient.CoreV1().Secrets(config.InstallationNamespace).
		Delete(ctx, config.secretName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	return nil
}

// ensureDriver sets Status.KubeClientDriver based on agent configuration to declare
// the authentication capability (csr or token) to the hub controller.
// Only sets the driver if the addon has a KubeClient type registration (KubeAPIServerClientSignerName).
// For custom signers, the driver is set to empty string.
func (c *addOnRegistrationController) ensureDriver(ctx context.Context, addon *addonv1alpha1.ManagedClusterAddOn) (bool, error) {
	logger := klog.FromContext(ctx)

	// Check if addon has any KubeClient type registrations
	hasKubeClientRegistration := false
	for _, reg := range addon.Status.Registrations {
		if reg.SignerName == certificatesv1.KubeAPIServerClientSignerName {
			hasKubeClientRegistration = true
			break
		}
	}

	addonCopy := addon.DeepCopy()
	if hasKubeClientRegistration {
		addonCopy.Status.KubeClientDriver = c.addonAuthConfig.GetKubeClientAuth()
	} else {
		addonCopy.Status.KubeClientDriver = ""
	}

	updated, err := c.patcher.PatchStatus(ctx, addonCopy, addonCopy.Status, addon.Status)
	if err != nil {
		return false, err
	}

	if updated {
		logger.Info("Updated kubeClientDriver in status", "addon", addon.Name, "driver", addonCopy.Status.KubeClientDriver, "hasKubeClientRegistration", hasKubeClientRegistration)
	}

	return updated, nil
}

// cleanup cleans both the registration configs and client certificate controllers for the addon
func (c *addOnRegistrationController) cleanup(ctx context.Context, addOnName string) error {
	var errs []error
	for _, config := range c.addOnRegistrationConfigs[addOnName] {
		if err := c.stopRegistration(ctx, config); err != nil {
			errs = append(errs, err)
		}
	}

	if err := operatorhelpers.NewMultiLineAggregate(errs); err != nil {
		return err
	}

	delete(c.addOnRegistrationConfigs, addOnName)
	return nil
}
