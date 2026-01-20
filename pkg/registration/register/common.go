package register

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	leasev1client "k8s.io/client-go/kubernetes/typed/coordination/v1"
	eventsv1 "k8s.io/client-go/kubernetes/typed/events/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/klog/v2"

	addonclient "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	addonv1alpha1informers "open-cluster-management.io/api/client/addon/informers/externalversions/addon/v1alpha1"
	clusterv1client "open-cluster-management.io/api/client/cluster/clientset/versioned"
	hubclusterclientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterv1informer "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterv1listers "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/sdk-go/pkg/patcher"
)

// BaseKubeConfigFromBootStrap builds kubeconfig from bootstrap without authInfo configurations
func BaseKubeConfigFromBootStrap(bootstrapConfig *clientcmdapi.Config) (*clientcmdapi.Config, error) {
	kubeConfigCtx, cluster, err := currentKubeConfigCluster(bootstrapConfig)
	if err != nil {
		return nil, err
	}

	// Build kubeconfig.
	kubeconfig := &clientcmdapi.Config{
		// Define a cluster stanza based on the bootstrap kubeconfig.
		Clusters: map[string]*clientcmdapi.Cluster{
			kubeConfigCtx.Cluster: {
				Server:                   cluster.Server,
				InsecureSkipTLSVerify:    false,
				CertificateAuthority:     cluster.CertificateAuthority,
				CertificateAuthorityData: cluster.CertificateAuthorityData,
				ProxyURL:                 cluster.ProxyURL,
			}},
		// Define a context that connects the auth info and cluster, and set it as the default
		Contexts: map[string]*clientcmdapi.Context{DefaultKubeConfigContext: {
			Cluster:   kubeConfigCtx.Cluster,
			AuthInfo:  DefaultKubeConfigAuth,
			Namespace: "configuration",
		}},
		CurrentContext: DefaultKubeConfigContext,
	}

	return kubeconfig, nil
}

func currentKubeConfigCluster(config *clientcmdapi.Config) (*clientcmdapi.Context, *clientcmdapi.Cluster, error) {
	kubeConfigCtx, ok := config.Contexts[config.CurrentContext]
	if !ok {
		return nil, nil, fmt.Errorf("kubeconfig does not contains context: %s", config.CurrentContext)
	}

	cluster, ok := config.Clusters[kubeConfigCtx.Cluster]
	if !ok {
		return nil, nil, fmt.Errorf("kubeconfig does not contains cluster: %s", kubeConfigCtx.Cluster)
	}

	return kubeConfigCtx, cluster, nil
}

// The hub kubeconfig is valid when it shares the same value of the following with the
// bootstrap hub kubeconfig.
// 1. The hub server
// 2. The proxy url
// 3. The CA bundle
// 4. The current context cluster name
func IsHubKubeconfigValid(bootstrapKubeConfig, hubeKubeConfig *clientcmdapi.Config) (bool, error) {
	if bootstrapKubeConfig == nil {
		return false, nil
	}
	bootstrapCtx, bootstrapCluster, err := currentKubeConfigCluster(bootstrapKubeConfig)
	if err != nil {
		return false, err
	}

	if hubeKubeConfig == nil {
		return false, nil
	}
	hubKubeConfigCtx, hubKubeConfigCluster, err := currentKubeConfigCluster(hubeKubeConfig)
	switch {
	case err != nil:
		return false, err
	case bootstrapCluster.Server != hubKubeConfigCluster.Server,
		bootstrapCluster.ProxyURL != hubKubeConfigCluster.ProxyURL,
		bootstrapCluster.CertificateAuthority != hubKubeConfigCluster.CertificateAuthority,
		!reflect.DeepEqual(bootstrapCluster.CertificateAuthorityData, hubKubeConfigCluster.CertificateAuthorityData),
		// Here in addition to the server, proxyURL and CA bundle, we also need to compare the cluster name,
		// because in some cases even the hub cluster API server serving certificate(kubeconfig ca bundle)
		// is the same, but the signer certificate may be different(i.e the hub kubernetes cluster is rebuilt
		// with a same serving certificate and url), so setting the cluster name in the bootstrap kubeconfig
		// can help to distinguish the different clusters(signer certificate). And comparing the cluster name
		// can help to avoid the situation that the hub kubeconfig is valid but not for the current cluster.
		bootstrapCtx.Cluster != hubKubeConfigCtx.Cluster:
		return false, nil
	default:
		return true, nil
	}
}

