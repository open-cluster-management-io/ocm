package grpc

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/spf13/pflag"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/spiffetls/tlsconfig"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
	certutil "k8s.io/client-go/util/cert"

	operatorv1 "open-cluster-management.io/api/operator/v1"
	ocmspiffe "open-cluster-management.io/sdk-go/pkg/spiffe"
	"open-cluster-management.io/sdk-go/pkg/testing/fakespire"
)

const (
	testTD     = "ocm-hub.example.org"
	testHubID  = "spiffe://ocm-hub.example.org/ocm/hub/grpc-server"
	testWorkID = "spiffe://ocm-hub.example.org/ocm/cluster/cluster1/agent/work"
)

func TestSPIFFEListenerFlags(t *testing.T) {
	opts := NewGRPCServerOptions()
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	opts.AddFlags(fs)
	require.NoError(t, fs.Parse([]string{
		"--grpc-enabled=false",
		"--spiffe-enabled",
		"--spiffe-trust-domain=" + testTD,
		"--spiffe-agent-trust-domains=east.example.org,west.example.org",
	}))
	require.False(t, opts.GRPCEnabled)
	require.True(t, opts.SPIFFE.Enabled)
	require.Equal(t, DefaultSPIFFEListenAddress, opts.SPIFFE.ListenAddress)
	require.Equal(t, "unix:///spiffe-workload-api/spire-agent.sock", opts.SPIFFE.WorkloadAPISocket)
	require.Equal(t, operatorv1.DefaultSPIFFEAgentIDTemplate, opts.SPIFFE.AgentIDTemplate)
	require.Equal(t, operatorv1.DefaultSPIFFEAddOnAgentIDTemplate, opts.SPIFFE.AddOnAgentIDTemplate)

	cfg, err := opts.SPIFFE.complete()
	require.NoError(t, err)
	require.Equal(t, testHubID, cfg.serverID.String())
	require.Equal(t, "8091", cfg.port)
	var tds []string
	for _, td := range cfg.scheme.TrustDomains() {
		tds = append(tds, td.Name())
	}
	require.Equal(t, []string{testTD, "east.example.org", "west.example.org"}, tds)

	defaults := NewGRPCServerOptions()
	require.True(t, defaults.GRPCEnabled)
	require.False(t, defaults.SPIFFE.Enabled)
}

func TestSPIFFEListenerComplete(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(o *SPIFFEListenerOptions)
		wantErr string
	}{
		{"bad trust domain", func(o *SPIFFEListenerOptions) { o.TrustDomain = "OCM_HUB" }, "--spiffe-trust-domain"},
		{"no trust domain", func(o *SPIFFEListenerOptions) { o.TrustDomain = "" }, "--spiffe-trust-domain"},
		{"server ID in another trust domain", func(o *SPIFFEListenerOptions) {
			o.ServerSPIFFEID = "spiffe://east.example.org/ocm/hub/grpc-server"
		}, "is not in trust domain"},
		{"server ID not a SPIFFE ID", func(o *SPIFFEListenerOptions) { o.ServerSPIFFEID = "https://hub" }, "--spiffe-server-id"},
		{"server ID matches the agent template", func(o *SPIFFEListenerOptions) {
			o.ServerSPIFFEID = "spiffe://ocm-hub.example.org/ocm/cluster/hub/agent/work"
		}, "agents could present it"},
		{"bad agent template", func(o *SPIFFEListenerOptions) {
			o.AgentIDTemplate = "spiffe://{trustDomain}/{cluster}/{role}"
		}, "needs at least one fixed path segment"},
		{"bad agent trust domain", func(o *SPIFFEListenerOptions) { o.AgentTrustDomains = []string{"East_Example"} }, "East_Example"},
		{"bad listen address", func(o *SPIFFEListenerOptions) { o.ListenAddress = "8091" }, "--spiffe-listen-address"},
		{"a listen address with a host", func(o *SPIFFEListenerOptions) { o.ListenAddress = "127.0.0.1:8091" }, "give only the port"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			o := newSPIFFEListenerOptions()
			o.Enabled = true
			o.TrustDomain = testTD
			c.mutate(&o)
			_, err := o.complete()
			require.ErrorContains(t, err, c.wantErr)
		})
	}
}

func TestRunWithNoListener(t *testing.T) {
	opts := NewGRPCServerOptions()
	opts.GRPCEnabled = false
	err := opts.Run(context.Background(), &controllercmd.ControllerContext{KubeConfig: &rest.Config{Host: "https://example.com"}})
	require.ErrorContains(t, err, "no listener enabled")
}

// freePort returns a port nothing listens on right now.
func freePort(t *testing.T) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer lis.Close()
	return fmt.Sprintf("%d", lis.Addr().(*net.TCPAddr).Port)
}

// grpcConfig writes a grpc listener config with a self-signed serving certificate.
func grpcConfig(t *testing.T, port string) string {
	t.Helper()
	dir := t.TempDir()
	cert, key, err := certutil.GenerateSelfSignedCertKey("localhost", []net.IP{net.ParseIP("127.0.0.1")}, nil)
	require.NoError(t, err)
	certFile, keyFile := filepath.Join(dir, "tls.crt"), filepath.Join(dir, "tls.key")
	require.NoError(t, os.WriteFile(certFile, cert, 0600))
	require.NoError(t, os.WriteFile(keyFile, key, 0600))
	config := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(config, []byte(fmt.Sprintf(
		"server_bind_port: %q\ntls_cert_file: %s\ntls_key_file: %s\nclient_ca_file: \"\"\n", port, certFile, keyFile)), 0600))
	return config
}

