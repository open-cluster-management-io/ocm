package grpc

import (
	"context"
	"fmt"
	"os"
	"path"
	"time"

	"gopkg.in/yaml.v2"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/grpc"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	coordv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/klog/v2"

	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	cloudeventsaddon "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/addon"
	cloudeventscluster "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/cluster"
	cloudeventscsr "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/csr"
	cloudeventsevent "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/event"
	cloudeventslease "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/lease"
	cloudeventoptions "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/options"
	cestore "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/store"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic"

	"open-cluster-management.io/ocm/pkg/registration/register"
	"open-cluster-management.io/ocm/pkg/registration/register/csr"
)

type GRPCDriver struct {
	csrDriver      *csr.CSRDriver
	control        *csrControl
	opt            *Option
	configTemplate []byte
}

var _ register.RegisterDriver = &GRPCDriver{}
var _ register.AddonDriver = &GRPCDriver{}

func NewGRPCDriver(opt *Option, csrOption *csr.Option, secretOption register.SecretOption) (register.RegisterDriver, error) {
	secretOption.Signer = signer
	csrDrvier, err := csr.NewCSRDriver(csrOption, secretOption)
	if err != nil {
		return nil, err
	}
	return &GRPCDriver{
		csrDriver: csrDrvier,
		opt:       opt,
	}, nil
}

func (d *GRPCDriver) BuildClients(ctx context.Context, secretOption register.SecretOption, bootstrapped bool) (*register.Clients, error) {
	// For cloudevents drivers, we build hub client based on different driver configuration.
	var config any
	var err error
	var configFile string
	if bootstrapped {
		_, config, err = generic.NewConfigLoader("grpc", d.opt.BootstrapConfigFile).LoadConfig()
		if err != nil {
			return nil, fmt.Errorf(
				"failed to load hub bootstrap registration config from file %q: %w",
				d.opt.BootstrapConfigFile, err)
		}

		configFile = d.opt.BootstrapConfigFile
	} else {
		_, config, err = generic.NewConfigLoader("grpc", d.opt.ConfigFile).LoadConfig()
		if err != nil {
			return nil, fmt.Errorf(
				"failed to load hub registration config from file %q: %w",
				d.opt.ConfigFile, err)
		}

		configFile = d.opt.ConfigFile
	}

	configData, err := os.ReadFile(configFile)
	if err != nil {
		return nil, err
	}

	grpcConfig := &grpc.GRPCConfig{}
	if err := yaml.Unmarshal(configData, grpcConfig); err != nil {
		return nil, err
	}

	configData, err = yaml.Marshal(&grpc.GRPCConfig{
		URL:             grpcConfig.URL,
		CAFile:          grpcConfig.CAFile,
		KeepAliveConfig: grpcConfig.KeepAliveConfig,
		ClientKeyFile:   path.Join(secretOption.HubKubeconfigDir, csr.TLSKeyFile),
		ClientCertFile:  path.Join(secretOption.HubKubeconfigDir, csr.TLSCertFile),
	})
	if err != nil {
		return nil, err
	}

	d.configTemplate = configData

	clusterWatchStore := cestore.NewAgentInformerWatcherStore[*clusterv1.ManagedCluster]()
	clusterClientHolder, err := cloudeventscluster.NewClientHolder(
		ctx,
		cloudeventoptions.NewGenericClientOptions(
			config,
			cloudeventscluster.NewManagedClusterCodec(),
			secretOption.ClusterName,
		).
			WithClusterName(secretOption.ClusterName).
			WithClientWatcherStore(clusterWatchStore))
	if err != nil {
		return nil, err
	}
	clusterClient := clusterClientHolder.ClusterInterface()
	clusterInformers := clusterinformers.NewSharedInformerFactory(
		clusterClient, 10*time.Minute).Cluster().V1().ManagedClusters()
	clusterWatchStore.SetInformer(clusterInformers.Informer())

	leaseWatchStore := cestore.NewSimpleStore[*coordv1.Lease]()
	leaseClient, err := cloudeventslease.NewLeaseClient(
		ctx,
		cloudeventoptions.NewGenericClientOptions(
			config,
			cloudeventslease.NewManagedClusterAddOnCodec(),
			secretOption.ClusterName,
		).WithClusterName(secretOption.ClusterName).WithClientWatcherStore(leaseWatchStore),
		secretOption.ClusterName,
	)
	if err != nil {
		return nil, err
	}

	eventWatchStore := cestore.NewSimpleStore[*eventsv1.Event]()
	eventClient, err := cloudeventsevent.NewClientHolder(
		ctx,
		cloudeventoptions.NewGenericClientOptions(
			config,
			cloudeventsevent.NewEventCodec(),
			secretOption.ClusterName,
		).WithClusterName(secretOption.ClusterName).WithClientWatcherStore(eventWatchStore),
	)
	if err != nil {
		return nil, err
	}

	addonWatchStore := cestore.NewAgentInformerWatcherStore[*addonapiv1alpha1.ManagedClusterAddOn]()
	addonClientHolder, err := cloudeventsaddon.NewClientHolder(
		ctx,
		cloudeventoptions.NewGenericClientOptions(
			config,
			cloudeventsaddon.NewManagedClusterAddOnCodec(),
			secretOption.ClusterName,
		).WithClusterName(secretOption.ClusterName).WithClientWatcherStore(addonWatchStore))
	if err != nil {
		return nil, err
	}
	addonClient := addonClientHolder.ClusterInterface()
	addonInformer := addoninformers.NewSharedInformerFactory(
		addonClient, 10*time.Minute).Addon().V1alpha1().ManagedClusterAddOns()
	addonWatchStore.SetInformer(addonInformer.Informer())

	csrClientHolder, err := cloudeventscsr.NewAgentClientHolder(ctx,
		cloudeventoptions.NewGenericClientOptions(
			config,
			cloudeventscsr.NewCSRCodec(),
			secretOption.ClusterName,
		).WithClusterName(secretOption.ClusterName),
	)
	if err != nil {
		return nil, err
	}

	certControl := &csrControl{csrClientHolder: csrClientHolder}
	if err = d.csrDriver.SetCSRControl(certControl, secretOption.ClusterName); err != nil {
		return nil, err
	}

	d.control = certControl

	clients := &register.Clients{
		ClusterClient:   clusterClient,
		ClusterInformer: clusterInformers,
		AddonClient:     addonClient,
		AddonInformer:   addonInformer,
		LeaseClient:     leaseClient,
		EventsClient:    eventClient,
	}
	return clients, nil
}