func IsHubKubeConfigValidFunc(driver RegisterDriver, secretOption SecretOption) wait.ConditionWithContextFunc {
	return func(ctx context.Context) (bool, error) {
		if secretOption.BootStrapKubeConfigFile != "" {
			logger := klog.FromContext(ctx)
			if _, err := os.Stat(secretOption.HubKubeconfigFile); os.IsNotExist(err) {
				logger.V(4).Info("Kubeconfig file not found", "kubeconfigPath", secretOption.HubKubeconfigFile)
				return false, nil
			}

			// create a kubeconfig with references to the key/cert files in the same secret
			hubKubeconfig, err := clientcmd.LoadFromFile(secretOption.HubKubeconfigFile)
			if err != nil {
				return false, err
			}

			bootStrapConfig, err := clientcmd.LoadFromFile(secretOption.BootStrapKubeConfigFile)
			if err != nil {
				return false, err
			}
			if valid, err := IsHubKubeconfigValid(bootStrapConfig, hubKubeconfig); !valid || err != nil {
				return valid, err
			}
		}

		return driver.IsHubKubeConfigValid(ctx, secretOption)
	}
}

// NoopApprover is an approver with no operation, for testing
type NoopHubDriver struct{}

func NewNoopHubDriver() HubDriver {
	return &NoopHubDriver{}
}

func (n *NoopHubDriver) Accept(cluster *clusterv1.ManagedCluster) bool {
	return true
}

func (a *NoopHubDriver) CreatePermissions(_ context.Context, _ *clusterv1.ManagedCluster) error {
	return nil
}

func (a *NoopHubDriver) Run(ctx context.Context, _ int) {
	<-ctx.Done()
}

func (a *NoopHubDriver) Cleanup(_ context.Context, _ *clusterv1.ManagedCluster) error {
	return nil
}

func GenerateBootstrapStatusUpdater() StatusUpdateFunc {
	return func(ctx context.Context, cond metav1.Condition) error {
		return nil
	}
}

// GenerateStatusUpdater generates status update func after the bootstrap
func GenerateStatusUpdater(hubClusterClient hubclusterclientset.Interface,
	hubClusterLister clusterv1listers.ManagedClusterLister, clusterName string) StatusUpdateFunc {
	return func(ctx context.Context, cond metav1.Condition) error {
		cluster, err := hubClusterLister.Get(clusterName)
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		newCluster := cluster.DeepCopy()
		meta.SetStatusCondition(&newCluster.Status.Conditions, cond)
		patcher := patcher.NewPatcher[
			*clusterv1.ManagedCluster, clusterv1.ManagedClusterSpec, clusterv1.ManagedClusterStatus](
			hubClusterClient.ClusterV1().ManagedClusters())
		_, err = patcher.PatchStatus(ctx, newCluster, newCluster.Status, cluster.Status)
		return err
	}
}

// AggregatedHubDriver is a list of HubRegisterDrivers
type AggregatedHubDriver struct {
	hubRegisterDrivers []HubDriver
}

func NewAggregatedHubDriver(hubRegisterDrivers ...HubDriver) HubDriver {
	return &AggregatedHubDriver{
		hubRegisterDrivers: hubRegisterDrivers,
	}
}

