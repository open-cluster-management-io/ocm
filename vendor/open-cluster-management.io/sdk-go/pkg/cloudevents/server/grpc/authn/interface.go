package authn

import "context"

// Context key type defined to avoid collisions in other pkgs using context
// See https://golang.org/pkg/context/#WithValue
type contextKey string

const (
	ContextUserKey   contextKey = "user"
	ContextGroupsKey contextKey = "groups"
)

// Authenticator is the interface to authenticate for grpc server
type Authenticator interface {
	Authenticate(ctx context.Context) (context.Context, error)
}

func newContextWithIdentity(ctx context.Context, user string, groups []string) context.Context {
	ctx = context.WithValue(ctx, ContextUserKey, user)
	return context.WithValue(ctx, ContextGroupsKey, groups)
}
