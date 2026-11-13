package spiffe

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/klog/v2"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/events"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"
	grpcoptions "open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/grpc"
	ocmspiffe "open-cluster-management.io/sdk-go/pkg/spiffe"

	"open-cluster-management.io/ocm/pkg/registration/register"
	registergrpc "open-cluster-management.io/ocm/pkg/registration/register/grpc"
)

// workloadAPITimeout bounds how long opening the Workload API may block.
const workloadAPITimeout = 30 * time.Second

// SPIFFEDriver registers the agent with its SVID only. It keeps no hub credential in a Secret.
type SPIFFEDriver struct {
	opt *Option

	mu     sync.Mutex
	opts   *grpcoptions.GRPCOptions
	source *ocmspiffe.X509Source
	// newOptions opens the Workload API; replaceable in tests.
	newOptions func(ctx context.Context, c grpcoptions.SPIFFEConfig) (*grpcoptions.GRPCOptions, *ocmspiffe.X509Source, error)
}

var (
	_ register.RegisterDriver     = &SPIFFEDriver{}
	_ register.SecretlessDriver   = &SPIFFEDriver{}
	_ register.AddonDriverFactory = &SPIFFEDriver{}
)

// NewSPIFFEDriver returns the spiffe driver.
func NewSPIFFEDriver(opt *Option) *SPIFFEDriver {
	return &SPIFFEDriver{opt: opt, newOptions: openWorkloadAPI}
}

func openWorkloadAPI(ctx context.Context, c grpcoptions.SPIFFEConfig) (*grpcoptions.GRPCOptions, *ocmspiffe.X509Source, error) {
	opts, closer, err := grpcoptions.NewSPIFFEGRPCOptions(ctx, c)
	if err != nil {
		return nil, nil, err
	}
	src, ok := closer.(*ocmspiffe.X509Source)
	if !ok {
		_ = closer.Close()
		return nil, nil, errors.New("unexpected Workload API source type")
	}
	return opts, src, nil
}

// Secretless implements register.SecretlessDriver: the agent skips the bootstrap phase and the
// hub-kubeconfig Secret controllers.
func (d *SPIFFEDriver) Secretless() bool { return true }

// ensure opens the Workload API once. The source lives as long as the agent: SPIRE pushes
// rotated SVIDs over it, and every new connection to the hub uses the current one.
func (d *SPIFFEDriver) ensure(ctx context.Context) (*grpcoptions.GRPCOptions, *ocmspiffe.X509Source, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.opts != nil {
		return d.opts, d.source, nil
	}
	ctx, cancel := context.WithTimeout(ctx, workloadAPITimeout)
	defer cancel()
	opts, src, err := d.newOptions(ctx, grpcoptions.SPIFFEConfig{
		URL:               d.opt.HubEndpoint,
		HubSPIFFEID:       d.opt.HubSPIFFEID,
		AgentSPIFFEID:     d.opt.AgentSPIFFEID,
		WorkloadAPISocket: d.opt.WorkloadAPISocket,
	})
	if err != nil {
		return nil, nil, err
	}
	d.opts, d.source = opts, src
	return opts, src, nil
}

// IsHubKubeConfigValid reports whether the Workload API has the agent's SVID. The reason it
// hasn't, for example the IDs it received instead, is logged.
func (d *SPIFFEDriver) IsHubKubeConfigValid(ctx context.Context, _ register.SecretOption) (bool, error) {
	_, src, err := d.ensure(ctx)
	if err == nil {
		err = src.Ready()
	}
	if err != nil {
		klog.FromContext(ctx).Info("the agent's SVID is not available", "spiffeID", d.opt.AgentSPIFFEID, "reason", err.Error())
		return false, nil
	}
	return true, nil
}

// BuildClients builds the hub clients over CloudEvents on the SPIFFE listener.
func (d *SPIFFEDriver) BuildClients(ctx context.Context, secretOption register.SecretOption, bootstrap bool) (*register.Clients, error) {
	opts, _, err := d.ensure(ctx)
	if err != nil {
		return nil, err
	}
	return registergrpc.NewCloudEventsClients(ctx, opts, secretOption.ClusterName, bootstrap)
}

// Process has nothing to do: there is no credential to request or store.
func (d *SPIFFEDriver) Process(_ context.Context, _ string, secret *corev1.Secret, _ map[string][]byte,
	_ events.Recorder) (*corev1.Secret, *metav1.Condition, error) {
	return secret, nil, nil
}

// BuildKubeConfigFromTemplate returns the template unchanged; no kubeconfig is written.
func (d *SPIFFEDriver) BuildKubeConfigFromTemplate(template *clientcmdapi.Config) *clientcmdapi.Config {
	return template
}

// InformerHandler has no informer: there is no CSR to watch.
func (d *SPIFFEDriver) InformerHandler() (cache.SharedIndexInformer, factory.EventFilterFunc) {
	return nil, nil
}

// ManagedClusterDecorator leaves the cluster unchanged. The hub sets the trust domain pin.
func (d *SPIFFEDriver) ManagedClusterDecorator(cluster *clusterv1.ManagedCluster) *clusterv1.ManagedCluster {
	return cluster
}

// Close closes the Workload API source.
func (d *SPIFFEDriver) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.source == nil {
		return nil
	}
	return d.source.Close()
}

// Fork is never used for add-on credentials: with the spiffe add-on driver each add-on agent has
// its own SVID, and the add-on registration controller creates nothing. Implementing
// AddonDriverFactory makes the agent run that controller, which reports the add-ons' status.
func (d *SPIFFEDriver) Fork(addonName string, _ register.AddonAuthConfig, _ register.SecretOption) (register.RegisterDriver, error) {
	return nil, fmt.Errorf("add-on %s: the spiffe driver creates no add-on credentials", addonName)
}
