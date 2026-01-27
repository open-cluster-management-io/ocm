package server

import (
	"context"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"k8s.io/apimachinery/pkg/util/sets"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
)

// AgentEventServer handles resource-related events between grpc server and agents:
// 1. Resource spec events (create, update and delete) from the resource controller.
// 2. Resource status update events from the agent.
type AgentEventServer interface {
	EventHandler

	// RegisterService registers a backend service with a certain data type.
	RegisterService(ctx context.Context, t types.CloudEventsDataType, service Service)

	// Subscribers returns all current subscribers who subscribe to this server.
	Subscribers() sets.Set[string]
}

type EventHandler interface {
	// HandleEvent publish the event to the correct subscriber.
	HandleEvent(ctx context.Context, evt *cloudevents.Event) error
}

// TODO SourceEventServer to handle the grpc conversation between consumers and grpcserver.
