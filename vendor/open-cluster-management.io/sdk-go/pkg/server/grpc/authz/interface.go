package authz

import (
	"context"

	"google.golang.org/grpc"
)

// Decision represents the result of an authorization request.
type Decision int

const (
	// DecisionDeny means the request is explicitly denied.
	DecisionDeny Decision = iota
	// DecisionAllow means the request is explicitly allowed.
	DecisionAllow
	// DecisionNoOpinion means the authorizer has no opinion on the request.
	// This allows the authorization chain to continue to the next authorizer.
	DecisionNoOpinion
)

// UnaryAuthorizer defines the interface for authorizing unary gRPC requests.
// Implementations should validate whether the authenticated user has permission
// to perform the requested operation based on the context and request.
type UnaryAuthorizer interface {
	// AuthorizeRequest validates whether the user in the context is authorized
	// to perform the operation represented by the request. Returns a Decision
	// indicating the authorization result and an error if the authorization process itself fails.
	AuthorizeRequest(ctx context.Context, req any) (Decision, error)
}

// StreamAuthorizer defines the interface for authorizing streaming gRPC requests.
// Implementations should validate whether the authenticated user has permission
// to establish and maintain the streaming connection.
type StreamAuthorizer interface {
	// AuthorizeStream validates whether the user in the context is authorized
	// to establish the streaming connection. Returns a Decision indicating the
	// authorization result, a potentially wrapped ServerStream, and an error
	// if the authorization process itself fails. The returned ServerStream can
	// be used to intercept and authorize individual messages.
	AuthorizeStream(ctx context.Context, ss grpc.ServerStream, info *grpc.StreamServerInfo) (Decision, grpc.ServerStream, error)
}
