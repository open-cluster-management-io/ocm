package options

import (
	"context"

	cloudevents "github.com/cloudevents/sdk-go/v2"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/utils"
)

// ReceiveHandlerFn is a callback function invoked for each received CloudEvent.
// The handler is called synchronously within the Receive loop, so blocking operations
// in the handler will block the reception of subsequent events.
type ReceiveHandlerFn func(cxt context.Context, evt cloudevents.Event)

// CloudEventTransport sends/receives cloudevents based on different event protocol.
//
// Available implementations:
//   - MQTT
//   - gRPC
//   - PubSub
type CloudEventTransport interface {
	// Connect establishes a connection to the event transport.
	// This method should be called before Send or Receive.
	// Returns an error if the connection cannot be established.
	Connect(ctx context.Context) error

	// Send transmits a CloudEvent through the transport.
	// Returns an error if the event cannot be send.
	Send(ctx context.Context, evt cloudevents.Event) error

	// Subscribe sends a subscription request to the transport to subscribe topics/services.
	// This is a non-blocking method that should be called after Connect and before Receive.
	// Returns an error if the subscription request cannot be sent.
	Subscribe(ctx context.Context) error

	// Receive starts receiving events and invokes the provided handler for each event.
	// This is a BLOCKING call that runs an event loop until the context is canceled.
	// The handler function is called synchronously for each received event.
	// This method should typically be run in a separate goroutine.
	//
	// The method returns when:
	//   - The context is canceled (returns ctx.Err())
	//   - A fatal transport error occurs (returns the error)
	//
	// Note: The handler should avoid blocking operations to prevent blocking the
	// reception of subsequent events. For blocking operations, dispatch to a separate
	// goroutine within the handler.
	Receive(ctx context.Context, fn ReceiveHandlerFn) error

	// Close gracefully shuts down the transport, closing all connections and channels.
	// After Close is called, Send and Receive operations will fail.
	// This method waits for in-flight operations to complete or for the context to expire.
	Close(ctx context.Context) error

	// ErrorChan returns a read-only channel that receives asynchronous transport errors.
	// These errors may include connection failures, protocol errors, or other transport-level issues.
	// The channel is closed when Close() is called on the transport.
	// The source/agent client will attempt to reconnect when errors are received on this channel.
	ErrorChan() <-chan error
}

// CloudEventsSourceOptions provides the required options to build a source CloudEventsClient
type CloudEventsSourceOptions struct {
	// CloudEventsTransport sends/receives cloudevents based on different event protocol.
	CloudEventsTransport CloudEventTransport

	// SourceID is a unique identifier for a source, for example, it can generate a source ID by hashing the hub cluster
	// URL and appending the controller name. Similarly, a RESTful service can select a unique name or generate a unique
	// ID in the associated database for its source identification.
	SourceID string

	// EventRateLimit limits the event sending rate.
	EventRateLimit utils.EventRateLimit
}

// CloudEventsAgentOptions provides the required options to build an agent CloudEventsClient
type CloudEventsAgentOptions struct {
	// CloudEventsTransport sends/receives cloudevents based on different event protocol.
	CloudEventsTransport CloudEventTransport

	// AgentID is a unique identifier for an agent, for example, it can consist of a managed cluster name and an agent
	// name.
	AgentID string

	// ClusterName is the name of a managed cluster on which the agent runs.
	ClusterName string

	// EventRateLimit limits the event sending rate.
	EventRateLimit utils.EventRateLimit
}
