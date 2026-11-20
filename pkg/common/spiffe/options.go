// Package spiffe holds the SPIFFE settings shared by the Klusterlet agents (registration, work and
// singleton): they reach the hub's SPIFFE listener with their SPIRE-issued X.509-SVID only.
package spiffe

import (
	"errors"
	"fmt"

	"github.com/spf13/pflag"
	"github.com/spiffe/go-spiffe/v2/spiffeid"

	operatorv1 "open-cluster-management.io/api/operator/v1"
)

// DefaultWorkloadAPISocket is where the SPIFFE CSI driver mounts the SPIRE agent's Workload API.
const DefaultWorkloadAPISocket = "unix:///spiffe-workload-api/spire-agent.sock"

// Option holds the agent's SPIFFE settings. Nothing here is secret.
type Option struct {
	// HubEndpoint is host:port of the hub's grpc-spiffe listener.
	HubEndpoint string
	// HubSPIFFEID is the only SPIFFE ID accepted from the hub.
	HubSPIFFEID string
	// AgentSPIFFEID is this agent's own SPIFFE ID. The Klusterlet operator renders it from the
	// Klusterlet's agentIDTemplate; the agent never builds it.
	AgentSPIFFEID string
	// WorkloadAPISocket is the local SPIRE agent's Workload API address.
	WorkloadAPISocket string
	// TrustDomain is the agents' own trust domain; default the trust domain of HubSPIFFEID. The
	// registration agent renders add-on agents' IDs in it.
	TrustDomain string
	// AddOnAgentIDTemplate is the layout of add-on agent SPIFFE IDs (the Klusterlet's
	// addOnAgentIDTemplate).
	AddOnAgentIDTemplate string
}

// NewOption returns the default settings.
func NewOption() *Option {
	return &Option{WorkloadAPISocket: DefaultWorkloadAPISocket, AddOnAgentIDTemplate: operatorv1.DefaultSPIFFEAddOnAgentIDTemplate}
}

// AddFlags adds the spiffe driver's flags.
func (o *Option) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.HubEndpoint, "spiffe-hub-endpoint", o.HubEndpoint,
		"host:port of the hub's grpc-spiffe listener.")
	fs.StringVar(&o.HubSPIFFEID, "spiffe-hub-id", o.HubSPIFFEID,
		"The only SPIFFE ID accepted from the hub.")
	fs.StringVar(&o.AgentSPIFFEID, "spiffe-agent-id", o.AgentSPIFFEID,
		"The SPIFFE ID this agent must get from the Workload API.")
	fs.StringVar(&o.WorkloadAPISocket, "spiffe-workload-api-socket", o.WorkloadAPISocket,
		"Address of the local SPIRE agent's Workload API.")
	fs.StringVar(&o.TrustDomain, "spiffe-trust-domain", o.TrustDomain,
		"The agents' own SPIFFE trust domain. Default: the trust domain of --spiffe-hub-id.")
	fs.StringVar(&o.AddOnAgentIDTemplate, "spiffe-addon-agent-id-template", o.AddOnAgentIDTemplate,
		"SPIFFE ID layout of add-on agents; must equal the hub's.")
}

// Enabled reports whether the agent connects to the hub with its SVID.
func (o *Option) Enabled() bool {
	return o != nil && o.HubEndpoint != ""
}

// Validate checks the settings of --registration-auth=spiffe.
func (o *Option) Validate() error {
	if o.HubEndpoint == "" || o.HubSPIFFEID == "" || o.AgentSPIFFEID == "" {
		return errors.New("--spiffe-hub-endpoint, --spiffe-hub-id and --spiffe-agent-id are required with --registration-auth=spiffe")
	}
	if _, err := spiffeid.FromString(o.HubSPIFFEID); err != nil {
		return fmt.Errorf("--spiffe-hub-id %q: %w", o.HubSPIFFEID, err)
	}
	if _, err := spiffeid.FromString(o.AgentSPIFFEID); err != nil {
		return fmt.Errorf("--spiffe-agent-id %q: %w", o.AgentSPIFFEID, err)
	}
	if o.WorkloadAPISocket == "" {
		return errors.New("--spiffe-workload-api-socket is required with --registration-auth=spiffe")
	}
	return nil
}

// AgentTrustDomain returns TrustDomain, or the trust domain of HubSPIFFEID.
func (o *Option) AgentTrustDomain() (string, error) {
	if o.TrustDomain != "" {
		return o.TrustDomain, nil
	}
	hubID, err := spiffeid.FromString(o.HubSPIFFEID)
	if err != nil {
		return "", fmt.Errorf("--spiffe-hub-id %q: %w", o.HubSPIFFEID, err)
	}
	return hubID.TrustDomain().Name(), nil
}
