package util

import (
	"context"

	"google.golang.org/grpc"

	"open-cluster-management.io/sdk-go/pkg/server/grpc/authz"
)

type MockAuthorizer struct{}

// validate MockAuthorizer implement StreamAuthorizer and UnaryAuthorizer
var _ authz.StreamAuthorizer = (*MockAuthorizer)(nil)
var _ authz.UnaryAuthorizer = (*MockAuthorizer)(nil)

func NewMockAuthorizer() *MockAuthorizer {
	return &MockAuthorizer{}
}

func (s *MockAuthorizer) AuthorizeRequest(ctx context.Context, req any) (authz.Decision, error) {
	return authz.DecisionAllow, nil
}

func (s *MockAuthorizer) AuthorizeStream(ctx context.Context, ss grpc.ServerStream, info *grpc.StreamServerInfo) (authz.Decision, grpc.ServerStream, error) {
	return authz.DecisionAllow, ss, nil
}
