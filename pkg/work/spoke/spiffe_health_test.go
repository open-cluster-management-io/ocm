package spoke

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	workv1 "open-cluster-management.io/api/work/v1"
	cloudeventsoptions "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/options"
	cloudeventswork "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/work"
	"open-cluster-management.io/sdk-go/pkg/testing/fakespire"

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

const (
	testHubID  = "spiffe://ocm-hub.example.org/ocm/hub/grpc-server"
	testWorkID = "spiffe://ocm-hub.example.org/ocm/cluster/c1/agent/work"
)

func spiffeWorkConfig(socket string) *WorkAgentConfig {
	opts := NewWorkloadAgentOptions()
	opts.SPIFFE.HubEndpoint = "127.0.0.1:1"
	opts.SPIFFE.HubSPIFFEID = testHubID
	opts.SPIFFE.AgentSPIFFEID = testWorkID
	opts.SPIFFE.WorkloadAPISocket = socket
	agentOpts := commonoptions.NewAgentOptions()
	agentOpts.SpokeClusterName = "c1"
	return NewWorkAgentConfig(agentOpts, opts)
}

// Without spiffe, the SVID check always passes.
func TestSPIFFEHealthCheckDisabled(t *testing.T) {
	config := NewWorkAgentConfig(commonoptions.NewAgentOptions(), NewWorkloadAgentOptions())
	for _, c := range config.HealthCheckers() {
		if err := c.Check(nil); err != nil {
			t.Errorf("%s: %v without spiffe", c.Name(), err)
		}
	}
}

// Once started, the check follows the Workload API: ready with the agent's SVID, not ready when
// SPIRE stops issuing it.
func TestSPIFFEWorkClientAndHealth(t *testing.T) {
	ca := fakespire.NewCA(t, "ocm-hub.example.org")
	spire := fakespire.Start(t, ca)
	spire.SetSVIDs(ca.Issue(t, testWorkID, time.Hour))
	config := spiffeWorkConfig(spire.Addr)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	host, works, informer, err := config.newWorkClientAndInformer(ctx)
	if err != nil || host != "127.0.0.1:1" || works == nil || informer == nil {
		t.Fatalf("host %q, works %v, informer %v, err %v", host, works, informer, err)
	}
	check := config.HealthCheckers()[0]
	if err := check.Check(nil); err != nil {
		t.Errorf("not ready with the agent's SVID: %v", err)
	}
	spire.SetSVIDs(ca.Issue(t, "spiffe://ocm-hub.example.org/ocm/cluster/c1/agent/registration", time.Hour))
	deadline := time.Now().Add(5 * time.Second)
	for check.Check(nil) == nil && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if err := check.Check(nil); err == nil {
		t.Error("still ready after the Workload API stopped issuing the agent's SVID")
	}
}

func TestSPIFFEWorkClientErrors(t *testing.T) {
	// A hub ID that is no SPIFFE ID.
	config := spiffeWorkConfig("unix:///nonexistent.sock")
	config.workOptions.SPIFFE.HubSPIFFEID = "https://hub"
	if _, _, _, err := config.newWorkClientAndInformer(context.Background()); err == nil {
		t.Error("no error for an invalid hub ID")
	}

	// The hub client can't be built.
	ca := fakespire.NewCA(t, "ocm-hub.example.org")
	spire := fakespire.Start(t, ca)
	spire.SetSVIDs(ca.Issue(t, testWorkID, time.Hour))
	defer func() { newWorkAgentClientHolder = cloudeventswork.NewAgentClientHolder }()
	newWorkAgentClientHolder = func(context.Context, *cloudeventsoptions.GenericClientOptions[*workv1.ManifestWork]) (
		*cloudeventswork.ClientHolder, error) {
		return nil, errors.New("client failed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, _, _, err := spiffeWorkConfig(spire.Addr).newWorkClientAndInformer(ctx); err == nil ||
		!strings.Contains(err.Error(), "client failed") {
		t.Errorf("error %v, want the client's", err)
	}
}
