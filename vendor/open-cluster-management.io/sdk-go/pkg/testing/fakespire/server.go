package fakespire

import (
	"crypto/x509"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/spiffe/go-spiffe/v2/proto/spiffe/workload"
	"github.com/spiffe/go-spiffe/v2/svid/x509svid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Server is a fake SPIRE agent Workload API. Set SVIDs with SetSVIDs; every open
// FetchX509SVID stream receives the update, as after a rotation by a real agent.
type Server struct {
	workload.UnimplementedSpiffeWorkloadAPIServer

	// Addr is the socket address, for workloadapi.WithAddr, for example
	// "unix:///tmp/fakespire123/agent.sock".
	Addr string

	ca        *CA
	mu        sync.Mutex
	svids     []*x509svid.SVID
	federated []*CA
	notify    chan struct{}
	gs        *grpc.Server
}

// Start serves a Workload API for ca's trust domain until the test ends.
func Start(t testing.TB, ca *CA) *Server {
	t.Helper()
	// A short directory: unix socket paths are limited to about 100 bytes, and t.TempDir() names
	// grow with the test name.
	dir, err := os.MkdirTemp("", "fakespire")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "agent.sock")
	lis, err := net.Listen("unix", sock)
	require.NoError(t, err)

	s := &Server{Addr: "unix://" + sock, ca: ca, notify: make(chan struct{}), gs: grpc.NewServer()}
	workload.RegisterSpiffeWorkloadAPIServer(s.gs, s)
	go func() { _ = s.gs.Serve(lis) }()
	t.Cleanup(s.Stop)
	return s
}

// Stop stops the server and ends every open stream, like a SPIRE agent going away.
func (s *Server) Stop() { s.gs.Stop() }

// SetSVIDs replaces the SVIDs and pushes them to every open stream. With no SVIDs, new
// streams are refused with PermissionDenied, as a real agent does when no entry matches.
func (s *Server) SetSVIDs(svids ...*x509svid.SVID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.svids = svids
	s.broadcast()
}

// Federate adds the bundles of other trust domains to every response, like a SPIRE agent whose
// server federates with them.
func (s *Server) Federate(cas ...*CA) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.federated = append(s.federated, cas...)
	s.broadcast()
}

// broadcast wakes every stream. s.mu must be held.
func (s *Server) broadcast() {
	close(s.notify)
	s.notify = make(chan struct{})
}

// FetchX509SVID implements the Workload API.
func (s *Server) FetchX509SVID(_ *workload.X509SVIDRequest, stream workload.SpiffeWorkloadAPI_FetchX509SVIDServer) error {
	if md, _ := metadata.FromIncomingContext(stream.Context()); len(md.Get("workload.spiffe.io")) == 0 {
		return status.Error(codes.InvalidArgument, "security header missing from request")
	}
	for {
		s.mu.Lock()
		resp, notify := s.response(), s.notify
		s.mu.Unlock()
		if len(resp.Svids) == 0 {
			return status.Error(codes.PermissionDenied, "no identity issued")
		}
		if err := stream.Send(resp); err != nil {
			return err
		}
		select {
		case <-notify:
		case <-stream.Context().Done():
			return nil
		}
	}
}

// response builds the current X509SVIDResponse. s.mu must be held.
func (s *Server) response() *workload.X509SVIDResponse {
	resp := &workload.X509SVIDResponse{}
	bundle := concatDER(s.ca.Bundle().X509Authorities())
	for _, sv := range s.svids {
		keyDER, err := x509.MarshalPKCS8PrivateKey(sv.PrivateKey)
		if err != nil {
			panic(err) // test keys are always ECDSA
		}
		resp.Svids = append(resp.Svids, &workload.X509SVID{
			SpiffeId:    sv.ID.String(),
			X509Svid:    concatDER(sv.Certificates),
			X509SvidKey: keyDER,
			Bundle:      bundle,
		})
	}
	if len(s.federated) > 0 {
		resp.FederatedBundles = map[string][]byte{}
		for _, ca := range s.federated {
			resp.FederatedBundles[ca.TrustDomain.IDString()] = concatDER(ca.Bundle().X509Authorities())
		}
	}
	return resp
}

func concatDER(certs []*x509.Certificate) []byte {
	var out []byte
	for _, c := range certs {
		out = append(out, c.Raw...)
	}
	return out
}
