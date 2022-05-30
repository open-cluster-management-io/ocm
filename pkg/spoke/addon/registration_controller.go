package addon

import (
	"context"
	"fmt"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	operatorhelpers "github.com/openshift/library-go/pkg/operator/v1helpers"
	certificatesv1 "k8s.io/api/certificates/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/informers"
	certificatesinformers "k8s.io/client-go/informers/certificates"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	addonclient "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformerv1alpha1 "open-cluster-management.io/api/client/addon/informers/externalversions/addon/v1alpha1"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"
	"open-cluster-management.io/registration/pkg/clientcert"
	"open-cluster-management.io/registration/pkg/helpers"
)

// addOnRegistrationController monitors ManagedClusterAddOns on hub and starts addOn registration
// according to the registrationConfigs read from annotations of ManagedClusterAddOns. Echo addOn
// may have multiple registrationConfigs. A clientcert.NewClientCertificateController will be started
// for each of them.
type addOnRegistrationController struct {
	clusterName     string
	agentName       string
	kubeconfigData  []byte
	spokeKubeClient kubernetes.Interface
	hubAddOnLister  addonlisterv1alpha1.ManagedClusterAddOnLister
	hubCSRInformer  certificatesinformers.Interface
	hubKubeClient   kubernetes.Interface
	addOnClient     addonclient.Interface
	recorder        events.Recorder

	startRegistrationFunc func(ctx context.Context, config registrationConfig) context.CancelFunc

	// registrationConfigs maps the addon name to a map of registrationConfigs whose key is the hash of
	// the registrationConfig
	addOnRegistrationConfigs map[string]map[string]registrationConfig
}

// NewAddOnRegistrationController returns an instance of addOnRegistrationController
func NewAddOnRegistrationController(
	clusterName string,
	agentName string,
	kubeconfigData []byte,
	kubeClient kubernetes.Interface,
	addOnClient addonclient.Interface,
	hubCSRInformer certificatesinformers.Interface,
	hubAddOnInformers addoninformerv1alpha1.ManagedClusterAddOnInformer,
	hubCSRClient kubernetes.Interface,
	recorder events.Recorder,
) factory.Controller {
	c := &addOnRegistrationController{
		clusterName:              clusterName,
		agentName:                agentName,
		kubeconfigData:           kubeconfigData,
		spokeKubeClient:          kubeClient,
		hubAddOnLister:           hubAddOnInformers.Lister(),
		hubCSRInformer:           hubCSRInformer,
		hubKubeClient:            hubCSRClient,
		addOnClient:              addOnClient,
		recorder:                 recorder,
		addOnRegistrationConfigs: map[string]map[string]registrationConfig{},
	}

	c.startRegistrationFunc = c.startRegistration

	return factory.New().
		WithInformersQueueKeyFunc(
			func(obj runtime.Object) string {
				accessor, _ := meta.Accessor(obj)
				return accessor.GetName()
			},
			hubAddOnInformers.Informer()).
		WithSync(c.sync).
		ResyncEvery(10*time.Minute).
		ToController("AddOnRegistrationController", recorder)
}

func (c *addOnRegistrationController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	queueKey := syncCtx.QueueKey()
	if queueKey != factory.DefaultQueueKey {
		// sync a particular addOn
		return c.syncAddOn(ctx, syncCtx, queueKey)
	}

	// handle resync
	errs := []error{}
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
	klog.V(4).Infof("Reconciling addOn %q", addOnName)

	addOn, err := c.hubAddOnLister.ManagedClusterAddOns(c.clusterName).Get(addOnName)
	if errors.IsNotFound(err) {
		// addon is deleted
		return c.cleanup(ctx, addOnName)
	}
	if err != nil {
		return err
	}

	// addon is deleting
	if !addOn.DeletionTimestamp.IsZero() {
		return c.cleanup(ctx, addOnName)
	}

	cachedConfigs := c.addOnRegistrationConfigs[addOnName]
	configs, err := getRegistrationConfigs(addOn)
	if err != nil {
		return err
	}

	// stop registration for the stale registration configs
	errs := []error{}
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
			syncedConfigs[hash] = cachedConfig
			continue
		}

		// start registration for the new added configs
		config.stopFunc = c.startRegistrationFunc(ctx, config)
		syncedConfigs[hash] = config
	}

	if len(syncedConfigs) == 0 {
		delete(c.addOnRegistrationConfigs, addOnName)
		return nil
	}
	c.addOnRegistrationConfigs[addOnName] = syncedConfigs
	return nil
}