type hubUnderTest struct {
	ca         *fakespire.CA
	hubAPI     *fakespire.Server
	grpcPort   string
	spiffePort string
	opts       *GRPCServerOptions
	done       chan error
	cancel     context.CancelFunc
}

func startHubServer(t *testing.T, grpcEnabled bool, serverIDs ...string) *hubUnderTest {
	t.Helper()
	ca := fakespire.NewCA(t, testTD)
	hubAPI := fakespire.Start(t, ca)
	if len(serverIDs) == 0 {
		serverIDs = []string{testHubID}
	}
	for _, id := range serverIDs {
		hubAPI.SetSVIDs(ca.Issue(t, id, time.Hour))
	}
	h := &hubUnderTest{ca: ca, hubAPI: hubAPI, grpcPort: freePort(t), spiffePort: freePort(t), done: make(chan error, 1)}
	h.opts = NewGRPCServerOptions()
	h.opts.GRPCServerConfig = grpcConfig(t, h.grpcPort)
	h.opts.GRPCEnabled = grpcEnabled
	h.opts.SPIFFE.Enabled = true
	h.opts.SPIFFE.TrustDomain = testTD
	h.opts.SPIFFE.ListenAddress = ":" + h.spiffePort
	h.opts.SPIFFE.WorkloadAPISocket = hubAPI.Addr

	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	t.Cleanup(cancel)
	go func() {
		h.done <- h.opts.Run(ctx, &controllercmd.ControllerContext{KubeConfig: &rest.Config{Host: "https://127.0.0.1:1"}})
	}()
	return h
}

// spiffeDial completes a SPIFFE mTLS handshake with the listener as agent id.
func (h *hubUnderTest) spiffeDial(t *testing.T, id string) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	api := fakespire.Start(t, h.ca)
	api.SetSVIDs(h.ca.Issue(t, id, time.Hour))
	src, err := ocmspiffe.NewX509Source(ctx, api.Addr, spiffeid.RequireFromString(id))
	require.NoError(t, err)
	defer src.Close()
	cfg := tlsconfig.MTLSClientConfig(src, src, tlsconfig.AuthorizeID(spiffeid.RequireFromString(testHubID)))
	cfg.NextProtos = []string{"h2"}
	conn, err := tls.Dial("tcp", "127.0.0.1:"+h.spiffePort, cfg)
	if err != nil {
		return err
	}
	defer conn.Close()
	return firstRead(conn)
}

// firstRead tells an accepted connection from a refused one. A gRPC server sends its HTTP/2
// SETTINGS frame as soon as it accepts a connection; a refused client gets a TLS alert (TLS 1.3
// reports a refused client certificate on the first read) or EOF (the SPIFFE credentials drop a
// client whose ID isn't an agent ID after the handshake).
func firstRead(conn net.Conn) error {
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Read(make([]byte, 1)); err != nil {
		return fmt.Errorf("connection refused by the server: %w", err)
	}
	return nil
}

// plainTLSDial completes a TLS handshake without a client certificate, as a token client does.
func plainTLSDial(port string) error {
	conn, err := tls.Dial("tcp", "127.0.0.1:"+port, &tls.Config{InsecureSkipVerify: true, NextProtos: []string{"h2"}}) //nolint:gosec
	if err != nil {
		return err
	}
	defer conn.Close()
	return firstRead(conn)
}

func eventually(t *testing.T, f func() error) {
	t.Helper()
	var err error
	require.Eventually(t, func() bool { err = f(); return err == nil }, 10*time.Second, 50*time.Millisecond, "last error: %v", err)
}

func TestSPIFFEOnly(t *testing.T) {
	h := startHubServer(t, false)
	eventually(t, func() error { return h.spiffeDial(t, testWorkID) })

	// A token client has no SVID: refused at TLS on the SPIFFE port.
	require.Error(t, plainTLSDial(h.spiffePort))
	// The grpc listener is off.
	_, err := net.DialTimeout("tcp", "127.0.0.1:"+h.grpcPort, time.Second)
	require.Error(t, err)

	h.cancel()
	select {
	case err := <-h.done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return")
	}
}

func TestSPIFFEAndGRPCTogether(t *testing.T) {
	h := startHubServer(t, true)
	eventually(t, func() error { return h.spiffeDial(t, testWorkID) })
	eventually(t, func() error { return plainTLSDial(h.grpcPort) })
	require.Error(t, plainTLSDial(h.spiffePort))
	// A client SVID that isn't an agent ID is refused on the SPIFFE port.
	require.Error(t, h.spiffeDial(t, "spiffe://ocm-hub.example.org/ns/default/sa/default"))
}

func TestSPIFFEListenerNoServerSVID(t *testing.T) {
	h := startHubServer(t, false, "spiffe://ocm-hub.example.org/ns/open-cluster-management-hub/sa/default")
	select {
	case err := <-h.done:
		var notFound *ocmspiffe.ErrSVIDNotFound
		require.True(t, errors.As(err, &notFound), "got %v", err)
		require.Equal(t, testHubID, notFound.Want.String())
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not fail")
	}
}

// Invalid SPIFFE settings stop the server before it connects to anything.
func TestRunWithInvalidSPIFFESettings(t *testing.T) {
	opts := NewGRPCServerOptions()
	opts.GRPCEnabled = false
	opts.SPIFFE.Enabled = true
	opts.SPIFFE.TrustDomain = "Not A Trust Domain"
	err := opts.Run(context.Background(), &controllercmd.ControllerContext{KubeConfig: &rest.Config{Host: "https://example.com"}})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "no listener enabled")
}
