package addon

import (
	"context"
	"fmt"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
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
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	addonclient "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformerv1alpha1 "open-cluster-management.io/api/client/addon/informers/externalversions/addon/v1alpha1"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"
	"open-cluster-management.io/registration/pkg/clientcert"
	"open-cluster-management.io/registration/pkg/helpers"
)

const (
	indexByAddon = "indexByAddon"

	// TODO(qiujian16) expose it if necessary in the future.
	addonCSRThreshold = 10
)

// addOnRegistrationController monitors ManagedClusterAddOns on hub and starts addOn registration
// according to the registrationConfigs read from annotations of ManagedClusterAddOns. Echo addOn
// may have multiple registrationConfigs. A clientcert.NewClientCertificateController will be started
// for each of them.
type addOnRegistrationController struct {
	clusterName          string
	agentName            string
	kubeconfigData       []byte
	managementKubeClient kubernetes.Interface // in-cluster local management kubeClient
	spokeKubeClient      kubernetes.Interface
	hubAddOnLister       addonlisterv1alpha1.ManagedClusterAddOnLister
	addOnClient          addonclient.Interface
	csrControl           clientcert.CSRControl
	recorder             events.Recorder
	csrIndexer           cache.Indexer

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
	addOnClient addonclient.Interface,
	managementKubeClient kubernetes.Interface,
	managedKubeClient kubernetes.Interface,
	csrControl clientcert.CSRControl,
	hubAddOnInformers addoninformerv1alpha1.ManagedClusterAddOnInformer,
	recorder events.Recorder,
) factory.Controller {
	c := &addOnRegistrationController{
		clusterName:              clusterName,
		agentName:                agentName,
		kubeconfigData:           kubeconfigData,
		managementKubeClient:     managementKubeClient,
		spokeKubeClient:          managedKubeClient,
		hubAddOnLister:           hubAddOnInformers.Lister(),
		csrControl:               csrControl,
		addOnClient:              addOnClient,
		recorder:                 recorder,
		csrIndexer:               csrControl.Informer().GetIndexer(),
		addOnRegistrationConfigs: map[string]map[string]registrationConfig{},
	}

	err := csrControl.Informer().AddIndexers(cache.Indexers{
		indexByAddon: indexByAddonFunc,
	})
	if err != nil {
		utilruntime.HandleError(err)
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

	// the kubeClient here will be used to generate the hub kubeconfig secret for addon agents, it generates the secret
	// on the managed cluster by default, but if the addon agent is not running on the managed cluster(in Hosted mode
	// the addon agent runs outside the managed cluster, for more details see the hosted mode design docs for addon:
	// https://github.com/open-cluster-management-io/enhancements/pull/65), it generate the secret on the
	// management(hosting) cluster
	var kubeClient kubernetes.Interface = c.spokeKubeClient
	if config.AgentRunningOutsideManagedCluster {
		kubeClient = c.managementKubeClient
	}

	kubeInformerFactory := informers.NewSharedInformerFactoryWithOptions(
		kubeClient, 10*time.Minute, informers.WithNamespace(config.InstallationNamespace))

	additonalSecretData := map[string][]byte{}
	if config.registration.SignerName == certificatesv1.KubeAPIServerClientSignerName {
		additonalSecretData[clientcert.KubeconfigFile] = c.kubeconfigData
	}

	// build and start a client cert controller
	clientCertOption := clientcert.ClientCertOption{
		SecretNamespace:               config.InstallationNamespace,
		SecretName:                    config.secretName,
		AdditionalSecretData:          additonalSecretData,
		AdditionalSecretDataSensitive: true,
	}

	csrOption := clientcert.CSROption{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("addon-%s-%s-", c.clusterName, config.addOnName),
			Labels: map[string]string{
				// the labels are only hints. Anyone could set/modify them.
				clusterv1.ClusterNameLabelKey: c.clusterName,
				addonv1alpha1.AddonLabelKey:   config.addOnName,
			},
		},
		Subject:         config.x509Subject(c.clusterName, c.agentName),
		DNSNames:        []string{fmt.Sprintf("%s.addon.open-cluster-management.io", config.addOnName)},
		SignerName:      config.registration.SignerName,
		EventFilterFunc: createCSREventFilterFunc(c.clusterName, config.addOnName, config.registration.SignerName),
		HaltCSRCreation: c.haltCSRCreationFunc(config.addOnName),
	}

	controllerName := fmt.Sprintf("ClientCertController@addon:%s:signer:%s", config.addOnName, config.registration.SignerName)

	statusUpdater := c.generateStatusUpdate(c.clusterName, config.addOnName)

	clientCertController := clientcert.NewClientCertificateController(
		clientCertOption,
		csrOption,
		c.csrControl,
		kubeInformerFactory.Core().V1().Secrets(),
		kubeClient.CoreV1(),
		statusUpdater,
		c.recorder,
		controllerName,
	)

	go kubeInformerFactory.Start(ctx.Done())
	go clientCertController.Run(ctx, 1)

	return stopFunc
}

func (c *addOnRegistrationController) haltCSRCreationFunc(addonName string) func() bool {
	return func() bool {
		items, err := c.csrIndexer.ByIndex(indexByAddon, fmt.Sprintf("%s/%s", c.clusterName, addonName))
		if err != nil {
			return false
		}

		if len(items) >= addonCSRThreshold {
			return true
		}

		return false
	}
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

	var kubeClient kubernetes.Interface = c.spokeKubeClient
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

func indexByAddonFunc(obj interface{}) ([]string, error) {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return nil, err
	}

	cluster, ok := accessor.GetLabels()[clusterv1.ClusterNameLabelKey]
	if !ok {
		return []string{}, nil
	}

	addon, ok := accessor.GetLabels()[addonv1alpha1.AddonLabelKey]
	if !ok {
		return []string{}, nil
	}

	return []string{fmt.Sprintf("%s/%s", cluster, addon)}, nil
}

func createCSREventFilterFunc(clusterName, addOnName, signerName string) factory.EventFilterFunc {
	return func(obj interface{}) bool {
		accessor, err := meta.Accessor(obj)
		if err != nil {
			return false
		}
		labels := accessor.GetLabels()
		// only enqueue csr from a specific managed cluster
		if labels[clusterv1.ClusterNameLabelKey] != clusterName {
			return false
		}
		// only enqueue csr created for a specific addon
		if labels[addonv1alpha1.AddonLabelKey] != addOnName {
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
