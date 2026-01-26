package server

import (
	"context"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	cetypes "open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
)

// Service is the interface that the Agent Event Server uses to get cloudevent from the backend storage,
// sends to the related agent, and handle the statusUpdate event sent from the agent.

// TODO need a method to check if an event has been processed already.
type Service interface {
	// List the cloudEvent from the service
	List(ctx context.Context, listOpts cetypes.ListOptions) ([]*cloudevents.Event, error)

	// HandleStatusUpdate processes the resource status update from the agent.
	HandleStatusUpdate(ctx context.Context, evt *cloudevents.Event) error

	// RegisterHandler register the handler to the service.
	RegisterHandler(ctx context.Context, handler EventHandler)
}