// startRegistration starts a client certificate controller with the given config
func (c *addOnRegistrationController) startRegistration(ctx context.Context, config registrationConfig) context.CancelFunc {
	ctx, stopFunc := context.WithCancel(ctx)
	kubeInformerFactory := informers.NewSharedInformerFactoryWithOptions(c.spokeKubeClient, 10*time.Minute, informers.WithNamespace(config.installationNamespace))

	additonalSecretData := map[string][]byte{}
	if config.registration.SignerName == certificatesv1.KubeAPIServerClientSignerName {
		additonalSecretData[clientcert.KubeconfigFile] = c.kubeconfigData
	}

	// build and start a client cert controller
	clientCertOption := clientcert.ClientCertOption{
		SecretNamespace:               config.installationNamespace,
		SecretName:                    config.secretName,
		AdditionalSecretData:          additonalSecretData,
		AdditionalSecretDataSensitive: true,
	}

	csrOption := clientcert.CSROption{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("addon-%s-%s-", c.clusterName, config.addOnName),
			Labels: map[string]string{
				// the labels are only hints. Anyone could set/modify them.
				clientcert.ClusterNameLabel: c.clusterName,
				clientcert.AddonNameLabel:   config.addOnName,
			},
		},
		Subject:         config.x509Subject(c.clusterName, c.agentName),
		DNSNames:        []string{fmt.Sprintf("%s.addon.open-cluster-management.io", config.addOnName)},
		SignerName:      config.registration.SignerName,
		EventFilterFunc: createCSREventFilterFunc(c.clusterName, config.addOnName, config.registration.SignerName),
	}

	controllerName := fmt.Sprintf("ClientCertController@addon:%s:signer:%s", config.addOnName, config.registration.SignerName)

	statusUpdater := c.generateStatusUpdate(c.clusterName, config.addOnName)

	clientCertController, err := clientcert.NewClientCertificateController(
		clientCertOption,
		csrOption,
		c.hubCSRInformer,
		kubeInformerFactory.Core().V1().Secrets(),
		c.spokeKubeClient,
		c.hubKubeClient,
		statusUpdater,
		c.recorder,
		controllerName,
	)
	if err != nil {
		utilruntime.HandleError(err)
	}

	go kubeInformerFactory.Start(ctx.Done())
	go clientCertController.Run(ctx, 1)

	return stopFunc
}

func (c *addOnRegistrationController) generateStatusUpdate(clusterName, addonName string) clientcert.StatusUpdateFunc {
	return func(ctx context.Context, cond metav1.Condition) error {
		_, _, updatedErr := helpers.UpdateManagedClusterAddOnStatus(
			ctx, c.addOnClient, clusterName, addonName, helpers.UpdateManagedClusterAddOnStatusFn(cond),
		)

		return updatedErr
	}
}

// stopRegistration stops the client certificate controller for the given config
func (c *addOnRegistrationController) stopRegistration(ctx context.Context, config registrationConfig) error {
	if config.stopFunc != nil {
		config.stopFunc()
	}

	// delete the secret generated
	err := c.spokeKubeClient.CoreV1().Secrets(config.installationNamespace).Delete(ctx, config.secretName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	return nil
}

// cleanup cleans both the registration configs and client certificate controllers for the addon
func (c *addOnRegistrationController) cleanup(ctx context.Context, addOnName string) error {
	errs := []error{}
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

func createCSREventFilterFunc(clusterName, addOnName, signerName string) factory.EventFilterFunc {
	return func(obj interface{}) bool {
		accessor, err := meta.Accessor(obj)
		if err != nil {
			return false
		}
		labels := accessor.GetLabels()
		// only enqueue csr from a specific managed cluster
		if labels[clientcert.ClusterNameLabel] != clusterName {
			return false
		}
		// only enqueue csr created for a specific addon
		if labels[clientcert.AddonNameLabel] != addOnName {
			return false
		}

		// only enqueue csr with a specific signer name
		csr, ok := obj.(*certificatesv1.CertificateSigningRequest)
		if !ok {
			return false
		}
		if len(csr.Spec.SignerName) == 0 {
			return false
		}
		if csr.Spec.SignerName != signerName {
			return false
		}
		return true
	}
}
