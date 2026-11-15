package spiffe

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/sdk-go/pkg/testing/fakespire"

	"open-cluster-management.io/ocm/pkg/registration/register"
)

const (
	hubID          = "spiffe://ocm-hub.example.org/ocm/hub/grpc-server"
	registrationID = "spiffe://ocm-hub.example.org/ocm/cluster/cluster1/agent/registration"
	workID         = "spiffe://ocm-hub.example.org/ocm/cluster/cluster1/agent/work"
)

func TestOptionValidate(t *testing.T) {
	valid := func() *Option {
		o := NewOption()
		o.HubEndpoint, o.HubSPIFFEID, o.AgentSPIFFEID = "hub:443", hubID, registrationID
		return o
	}
	require.NoError(t, valid().Validate())
	require.Equal(t, DefaultWorkloadAPISocket, NewOption().WorkloadAPISocket)

	cases := []struct {
		name    string
		mutate  func(o *Option)
		wantErr string
	}{
		{"no endpoint", func(o *Option) { o.HubEndpoint = "" }, "--spiffe-hub-endpoint, --spiffe-hub-id and --spiffe-agent-id are required"},
		{"no hub ID", func(o *Option) { o.HubSPIFFEID = "" }, "are required"},
		{"no agent ID", func(o *Option) { o.AgentSPIFFEID = "" }, "are required"},
		{"bad hub ID", func(o *Option) { o.HubSPIFFEID = "https://hub" }, "--spiffe-hub-id"},
		{"bad agent ID", func(o *Option) { o.AgentSPIFFEID = "cluster1" }, "--spiffe-agent-id"},
		{"no socket", func(o *Option) { o.WorkloadAPISocket = "" }, "--spiffe-workload-api-socket"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			o := valid()
			c.mutate(o)
			require.ErrorContains(t, o.Validate(), c.wantErr)
		})
	}
}

// newDriver returns a driver for registrationID whose Workload API serves ids.
func newDriver(t *testing.T, ids ...string) *SPIFFEDriver {
	t.Helper()
	ca := fakespire.NewCA(t, "ocm-hub.example.org")
	api := fakespire.Start(t, ca)
	for _, id := range ids {
		api.SetSVIDs(ca.Issue(t, id, time.Hour))
	}
	o := NewOption()
	o.HubEndpoint, o.HubSPIFFEID, o.AgentSPIFFEID, o.WorkloadAPISocket = "127.0.0.1:1", hubID, registrationID, api.Addr
	d := NewSPIFFEDriver(o)
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func TestIsHubKubeConfigValid(t *testing.T) {
	ctx := context.Background()

	ok, err := newDriver(t, registrationID).IsHubKubeConfigValid(ctx, register.SecretOption{})
	require.NoError(t, err)
	require.True(t, ok, "the agent's SVID is there")

	ok, err = newDriver(t, workID).IsHubKubeConfigValid(ctx, register.SecretOption{})
	require.NoError(t, err)
	require.False(t, ok, "only another agent's SVID is there")
}

func TestBuildClientsWithoutSVID(t *testing.T) {
	_, err := newDriver(t, workID).BuildClients(context.Background(), register.SecretOption{ClusterName: "cluster1"}, false)
	require.ErrorContains(t, err, "no SVID with ID "+registrationID)
	require.ErrorContains(t, err, workID, "the error lists the IDs received")
}

func TestBuildClients(t *testing.T) {
	d := newDriver(t, registrationID)
	clients, err := d.BuildClients(context.Background(), register.SecretOption{ClusterName: "cluster1"}, false)
	require.NoError(t, err)
	require.NotNil(t, clients.ClusterClient)
	require.NotNil(t, clients.LeaseClient)
	require.NotNil(t, clients.AddonClient)
	require.NotNil(t, clients.EventsClient)

	// The Workload API source is opened once and shared.
	first := d.source
	_, err = d.BuildClients(context.Background(), register.SecretOption{ClusterName: "cluster1"}, true)
	require.NoError(t, err)
	require.Same(t, first, d.source)
}

func TestSecretless(t *testing.T) {
	d := NewSPIFFEDriver(NewOption())
	require.True(t, register.IsSecretless(d))

	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "hub-kubeconfig-secret"}}
	got, cond, err := d.Process(context.Background(), "test", secret, map[string][]byte{}, nil)
	require.NoError(t, err)
	require.Nil(t, cond)
	require.Same(t, secret, got, "nothing is written")

	informer, filter := d.InformerHandler()
	require.Nil(t, informer)
	require.Nil(t, filter)

	tmpl := &clientcmdapi.Config{}
	require.Same(t, tmpl, d.BuildKubeConfigFromTemplate(tmpl))
	c := &clusterv1.ManagedCluster{}
	require.Same(t, c, d.ManagedClusterDecorator(c))
}

// The spiffe driver never creates add-on credentials: each add-on agent has its own SVID.
func TestSPIFFEDriverForkRefuses(t *testing.T) {
	d := NewSPIFFEDriver(NewOption())
	if _, err := d.Fork("my-addon", nil, register.SecretOption{}); err == nil || !strings.Contains(err.Error(), "my-addon") {
		t.Errorf("Fork: %v", err)
	}
}