func (d *GRPCDriver) Fork(addonName string, secretOption register.SecretOption) register.RegisterDriver {
	csrDriver := d.csrDriver.Fork(addonName, secretOption)
	return &GRPCDriver{
		control:   d.control,
		opt:       d.opt,
		csrDriver: csrDriver.(*csr.CSRDriver),
	}
}

func (d *GRPCDriver) Process(
	ctx context.Context, controllerName string, secret *corev1.Secret, additionalSecretData map[string][]byte,
	recorder events.Recorder) (*corev1.Secret, *metav1.Condition, error) {
	additionalSecretData["config.yaml"] = d.configTemplate
	return d.csrDriver.Process(ctx, controllerName, secret, additionalSecretData, recorder)
}

func (d *GRPCDriver) BuildKubeConfigFromTemplate(_ *clientcmdapi.Config) *clientcmdapi.Config {
	return nil
}

func (d *GRPCDriver) InformerHandler() (cache.SharedIndexInformer, factory.EventFilterFunc) {
	return d.control.Informer(), nil
}

func (d *GRPCDriver) IsHubKubeConfigValid(ctx context.Context, secretOption register.SecretOption) (bool, error) {
	logger := klog.FromContext(ctx)
	keyPath := path.Join(secretOption.HubKubeconfigDir, csr.TLSKeyFile)
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		logger.V(4).Info("TLS key file not found", "keyPath", keyPath)
		return false, nil
	}

	certPath := path.Join(secretOption.HubKubeconfigDir, csr.TLSCertFile)
	certData, err := os.ReadFile(path.Clean(certPath))
	if err != nil {
		logger.V(4).Info("Unable to load TLS cert file", "certPath", certPath)
		return false, nil
	}

	// only set when clustername/agentname are set
	if len(secretOption.ClusterName) > 0 && len(secretOption.AgentName) > 0 {
		// check if the tls certificate is issued for the current cluster/agent
		clusterNameInCert, agentNameInCert, err := csr.GetClusterAgentNamesFromCertificate(certData)
		if err != nil {
			return false, nil
		}
		if secretOption.ClusterName != clusterNameInCert || secretOption.AgentName != agentNameInCert {
			logger.V(4).Info("Certificate in file is issued for different agent",
				"certPath", certPath,
				"issuedFor", fmt.Sprintf("%s:%s", secretOption.ClusterName, secretOption.AgentName),
				"expectedFor", fmt.Sprintf("%s:%s", secretOption.ClusterName, secretOption.AgentName))

			return false, nil
		}
	}
	return csr.IsCertificateValid(logger, certData, nil)
}

func (d *GRPCDriver) ManagedClusterDecorator(cluster *clusterv1.ManagedCluster) *clusterv1.ManagedCluster {
	return cluster
}
