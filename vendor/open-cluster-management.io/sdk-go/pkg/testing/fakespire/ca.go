// Package fakespire is an in-process stand-in for SPIRE in tests: a trust domain CA that issues
// X.509-SVIDs, and a Workload API server that streams them over a unix socket. It is test-only;
// no production code may import it.
package fakespire

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"math/big"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spiffe/go-spiffe/v2/bundle/x509bundle"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/svid/x509svid"
	"github.com/stretchr/testify/require"
)

var serial atomic.Int64

// CA is an in-memory SPIFFE trust domain CA for tests.
type CA struct {
	TrustDomain spiffeid.TrustDomain
	cert        *x509.Certificate
	key         crypto.Signer
}

// NewCA creates a CA for trust domain td, valid from an hour ago for a day.
func NewCA(t testing.TB, td string) *CA {
	t.Helper()
	tdID, err := spiffeid.TrustDomainFromString(td)
	require.NoError(t, err)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(serial.Add(1)),
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		URIs:                  []*url.URL{tdID.ID().URL()},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return &CA{TrustDomain: tdID, cert: cert, key: key}
}

// Issue creates an X.509-SVID for id, valid from a second ago for ttl. id may be in any trust
// domain, so a test can also build an SVID that the CA has no right to sign.
func (c *CA) Issue(t testing.TB, id string, ttl time.Duration) *x509svid.SVID {
	t.Helper()
	sid, err := spiffeid.FromString(id)
	require.NoError(t, err)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(serial.Add(1)),
		NotBefore:    time.Now().Add(-time.Second),
		NotAfter:     time.Now().Add(ttl),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		URIs:         []*url.URL{sid.URL()},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, c.cert, &key.PublicKey, c.key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return &x509svid.SVID{ID: sid, Certificates: []*x509.Certificate{cert}, PrivateKey: key}
}

// Bundle is the trust bundle for this CA.
func (c *CA) Bundle() *x509bundle.Bundle {
	return x509bundle.FromX509Authorities(c.TrustDomain, []*x509.Certificate{c.cert})
}
