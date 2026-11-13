package grpc

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/spf13/pflag"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"google.golang.org/grpc"

	operatorv1 "open-cluster-management.io/api/operator/v1"
	grpcauthz "open-cluster-management.io/sdk-go/pkg/cloudevents/server/grpc/authz/kube"
	sdkgrpc "open-cluster-management.io/sdk-go/pkg/server/grpc"
	grpcauthn "open-cluster-management.io/sdk-go/pkg/server/grpc/authn"
	spiffeserver "open-cluster-management.io/sdk-go/pkg/server/grpc/spiffe"
	ocmspiffe "open-cluster-management.io/sdk-go/pkg/spiffe"
)

// DefaultSPIFFEListenAddress is the address of the SPIFFE listener. The grpc listener uses 8090
// and the health endpoint 8443.
const DefaultSPIFFEListenAddress = ":8091"

// SPIFFEListenerOptions configure the listener of the spiffe registration driver: SPIFFE mTLS
// with the server's SVID from the local Workload API, and agents authenticated only by their
// SPIRE-issued SVIDs. Nothing here is secret.
type SPIFFEListenerOptions struct {
	Enabled              bool
	ListenAddress        string
	TrustDomain          string
	AgentTrustDomains    []string
	ServerSPIFFEID       string
	WorkloadAPISocket    string
	AgentIDTemplate      string
	AddOnAgentIDTemplate string
}

func newSPIFFEListenerOptions() SPIFFEListenerOptions {
	return SPIFFEListenerOptions{
		ListenAddress:        DefaultSPIFFEListenAddress,
		WorkloadAPISocket:    "unix:///spiffe-workload-api/spire-agent.sock",
		AgentIDTemplate:      operatorv1.DefaultSPIFFEAgentIDTemplate,
		AddOnAgentIDTemplate: operatorv1.DefaultSPIFFEAddOnAgentIDTemplate,
	}
}

// AddFlags adds the SPIFFE listener flags.
func (o *SPIFFEListenerOptions) AddFlags(fs *pflag.FlagSet) {
	fs.BoolVar(&o.Enabled, "spiffe-enabled", o.Enabled,
		"Run the SPIFFE listener, which authenticates agents only by their SPIRE-issued X.509-SVIDs.")
	fs.StringVar(&o.ListenAddress, "spiffe-listen-address", o.ListenAddress, "Address of the SPIFFE listener.")
	fs.StringVar(&o.TrustDomain, "spiffe-trust-domain", o.TrustDomain,
		"The hub's SPIFFE trust domain. The server's SVID is in it.")
	fs.StringSliceVar(&o.AgentTrustDomains, "spiffe-agent-trust-domains", o.AgentTrustDomains,
		"Trust domains agents are accepted from, comma separated. The hub's trust domain is always accepted.")
	fs.StringVar(&o.ServerSPIFFEID, "spiffe-server-id", o.ServerSPIFFEID,
		"SPIFFE ID the server presents. Default spiffe://<trust domain>/ocm/hub/grpc-server.")
	fs.StringVar(&o.WorkloadAPISocket, "spiffe-workload-api-socket", o.WorkloadAPISocket,
		"Address of the local SPIRE agent's Workload API.")
	fs.StringVar(&o.AgentIDTemplate, "spiffe-agent-id-template", o.AgentIDTemplate,
		"SPIFFE ID layout of Klusterlet agents.")
	fs.StringVar(&o.AddOnAgentIDTemplate, "spiffe-addon-agent-id-template", o.AddOnAgentIDTemplate,
		"SPIFFE ID layout of add-on agents.")
}

// spiffeListenerConfig is the checked form of SPIFFEListenerOptions.
type spiffeListenerConfig struct {
	port     string
	serverID spiffeid.ID
	scheme   *grpcauthn.IDScheme
}

// complete checks the options and fills the defaults. It fails on anything the listener could
// not start with, before any connection to the Workload API.
func (o *SPIFFEListenerOptions) complete() (*spiffeListenerConfig, error) {
	td, err := spiffeid.TrustDomainFromString(o.TrustDomain)
	if err != nil {
		return nil, fmt.Errorf("--spiffe-trust-domain %q: %w", o.TrustDomain, err)
	}
	serverID := o.ServerSPIFFEID
	if serverID == "" {
		serverID = "spiffe://" + td.Name() + "/ocm/hub/grpc-server"
	}
	sid, err := spiffeid.FromString(serverID)
	if err != nil {
		return nil, fmt.Errorf("--spiffe-server-id %q: %w", serverID, err)
	}
	if !sid.MemberOf(td) {
		return nil, fmt.Errorf("--spiffe-server-id %q is not in trust domain %q", serverID, td.Name())
	}
	trustDomains := (&operatorv1.SPIFFERegistrationConfig{
		TrustDomain:       td.Name(),
		AgentTrustDomains: o.AgentTrustDomains,
	}).GetAgentTrustDomains()
	scheme, err := grpcauthn.NewIDScheme(o.AgentIDTemplate, o.AddOnAgentIDTemplate, trustDomains)
	if err != nil {
		return nil, err
	}
	if err := scheme.CheckServerID(sid); err != nil {
		return nil, err
	}
	_, port, err := net.SplitHostPort(o.ListenAddress)
	if err != nil {
		return nil, fmt.Errorf("--spiffe-listen-address %q: %w", o.ListenAddress, err)
	}
	return &spiffeListenerConfig{port: port, serverID: sid, scheme: scheme}, nil
}

// runSPIFFEListener serves the CloudEvents services on the SPIFFE listener until ctx ends. The
// SPIFFE authenticator is the only authenticator here: tokens and OCM-signed certificates are
// never accepted on this port.
func (o *GRPCServerOptions) runSPIFFEListener(ctx context.Context, cfg *spiffeListenerConfig,
	base *sdkgrpc.GRPCServerOptions, authorizer *grpcauthz.SARAuthorizer, register func(*grpc.Server)) error {
	src, err := ocmspiffe.NewX509Source(ctx, o.SPIFFE.WorkloadAPISocket, cfg.serverID)
	if err != nil {
		return fmt.Errorf("SPIFFE listener: %w", err)
	}
	defer func() { _ = src.Close() }()

	// Same limits and keepalive as the grpc listener; the transport credentials replace its TLS.
	opts := *base
	opts.ServerBindPort = cfg.port
	return sdkgrpc.NewGRPCServer(&opts).
		WithTransportCredentials(spiffeserver.NewServerCredentials(src, cfg.scheme, o.registry)).
		WithAuthenticator(grpcauthn.NewSPIFFEAuthenticator(cfg.scheme)).
		WithUnaryAuthorizer(authorizer).
		WithStreamAuthorizer(authorizer).
		WithInterceptors(
			[]grpc.UnaryServerInterceptor{grpcauthn.SVIDExpiryUnaryInterceptor(time.Now)},
			[]grpc.StreamServerInterceptor{grpcauthn.SVIDExpiryStreamInterceptor(time.Now)}).
		WithRegisterFunc(register).
		Run(ctx)
}
