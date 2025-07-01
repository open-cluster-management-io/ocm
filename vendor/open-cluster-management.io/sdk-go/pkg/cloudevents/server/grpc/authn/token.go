package authn

import (
	"context"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type TokenAuthenticator struct {
	client kubernetes.Interface
}

var _ Authenticator = &TokenAuthenticator{}

func NewTokenAuthenticator(client kubernetes.Interface) *TokenAuthenticator {
	return &TokenAuthenticator{client: client}
}

func (t *TokenAuthenticator) Authenticate(ctx context.Context) (context.Context, error) {
	// Extract the metadata from the context
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ctx, status.Error(codes.InvalidArgument, "missing metadata")
	}

	// Extract the access token from the metadata
	authorization, ok := md["authorization"]
	if !ok || len(authorization) == 0 {
		return ctx, status.Error(codes.Unauthenticated, "invalid token")
	}

	token := strings.TrimPrefix(authorization[0], "Bearer ")
	tr, err := t.client.AuthenticationV1().TokenReviews().Create(ctx, &authenticationv1.TokenReview{
		Spec: authenticationv1.TokenReviewSpec{
			Token: token,
		},
	}, metav1.CreateOptions{})
	if err != nil {
		return ctx, err
	}

	if !tr.Status.Authenticated {
		return ctx, status.Error(codes.Unauthenticated, "token not authenticated")
	}

	newCtx := newContextWithIdentity(ctx, tr.Status.User.Username, tr.Status.User.Groups)
	return newCtx, nil
}
