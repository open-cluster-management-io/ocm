package server

import (
	"context"

	"k8s.io/apimachinery/pkg/util/sets"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
)

// AgentEventServer handles resource-related events between grpc server and agents:
// 1. Resource spec events (create, update and delete) from the resource controller.
// 2. Resource status update events from the agent.
type AgentEventServer interface {
	EventHandler

	// RegisterService registers a backend service with a certain data type.
	RegisterService(t types.CloudEventsDataType, service Service)

	// Start initiates the EventServer to listen to agents.
	Start(ctx context.Context, addr string)

	// Subscribers returns all current subscribers who subscribe to this server.
	Subscribers() sets.Set[string]
}

type EventHandler interface {
	// OnCreate is the callback when resource is created in the service.
	OnCreate(ctx context.Context, t types.CloudEventsDataType, resourceID string) error

	// OnUpdate is the callback when resource is updated in the service.
	OnUpdate(ctx context.Context, t types.CloudEventsDataType, resourceID string) error

	// OnDelete is the callback when resource is deleted from the service.
	OnDelete(ctx context.Context, t types.CloudEventsDataType, resourceID string) error
}

// TODO SourceEventServer to handle the grpc conversation between consumers and grpcserver.
