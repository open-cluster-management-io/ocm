package spiffe

import (
	"strings"
	"testing"
)

func TestAgentTrustDomain(t *testing.T) {
	o := NewOption()
	o.HubSPIFFEID = "spiffe://ocm-hub.example.org/ocm/hub/grpc-server"
	if td, err := o.AgentTrustDomain(); err != nil || td != "ocm-hub.example.org" {
		t.Errorf("default: %q, %v; want the hub's trust domain", td, err)
	}
	o.TrustDomain = "east.example.org"
	if td, err := o.AgentTrustDomain(); err != nil || td != "east.example.org" {
		t.Errorf("set: %q, %v", td, err)
	}
	o.TrustDomain, o.HubSPIFFEID = "", "https://hub"
	if _, err := o.AgentTrustDomain(); err == nil || !strings.Contains(err.Error(), "--spiffe-hub-id") {
		t.Errorf("invalid hub ID: %v", err)
	}
}
