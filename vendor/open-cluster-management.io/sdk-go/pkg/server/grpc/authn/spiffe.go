package authn

import (
	"context"
	"time"

	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

const (
	// ContextSPIFFEIDKey holds the peer's SPIFFE ID (spiffeid.ID).
	ContextSPIFFEIDKey contextKey = "spiffe-id"
	// ContextSVIDExpiryKey holds the peer's SVID NotAfter (time.Time).
	ContextSVIDExpiryKey contextKey = "svid-expiry"
	// ContextOCMIdentityKey holds the peer's OCMIdentity.
	ContextOCMIdentityKey contextKey = "ocm-identity"
)

// SPIFFEAuthInfo is the gRPC AuthInfo of a connection whose client X.509-SVID the SPIFFE server
// credentials verified against the SPIRE trust bundles and mapped to an OCM identity. go-spiffe
// verifies in its own callback, so the TLS state has no VerifiedChains; this type is the proof
// that the verification happened.
type SPIFFEAuthInfo struct {
	credentials.TLSInfo
	// SPIFFEID is the client's SPIFFE ID.
	SPIFFEID spiffeid.ID
	// NotAfter is when the client's SVID expires.
	NotAfter time.Time
}

// AuthType implements credentials.AuthInfo.
func (SPIFFEAuthInfo) AuthType() string { return "spiffe" }

// SPIFFEAuthenticator authenticates a peer by the X.509-SVID it presented in the TLS handshake.
// It accepts only connections made through the SPIFFE server credentials (SPIFFEAuthInfo), and
// maps the SVID's SPIFFE ID to an OCM identity with the IDScheme. Use it as the only
// authenticator of the SPIFFE listener, never next to the token or mTLS authenticators.
type SPIFFEAuthenticator struct {
	scheme *IDScheme
	now    func() time.Time
}

var _ Authenticator = &SPIFFEAuthenticator{}

// NewSPIFFEAuthenticator returns an authenticator that maps peer SVIDs with scheme.
func NewSPIFFEAuthenticator(scheme *IDScheme) *SPIFFEAuthenticator {
	return &SPIFFEAuthenticator{scheme: scheme, now: time.Now}
}

// Authenticate puts the user and groups, the SPIFFE ID, the SVID expiry and the OCMIdentity of
// the peer into the context.
func (a *SPIFFEAuthenticator) Authenticate(ctx context.Context) (context.Context, error) {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return ctx, status.Error(codes.Unauthenticated, "no peer found")
	}
	info, ok := p.AuthInfo.(SPIFFEAuthInfo)
	if !ok {
		return ctx, status.Error(codes.Unauthenticated, "the connection was not verified by the SPIFFE server credentials")
	}
	id := info.SPIFFEID
	if !a.now().Before(info.NotAfter) {
		return ctx, status.Errorf(codes.Unauthenticated, "SVID for %s expired at %s", id, info.NotAfter)
	}
	identity, err := a.scheme.Parse(id)
	if err != nil {
		return ctx, status.Error(codes.Unauthenticated, err.Error())
	}
	ctx = newContextWithIdentity(ctx, identity.User, identity.Groups)
	ctx = context.WithValue(ctx, ContextSPIFFEIDKey, id)
	ctx = context.WithValue(ctx, ContextSVIDExpiryKey, info.NotAfter)
	return context.WithValue(ctx, ContextOCMIdentityKey, identity), nil
}

// OCMIdentityFromContext returns the agent identity set by SPIFFEAuthenticator.
func OCMIdentityFromContext(ctx context.Context) (OCMIdentity, bool) {
	id, ok := ctx.Value(ContextOCMIdentityKey).(OCMIdentity)
	return id, ok
}

// SVIDExpiryUnaryInterceptor refuses a unary RPC whose peer SVID has expired. It must run after
// the authn interceptor, which puts the expiry into the context.
func SVIDExpiryUnaryInterceptor(now func() time.Time) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
		if err := checkExpiry(ctx, now); err != nil {
			return nil, err
		}
		return h(ctx, req)
	}
}

// SVIDExpiryStreamInterceptor refuses a stream whose peer SVID has expired when it opens. A
// stream that is already open is ended by closing its connection at expiry.
func SVIDExpiryStreamInterceptor(now func() time.Time) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, h grpc.StreamHandler) error {
		if err := checkExpiry(ss.Context(), now); err != nil {
			return err
		}
		return h(srv, ss)
	}
}

func checkExpiry(ctx context.Context, now func() time.Time) error {
	exp, ok := ctx.Value(ContextSVIDExpiryKey).(time.Time)
	if !ok {
		return status.Error(codes.Unauthenticated, "no SVID expiry in context")
	}
	if !now().Before(exp) {
		return status.Errorf(codes.Unauthenticated, "SVID expired at %s", exp)
	}
	return nil
}
