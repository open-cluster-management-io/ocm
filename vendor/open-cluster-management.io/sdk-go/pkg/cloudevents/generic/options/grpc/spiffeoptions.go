package grpc

import (
	"context"
	"fmt"
	"time"

	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/spiffetls/tlsconfig"

	ocmspiffe "open-cluster-management.io/sdk-go/pkg/spiffe"
)

// SPIFFEConfig is what an agent needs to dial the hub with its SPIRE-issued SVID. Nothing in it
// is secret: the SVID and its key come from the local Workload API and stay in memory.
type SPIFFEConfig struct {
	// URL is host:port of the hub's grpc-spiffe listener.
	URL string
	// HubSPIFFEID is the only SPIFFE ID accepted from the hub.
	HubSPIFFEID string
	// AgentSPIFFEID is the agent's own SPIFFE ID; the SVID with this ID is presented.
	AgentSPIFFEID string
	// WorkloadAPISocket is the local SPIRE agent's Workload API address.
	WorkloadAPISocket string
}

// NewSPIFFEGRPCOptions builds client options whose TLS config comes from the Workload API. It
// blocks until the agent's SVID arrives, and fails with *spiffe.ErrSVIDNotFound if the Workload
// API has no SVID with AgentSPIFFEID. The returned source is the agent's SVID: close it when the
// agent stops, and use its Ready method to report whether the SVID is still there.
//
// The TLS config reads the SVID and the trust bundles on every handshake, so a reconnect after
// rotation presents the new SVID without further code.
func NewSPIFFEGRPCOptions(ctx context.Context, c SPIFFEConfig) (*GRPCOptions, *ocmspiffe.X509Source, error) {
	if c.URL == "" {
		return nil, nil, fmt.Errorf("the hub URL is required")
	}
	hubID, err := spiffeid.FromString(c.HubSPIFFEID)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid hub SPIFFE ID %q: %w", c.HubSPIFFEID, err)
	}
	agentID, err := spiffeid.FromString(c.AgentSPIFFEID)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid agent SPIFFE ID %q: %w", c.AgentSPIFFEID, err)
	}
	src, err := ocmspiffe.NewX509Source(ctx, c.WorkloadAPISocket, agentID)
	if err != nil {
		return nil, nil, err
	}
	opts := &GRPCOptions{
		Dialer: &GRPCDialer{
			URL: c.URL,
			KeepAliveOptions: KeepAliveOptions{
				Time:    30 * time.Second,
				Timeout: 10 * time.Second,
			},
			TLSConfig: tlsconfig.MTLSClientConfig(src, src, tlsconfig.AuthorizeID(hubID)),
		},
	}
	return opts, src, nil
}
