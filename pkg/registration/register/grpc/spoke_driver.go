package grpc

import (
	"context"
	"fmt"
	"os"
	"path"
	"time"

	"gopkg.in/yaml.v2"
	certificatesv1 "k8s.io/api/certificates/v1"
	coordv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/klog/v2"

	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	operatorv1 "open-cluster-management.io/api/operator/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/events"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"
	cloudeventsaddon "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/addon"
	cloudeventsaddonv1alpha1 "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/addon/v1alpha1"
	cloudeventscluster "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/cluster"
	cloudeventscsr "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/csr"
	cloudeventsevent "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/event"
	cloudeventslease "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/lease"
	cloudeventsoptions "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/options"
	cloudeventsstore "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/store"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/constants"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/builder"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/cert"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/grpc"

	"open-cluster-management.io/ocm/pkg/registration/register"
	"open-cluster-management.io/ocm/pkg/registration/register/csr"
)

type GRPCDriver struct {
	csrDriver      *csr.CSRDriver
	control        *ceCSRControl
	opt            *Option
	configTemplate []byte
}

var _ register.RegisterDriver = &GRPCDriver{}
var _ register.AddonDriver = &GRPCDriver{}

func NewGRPCDriver(opt *Option, csrOption *csr.Option, secretOption register.SecretOption) (register.RegisterDriver, error) {
	secretOption.Signer = operatorv1.GRPCAuthSigner
	csrDriver, err := csr.NewCSRDriver(csrOption, secretOption)
	if err != nil {
		return nil, err
	}
	return &GRPCDriver{
		csrDriver: csrDriver,
		opt:       opt,
	}, nil
}

func (d *GRPCDriver) BuildClients(ctx context.Context, secretOption register.SecretOption, bootstrapped bool) (*register.Clients, error) {
	config, configData, err := d.loadConfig(secretOption, bootstrapped)
	if err != nil {
		return nil, err
	}
	d.configTemplate = configData

	clusterWatchStore := cloudeventsstore.NewAgentInformerWatcherStore[*clusterv1.ManagedCluster]()
	clusterClientHolder, err := cloudeventscluster.NewClientHolder(
		ctx,
		cloudeventsoptions.NewGenericClientOptions(
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

	csrClientHolder, err := cloudeventscsr.NewAgentClientHolder(ctx,
		cloudeventsoptions.NewGenericClientOptions(
			config,
			cloudeventscsr.NewCSRCodec(),
			secretOption.ClusterName,
		).WithClusterName(secretOption.ClusterName),
	)
	if err != nil {
		return nil, err
	}
	csrControl := &ceCSRControl{csrClientHolder: csrClientHolder}
	if err := d.csrDriver.SetCSRControl(csrControl, secretOption.ClusterName); err != nil {
		return nil, err
	}
	d.control = csrControl

	// Initialize the cluster client and CSR control in the bootstrap phase.
	// Other clients should not be initialized, since they require
	// permissions that are not allowed in the bootstrap phase.
	if bootstrapped {
		return &register.Clients{
			ClusterClient:   clusterClient,
			ClusterInformer: clusterInformers,
		}, nil
	}

	leaseWatchStore := cloudeventsstore.NewSimpleStore[*coordv1.Lease]()
	leaseClient, err := cloudeventslease.NewLeaseClient(
		ctx,
		cloudeventsoptions.NewGenericClientOptions(
			config,
			cloudeventslease.NewLeaseCodec(),
			secretOption.ClusterName,
		).WithClusterName(secretOption.ClusterName).WithClientWatcherStore(leaseWatchStore),
		secretOption.ClusterName,
	)
	if err != nil {
		return nil, err
	}

	eventClient, err := cloudeventsevent.NewClientHolder(
		ctx,
		cloudeventsoptions.NewGenericClientOptions(
			config,
			cloudeventsevent.NewEventCodec(),
			secretOption.ClusterName,
		).WithClusterName(secretOption.ClusterName).WithSubscription(false).WithResyncEnabled(false),
	)
	if err != nil {
		return nil, err
	}

	addonWatchStore := cloudeventsstore.NewAgentInformerWatcherStore[*addonapiv1alpha1.ManagedClusterAddOn]()
	addonClient, err := cloudeventsaddon.ManagedClusterAddOnInterface(
		ctx,
		cloudeventsoptions.NewGenericClientOptions(
			config,
			cloudeventsaddonv1alpha1.NewManagedClusterAddOnCodec(),
			secretOption.ClusterName,
		).WithClusterName(secretOption.ClusterName).WithClientWatcherStore(addonWatchStore))
	if err != nil {
		return nil, err
	}
	addonInformer := addoninformers.NewSharedInformerFactoryWithOptions(
		addonClient, 10*time.Minute, addoninformers.WithNamespace(secretOption.ClusterName)).
		Addon().V1alpha1().ManagedClusterAddOns()

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

func (d *GRPCDriver) BuildKubeConfigFromTemplate(kubeConfig *clientcmdapi.Config) *clientcmdapi.Config {
	kubeConfig.AuthInfos = map[string]*clientcmdapi.AuthInfo{register.DefaultKubeConfigAuth: {
		ClientCertificate: "tls.crt",
		ClientKey:         "tls.key",
	}}
	return kubeConfig
}

func (d *GRPCDriver) InformerHandler() (cache.SharedIndexInformer, factory.EventFilterFunc) {
	return d.control.Informer(), nil
}

func (d *GRPCDriver) IsHubKubeConfigValid(ctx context.Context, secretOption register.SecretOption) (bool, error) {
	logger := klog.FromContext(ctx)

	certPath := path.Join(secretOption.HubKubeconfigDir, csr.TLSCertFile)
	certData, err := os.ReadFile(path.Clean(certPath))
	if err != nil {
		logger.V(4).Info("Unable to load TLS cert file", "certPath", certPath)
		return false, nil
	}

	keyPath := path.Join(secretOption.HubKubeconfigDir, csr.TLSKeyFile)
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		logger.V(4).Info("TLS key file not found", "keyPath", keyPath)
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
				"issuedFor", fmt.Sprintf("%s:%s", clusterNameInCert, agentNameInCert),
				"expectedFor", fmt.Sprintf("%s:%s", secretOption.ClusterName, secretOption.AgentName))

			return false, nil
		}
	}
	return csr.IsCertificateValid(logger, certData, nil)
}

