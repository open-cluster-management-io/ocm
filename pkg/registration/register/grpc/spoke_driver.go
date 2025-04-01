package grpc

import (
	"context"
	"fmt"
	"os"
	"path"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	certificates "k8s.io/api/certificates/v1"
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
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic"

	"open-cluster-management.io/ocm/pkg/registration/register"
	"open-cluster-management.io/ocm/pkg/registration/register/csr"
)

type GRPCDriver struct {
	csrDriver *csr.CSRDriver
	control   *csrControl
	opt       *Option
}

var _ register.RegisterDriver = &GRPCDriver{}
var _ register.AddonDriver = &GRPCDriver{}

func NewGRPCDriver(opt *Option, csrOption *csr.Option, secretOption register.SecretOption) register.RegisterDriver {
	return &GRPCDriver{
		csrDriver: csr.NewCSRDriver(csrOption, secretOption),
		opt:       opt,
	}
}

func (d *GRPCDriver) BuildClients(ctx context.Context, secretOption register.SecretOption, bootstrapped bool) (*register.Clients, error) {
	// For cloudevents drivers, we build hub client based on different driver configuration.
	var config any
	var err error
	if bootstrapped {
		_, config, err = generic.NewConfigLoader("grpc", d.opt.BootstrapConfigFile).
			LoadConfig()
		if err != nil {
			return nil, fmt.Errorf(
				"failed to load hub bootstrap registration config from file %q: %w",
				d.opt.BootstrapConfigFile, err)
		}
	} else {
		_, config, err = generic.NewConfigLoader("grpc", d.opt.ConfigFile).
			LoadConfig()
		if err != nil {
			return nil, fmt.Errorf(
				"failed to load hub registration config from file %q: %w",
				d.opt.ConfigFile, err)
		}
	}

	clusterClientHolder, err := cloudeventscluster.NewClientHolder(
		ctx,
		cloudeventoptions.NewGenericClientOptions[*clusterv1.ManagedCluster](
			config,
			cloudeventscluster.NewManagedClusterCodec(),
			secretOption.ClusterName,
		))
	if err != nil {
		return nil, err
	}

	leaseClient, err := cloudeventslease.NewLeaseClient(
		ctx,
		cloudeventoptions.NewGenericClientOptions[*coordv1.Lease](
			config,
			cloudeventslease.NewManagedClusterAddOnCodec(),
			secretOption.ClusterName,
		),
		secretOption.ClusterName,
	)

	eventClient, err := cloudeventsevent.NewClientHolder(
		ctx,
		cloudeventoptions.NewGenericClientOptions[*eventsv1.Event](
			config,
			cloudeventsevent.NewManagedClusterAddOnCodec(),
			secretOption.ClusterName,
		),
	)

	addonClientHolder, err := cloudeventsaddon.NewClientHolder(
		ctx,
		cloudeventoptions.NewGenericClientOptions[*addonapiv1alpha1.ManagedClusterAddOn](
			config,
			cloudeventsaddon.NewManagedClusterAddOnCodec(),
			secretOption.ClusterName,
		))

	csrClientHolder, err := cloudeventscsr.NewAgentClientHolder(ctx,
		cloudeventoptions.NewGenericClientOptions[*certificates.CertificateSigningRequest](
			config,
			cloudeventscsr.NewCSRCodec(),
			secretOption.ClusterName,
		),
	)
	if err != nil {
		return nil, err
	}
	certControl := &csrControl{csrClientHolder: csrClientHolder}
	err = d.csrDriver.SetCSRControl(certControl, secretOption.ClusterName)
	if err != nil {
		return nil, err
	}

	clients := &register.Clients{
		ClusterClient: clusterClientHolder.ClusterInterface(),
		ClusterInformer: clusterinformers.NewSharedInformerFactory(
			clusterClientHolder.ClusterInterface(), 10*time.Minute).Cluster().V1().ManagedClusters(),
		AddonClient: addonClientHolder.ClusterInterface(),
		AddonInformer: addoninformers.NewSharedInformerFactory(
			addonClientHolder.ClusterInterface(), 10*time.Minute).Addon().V1alpha1().ManagedClusterAddOns(),
		LeaseClient:  leaseClient,
		EventsClient: eventClient,
	}
	return clients, nil
}

func (c *GRPCDriver) Fork(addonName string, secretOption register.SecretOption) register.RegisterDriver {
	csrDriver := c.csrDriver.Fork(addonName, secretOption)
	return &GRPCDriver{
		control:   c.control,
		opt:       c.opt,
		csrDriver: csrDriver.(*csr.CSRDriver),
	}
}

func (c *GRPCDriver) Process(
	ctx context.Context, controllerName string, secret *corev1.Secret, additionalSecretData map[string][]byte,
	recorder events.Recorder) (*corev1.Secret, *metav1.Condition, error) {
	return c.csrDriver.Process(ctx, controllerName, secret, additionalSecretData, recorder)
}

func (c *GRPCDriver) BuildKubeConfigFromTemplate(_ *clientcmdapi.Config) *clientcmdapi.Config {
	return nil
}

func (c *GRPCDriver) InformerHandler() (cache.SharedIndexInformer, factory.EventFilterFunc) {
	return c.control.Informer(), nil
}

func (c *GRPCDriver) IsHubKubeConfigValid(ctx context.Context, secretOption register.SecretOption) (bool, error) {
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

func (c *GRPCDriver) ManagedClusterDecorator(cluster *clusterv1.ManagedCluster) *clusterv1.ManagedCluster {
	return cluster
}
