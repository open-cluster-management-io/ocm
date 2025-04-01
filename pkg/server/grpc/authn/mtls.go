package authn

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

type MtlsAuthenticator struct {
}

func NewMtlsAuthenticator() *MtlsAuthenticator {
	return &MtlsAuthenticator{}
}

func (a *MtlsAuthenticator) Authenticate(ctx context.Context) (context.Context, error) {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return ctx, status.Error(codes.Unauthenticated, "no peer found")
	}

	tlsAuth, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok {
		return ctx, status.Error(codes.Unauthenticated, "unexpected peer transport credentials")
	}

	if len(tlsAuth.State.VerifiedChains) == 0 || len(tlsAuth.State.VerifiedChains[0]) == 0 {
		return ctx, status.Error(codes.Unauthenticated, "could not verify peer certificate")
	}

	if tlsAuth.State.VerifiedChains[0][0] == nil {
		return ctx, status.Error(codes.Unauthenticated, "could not verify peer certificate")
	}

	user := tlsAuth.State.VerifiedChains[0][0].Subject.CommonName
	groups := tlsAuth.State.VerifiedChains[0][0].Subject.Organization
	newCtx := newContextWithIdentity(ctx, user, groups)
	return newCtx, nil
}