func (a *AggregatedHubDriver) CreatePermissions(ctx context.Context, cluster *clusterv1.ManagedCluster) error {
	var errs []error
	for _, hubRegisterDriver := range a.hubRegisterDrivers {
		if err := hubRegisterDriver.CreatePermissions(ctx, cluster); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.NewAggregate(errs)

}

// Accept returns true only if all the HubDrivers implementations accept the cluster
func (a *AggregatedHubDriver) Accept(cluster *clusterv1.ManagedCluster) bool {
	for _, hubRegisterDriver := range a.hubRegisterDrivers {
		if !hubRegisterDriver.Accept(cluster) {
			return false
		}
	}
	return true
}

func (a *AggregatedHubDriver) Run(ctx context.Context, workers int) {
	for _, driver := range a.hubRegisterDrivers {
		go driver.Run(ctx, workers)
	}

	<-ctx.Done()
}

func (a *AggregatedHubDriver) Cleanup(ctx context.Context, cluster *clusterv1.ManagedCluster) error {
	var errs []error
	for _, driver := range a.hubRegisterDrivers {
		if err := driver.Cleanup(ctx, cluster); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.NewAggregate(errs)
}

// Clients hold all client needed to connect to hub
type Clients struct {
	ClusterClient   clusterv1client.Interface
	LeaseClient     leasev1client.LeaseInterface
	AddonClient     addonclient.Interface
	EventsClient    eventsv1.EventsV1Interface
	ClusterInformer clusterv1informer.ManagedClusterInformer
	AddonInformer   addonv1alpha1informers.ManagedClusterAddOnInformer
}

// AddOnClients hold clients and informers used by addon driver
type AddOnClients struct {
	AddonClient   addonclient.Interface
	AddonInformer addonv1alpha1informers.ManagedClusterAddOnInformer
}

func KubeConfigFromSecretOption(s SecretOption, bootstrap bool) (*rest.Config, error) {
	var kubeConfig *rest.Config
	var err error
	if bootstrap {
		if s.BootStrapKubeConfigFile == "" {
			return nil, fmt.Errorf("no bootstrap kubeconfig found")
		}

		kubeConfig, err = clientcmd.BuildConfigFromFlags("", s.BootStrapKubeConfigFile)
		if err != nil {
			return nil, fmt.Errorf("unable to load bootstrap kubeconfig: %w", err)
		}
	} else {
		if s.HubKubeconfigFile == "" {
			return nil, fmt.Errorf("no hub kubeconfig found")
		}

		kubeConfig, err = clientcmd.BuildConfigFromFlags("", s.HubKubeconfigFile)
		if err != nil {
			return nil, fmt.Errorf("unable to load hub kubeconfig from file %q: %w", s.HubKubeconfigFile, err)
		}
	}
	return kubeConfig, nil
}

func BuildClientsFromConfig(kubeConfig *rest.Config, clusterName string) (*Clients, error) {
	clients := &Clients{}
	kubeClient, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		return nil, err
	}
	clients.LeaseClient = kubeClient.CoordinationV1().Leases(clusterName)
	clients.EventsClient = kubeClient.EventsV1()
	clients.ClusterClient, err = clusterv1client.NewForConfig(kubeConfig)
	if err != nil {
		return nil, err
	}
	clients.AddonClient, err = addonclient.NewForConfig(kubeConfig)
	if err != nil {
		return nil, err
	}

	clients.ClusterInformer = clusterinformers.NewSharedInformerFactoryWithOptions(
		clients.ClusterClient,
		10*time.Minute,
		clusterinformers.WithTweakListOptions(func(listOptions *metav1.ListOptions) {
			listOptions.FieldSelector = fields.OneTermEqualSelector("metadata.name", clusterName).String()
		}),
	).Cluster().V1().ManagedClusters()
	clients.AddonInformer = addoninformers.NewSharedInformerFactoryWithOptions(
		clients.AddonClient,
		10*time.Minute,
		addoninformers.WithNamespace(clusterName),
	).Addon().V1alpha1().ManagedClusterAddOns()
	return clients, nil
}

func BuildClientsFromSecretOption(s SecretOption, bootstrap bool) (*Clients, error) {
	kubeConfig, err := KubeConfigFromSecretOption(s, bootstrap)
	if err != nil {
		return nil, err
	}
	return BuildClientsFromConfig(kubeConfig, s.ClusterName)
}
