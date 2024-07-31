package register

import (
	"context"
	"fmt"
	"os"
	"reflect"

	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/klog/v2"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
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

		if secretOption.BootStrapKubeConfig != nil {
			if valid, err := IsHubKubeconfigValid(secretOption.BootStrapKubeConfig, hubKubeconfig); !valid || err != nil {
				return valid, err
			}
		}

		return driver.IsHubKubeConfigValid(ctx, secretOption)
	}
}

// AggregatedApprover is a list of approver that hub controller will run at the same time
type AggregatedApprover struct {
	approvers []Approver
}

func NewAggregatedApprover(approvers ...Approver) Approver {
	return &AggregatedApprover{
		approvers: approvers,
	}
}

func (a *AggregatedApprover) Run(ctx context.Context, workers int) {
	for _, approver := range a.approvers {
		go approver.Run(ctx, workers)
	}

	<-ctx.Done()
}

func (a *AggregatedApprover) Cleanup(ctx context.Context, cluster *clusterv1.ManagedCluster) error {
	var errs []error
	for _, approver := range a.approvers {
		if err := approver.Cleanup(ctx, cluster); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.NewAggregate(errs)
}

// NoopApprover is an approver with no operation, for testing
type NoopApprover struct{}

func NewNoopApprover() Approver {
	return &NoopApprover{}
}

func (a *NoopApprover) Run(ctx context.Context, _ int) {
	<-ctx.Done()
}

func (a *NoopApprover) Cleanup(_ context.Context, _ *clusterv1.ManagedCluster) error {
	return nil
}
