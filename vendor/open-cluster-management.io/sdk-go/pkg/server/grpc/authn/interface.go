package authn

import "context"

// Context key type defined to avoid collisions in other pkgs using context
// See https://golang.org/pkg/context/#WithValue
type contextKey string

const (
	ContextUserKey   contextKey = "user"
	ContextGroupsKey contextKey = "groups"
)

// Authenticator defines the interface for authenticating gRPC requests.
// Implementations should validate user credentials and return an enriched context
// containing user identity information for downstream processing.
type Authenticator interface {
	// Authenticate validates the incoming request context and returns an enriched context
	// containing user identity information (user and groups) or an error if authentication fails.
	// The returned context should include user identity using newContextWithIdentity function.
	Authenticate(ctx context.Context) (context.Context, error)
}

// newContextWithIdentity creates a new context with user identity information.
// It adds the user name and groups to the context using predefined context keys.
// This function is typically used by Authenticator implementations to enrich
// the context with authenticated user information.
func newContextWithIdentity(ctx context.Context, user string, groups []string) context.Context {
	ctx = context.WithValue(ctx, ContextUserKey, user)
	return context.WithValue(ctx, ContextGroupsKey, groups)
}