func (d *GRPCDriver) ManagedClusterDecorator(cluster *clusterv1.ManagedCluster) *clusterv1.ManagedCluster {
	return cluster
}

func (d *GRPCDriver) loadConfig(secretOption register.SecretOption, bootstrapped bool) (any, []byte, error) {
	var err error
	var config any
	var configFile string
	if bootstrapped {
		_, config, err = builder.NewConfigLoader(constants.ConfigTypeGRPC, d.opt.BootstrapConfigFile).LoadConfig()
		if err != nil {
			return nil, nil, fmt.Errorf(
				"failed to load hub bootstrap registration config from file %q: %w",
				d.opt.BootstrapConfigFile, err)
		}

		configFile = d.opt.BootstrapConfigFile
	} else {
		_, config, err = builder.NewConfigLoader(constants.ConfigTypeGRPC, d.opt.ConfigFile).LoadConfig()
		if err != nil {
			return nil, nil, fmt.Errorf(
				"failed to load hub registration config from file %q: %w",
				d.opt.ConfigFile, err)
		}

		configFile = d.opt.ConfigFile
	}

	grpcConfig, err := grpc.LoadConfig(configFile)
	if err != nil {
		return nil, nil, err
	}

	configData, err := yaml.Marshal(&grpc.GRPCConfig{
		CertConfig: cert.CertConfig{
			CAData:         grpcConfig.CAData,
			ClientKeyFile:  path.Join(secretOption.HubKubeconfigDir, csr.TLSKeyFile),
			ClientCertFile: path.Join(secretOption.HubKubeconfigDir, csr.TLSCertFile),
		},
		URL:             grpcConfig.URL,
		KeepAliveConfig: grpcConfig.KeepAliveConfig,
	})
	if err != nil {
		return nil, nil, err
	}

	return config, configData, nil
}

type ceCSRControl struct {
	csrClientHolder *cloudeventscsr.ClientHolder
}

var _ csr.CSRControl = &ceCSRControl{}

func (c *ceCSRControl) IsApproved(name string) (bool, error) {
	csr, err := c.csrClientHolder.Clients().Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		return false, err
	}

	for _, condition := range csr.Status.Conditions {
		if condition.Type == certificatesv1.CertificateApproved {
			return true, nil
		}
	}
	return false, nil
}

func (c *ceCSRControl) GetIssuedCertificate(name string) ([]byte, error) {
	csr, err := c.csrClientHolder.Clients().Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return csr.Status.Certificate, nil
}

func (c *ceCSRControl) Create(ctx context.Context, recorder events.Recorder, objMeta metav1.ObjectMeta, csrData []byte,
	signerName string, expirationSeconds *int32) (string, error) {
	csr := &certificatesv1.CertificateSigningRequest{
		ObjectMeta: objMeta,
		Spec: certificatesv1.CertificateSigningRequestSpec{
			Request: csrData,
			Usages: []certificatesv1.KeyUsage{
				certificatesv1.UsageDigitalSignature,
				certificatesv1.UsageKeyEncipherment,
				certificatesv1.UsageClientAuth,
			},
			SignerName:        signerName,
			ExpirationSeconds: expirationSeconds,
		},
	}

	req, err := c.csrClientHolder.Clients().Create(ctx, csr, metav1.CreateOptions{})
	if err != nil {
		return "", err
	}
	recorder.Eventf(ctx, "CSRCreated", "A csr %q is created", req.Name)
	return req.Name, nil
}

func (c *ceCSRControl) Informer() cache.SharedIndexInformer {
	return c.csrClientHolder.Informer()
}
