package spoke

import (
	"testing"

	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
)

// Before the agent starts (for example while it waits to become the leader), the SPIFFE check
// passes, or the liveness probe would restart the agent before it can start.
func TestSPIFFEHealthCheckBeforeStart(t *testing.T) {
	opts := NewWorkloadAgentOptions()
	opts.SPIFFE.HubEndpoint = "hub:443"
	opts.SPIFFE.HubSPIFFEID = "spiffe://ocm-hub.example.org/ocm/hub/grpc-server"
	opts.SPIFFE.AgentSPIFFEID = "spiffe://ocm-hub.example.org/ocm/cluster/c1/agent/work"
	config := NewWorkAgentConfig(commonoptions.NewAgentOptions(), opts)
	if !opts.SPIFFE.Enabled() {
		t.Fatal("spiffe not enabled by the options")
	}
	for _, c := range config.HealthCheckers() {
		if err := c.Check(nil); err != nil {
			t.Errorf("%s: %v before the agent started", c.Name(), err)
		}
	}
}
