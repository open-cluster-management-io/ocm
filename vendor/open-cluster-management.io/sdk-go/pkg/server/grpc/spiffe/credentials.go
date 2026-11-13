package spiffe

import (
	"crypto/x509"
	"fmt"
	"net"
	"slices"

	"github.com/spiffe/go-spiffe/v2/bundle/x509bundle"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/spiffetls/tlsconfig"
	"github.com/spiffe/go-spiffe/v2/svid/x509svid"
	"google.golang.org/grpc/credentials"

	ocmauthn "open-cluster-management.io/sdk-go/pkg/server/grpc/authn"
)

// Source is what the server credentials need from the Workload API: the server's SVID and the
// trust bundles. *spiffe.X509Source implements it.
type Source interface {
	x509svid.Source
	x509bundle.Source
}

// NewServerCredentials returns gRPC transport credentials that do SPIFFE mTLS with the server's
// SVID from src, accept client SVIDs only from the scheme's agent trust domains, map each client
// with the scheme, and track the connection in reg so it is closed at SVID expiry. A client
// whose ID the scheme refuses is dropped during the handshake.
func NewServerCredentials(src Source, scheme *ocmauthn.IDScheme, reg *Registry) credentials.TransportCredentials {
	tlsCfg := tlsconfig.MTLSServerConfig(src, src, authorizeMemberOfAny(scheme.TrustDomains()))
	return &trackingCreds{TransportCredentials: credentials.NewTLS(tlsCfg), scheme: scheme, reg: reg}
}

// authorizeMemberOfAny accepts a peer whose SPIFFE ID is in any of tds. The hub's SPIRE may
// federate with more trust domains than the hub accepts agents from.
func authorizeMemberOfAny(tds []spiffeid.TrustDomain) tlsconfig.Authorizer {
	return func(id spiffeid.ID, _ [][]*x509.Certificate) error {
		if slices.Contains(tds, id.TrustDomain()) {
			return nil
		}
		return fmt.Errorf("trust domain %q is not an accepted agent trust domain", id.TrustDomain().Name())
	}
}

type trackingCreds struct {
	credentials.TransportCredentials
	scheme *ocmauthn.IDScheme
	reg    *Registry
}

// ServerHandshake runs the TLS handshake, maps the client's SVID and registers the connection.
// The returned AuthInfo is an authn.SPIFFEAuthInfo, which SPIFFEAuthenticator requires.
func (c *trackingCreds) ServerHandshake(raw net.Conn) (net.Conn, credentials.AuthInfo, error) {
	conn, info, err := c.TransportCredentials.ServerHandshake(raw)
	if err != nil {
		return nil, nil, err
	}
	tlsInfo, ok := info.(credentials.TLSInfo)
	if !ok || len(tlsInfo.State.PeerCertificates) == 0 {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("no client certificate")
	}
	leaf := tlsInfo.State.PeerCertificates[0]
	sid, err := x509svid.IDFromCert(leaf)
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	id, err := c.scheme.Parse(sid)
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	authInfo := ocmauthn.SPIFFEAuthInfo{TLSInfo: tlsInfo, SPIFFEID: sid, NotAfter: leaf.NotAfter}
	return c.reg.track(conn, id, sid, leaf.SerialNumber, leaf.NotAfter), authInfo, nil
}

func (c *trackingCreds) Clone() credentials.TransportCredentials {
	return &trackingCreds{TransportCredentials: c.TransportCredentials.Clone(), scheme: c.scheme, reg: c.reg}
}
